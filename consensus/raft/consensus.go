package raft

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"

	cosmossdk_math "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

type StakingKeeper interface {
	GetValidatorByConsAddr(ctx context.Context, consAddr sdk.ConsAddress) (stakingtypes.Validator, error)
	GetAllValidators(ctx context.Context) ([]stakingtypes.Validator, error)
	TotalBondedTokens(ctx context.Context) (cosmossdk_math.Int, error)
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
}

type Config struct {
	NodeCount              int
	ElectionTimeoutMs      float64
	HeartbeatIntervalMs    float64
	ElectionTimeoutRangeMs float64
	SnapshotDistance       int
	MaxLogEntriesPerRPC    int
	MessageBytes           int
	FaultyRatio            float64
	MaxValidators          int
}

func normalizeConfig(cfg Config) Config {
	cfg.NodeCount = maxInt(1, readEnvInt("RAFT_NODE_COUNT", cfg.NodeCount))
	cfg.FaultyRatio = clamp01(readEnvFloat("RAFT_FAULTY_RATIO", cfg.FaultyRatio))
	cfg.ElectionTimeoutMs = readEnvFloat("RAFT_ELECTION_TIMEOUT_MS", cfg.ElectionTimeoutMs)
	cfg.HeartbeatIntervalMs = readEnvFloat("RAFT_HEARTBEAT_INTERVAL_MS", cfg.HeartbeatIntervalMs)
	cfg.ElectionTimeoutRangeMs = readEnvFloat("RAFT_ELECTION_TIMEOUT_RANGE_MS", cfg.ElectionTimeoutRangeMs)
	cfg.SnapshotDistance = readEnvInt("RAFT_SNAPSHOT_DISTANCE", cfg.SnapshotDistance)
	cfg.MaxLogEntriesPerRPC = readEnvInt("RAFT_MAX_LOG_ENTRIES_PER_RPC", cfg.MaxLogEntriesPerRPC)
	cfg.MessageBytes = readEnvInt("RAFT_MESSAGE_BYTES", cfg.MessageBytes)
	cfg.MaxValidators = readEnvInt("RAFT_MAX_VALIDATORS", cfg.MaxValidators)

	if cfg.NodeCount <= 0 {
		cfg.NodeCount = 4
	}
	if cfg.ElectionTimeoutMs <= 0 {
		cfg.ElectionTimeoutMs = 150
	}
	if cfg.HeartbeatIntervalMs <= 0 {
		cfg.HeartbeatIntervalMs = 50
	}
	if cfg.HeartbeatIntervalMs >= cfg.ElectionTimeoutMs {
		cfg.HeartbeatIntervalMs = cfg.ElectionTimeoutMs / 2
	}
	if cfg.ElectionTimeoutRangeMs <= 0 {
		cfg.ElectionTimeoutRangeMs = cfg.ElectionTimeoutMs
	}
	if cfg.SnapshotDistance <= 0 {
		cfg.SnapshotDistance = 10000
	}
	if cfg.MaxLogEntriesPerRPC <= 0 {
		cfg.MaxLogEntriesPerRPC = 500
	}
	if cfg.MessageBytes <= 0 {
		cfg.MessageBytes = 256
	}
	if cfg.MaxValidators <= 0 {
		cfg.MaxValidators = 100
	}
	return cfg
}

func DefaultConfig() Config {
	return Config{
		NodeCount:              4,
		ElectionTimeoutMs:      150,
		HeartbeatIntervalMs:    50,
		ElectionTimeoutRangeMs: 150,
		SnapshotDistance:       10000,
		MaxLogEntriesPerRPC:    500,
		MessageBytes:           256,
		FaultyRatio:            0,
		MaxValidators:          100,
	}
}

type RaftConsensus struct {
	mu      sync.RWMutex
	running bool

	Node              *RaftNode
	TrustScorer       *TrustScorer
	ValidatorSelector *ValidatorSelector

	Config        Config
	peers         []string
	stakingKeeper StakingKeeper
}

func NewRaftConsensus(cfg Config) *RaftConsensus {
	cfg = normalizeConfig(cfg)

	peers := make([]string, cfg.NodeCount-1)
	for i := range peers {
		peers[i] = fmt.Sprintf("node%d", i+1)
	}

	scorer := NewTrustScorer(cfg)
	selector := NewValidatorSelector(cfg)
	node := NewRaftNode(cfg, scorer, selector)

	return &RaftConsensus{
		Node:              node,
		TrustScorer:       scorer,
		ValidatorSelector: selector,
		Config:            cfg,
		peers:             peers,
	}
}

func (r *RaftConsensus) SetStakingKeeper(k StakingKeeper) {
	r.stakingKeeper = k
}

func (r *RaftConsensus) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return nil
	}

	r.running = true
	r.Node.Start()
	return nil
}

func (r *RaftConsensus) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return nil
	}

	r.running = false
	r.Node.Stop()
	return nil
}

func (r *RaftConsensus) SubmitCommand(command []byte) bool {
	return r.Node.SubmitCommand(command)
}

func (r *RaftConsensus) BeginBlock(ctx sdk.Context) {
}

func (r *RaftConsensus) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	metrics := r.Node.ComputeMetrics(ctx.BlockHeight())
	fmt.Printf(
		"raft_metrics block_time_ms=%.6f append_entries_ms=%.6f replication_ms=%.6f election_ms=%.6f elections=%d heartbeat_messages=%d total_messages=%d comm_bytes=%.0f node_count=%d quorum=%d faulty_ratio=%.4f election_timeout_ms=%.6f heartbeat_interval_ms=%.6f height=%d\n",
		metrics.BlockTimeMs,
		metrics.AppendEntriesMs,
		metrics.ReplicationMs,
		metrics.ElectionMs,
		metrics.Elections,
		metrics.HeartbeatMessages,
		metrics.TotalMessages,
		metrics.CommBytes,
		metrics.NodeCount,
		metrics.Quorum,
		metrics.FaultyRatio,
		metrics.ElectionTimeoutMs,
		metrics.HeartbeatIntervalMs,
		ctx.BlockHeight(),
	)

	if r.stakingKeeper != nil {
		r.updateValidatorSet(ctx)
	}
	return nil
}

func (r *RaftConsensus) updateValidatorSet(ctx sdk.Context) {
	validators, err := r.stakingKeeper.GetAllValidators(ctx)
	if err != nil {
		return
	}

	var addrs []string
	for _, v := range validators {
		if len(addrs) >= r.Config.MaxValidators {
			break
		}
		addrs = append(addrs, v.OperatorAddress)
	}

	if len(addrs) > 0 {
		r.ValidatorSelector.UpdateValidators(addrs)
	}
}

func (r *RaftConsensus) GetStatus() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return map[string]interface{}{
		"running": r.running,
		"node":    r.Node.GetStatus(),
		"leader":  r.isLeader(),
		"quorum":  r.Node.QuorumSize(),
	}
}

func (r *RaftConsensus) isLeader() bool {
	return r.Node.Role == RoleLeader
}

func (r *RaftConsensus) GetNode() *RaftNode {
	return r.Node
}

func readEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func readEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultVal
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
