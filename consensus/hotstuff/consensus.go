package hotstuff

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

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
	NodeCount          int
	FaultyRatio        float64
	ViewTimeoutMs      float64
	TimeoutExponent    float64
	BaseLatencyMs      float64
	JitterMs           float64
	MessageBytes       int
	PipelineDepth      int
	EnableThresholdSig bool
	MaxValidators      int
}

func normalizeConfig(cfg Config) Config {
	cfg.NodeCount = maxInt(1, readEnvInt("HOTSTUFF_NODE_COUNT", cfg.NodeCount))
	cfg.FaultyRatio = clamp01(readEnvFloat("HOTSTUFF_FAULTY_RATIO", cfg.FaultyRatio))
	cfg.ViewTimeoutMs = readEnvFloat("HOTSTUFF_VIEW_TIMEOUT_MS", cfg.ViewTimeoutMs)
	cfg.TimeoutExponent = readEnvFloat("HOTSTUFF_TIMEOUT_EXPONENT", cfg.TimeoutExponent)
	cfg.BaseLatencyMs = readEnvFloat("HOTSTUFF_BASE_LATENCY_MS", cfg.BaseLatencyMs)
	cfg.JitterMs = readEnvFloat("HOTSTUFF_JITTER_MS", cfg.JitterMs)
	cfg.MessageBytes = readEnvInt("HOTSTUFF_MESSAGE_BYTES", cfg.MessageBytes)
	cfg.PipelineDepth = readEnvInt("HOTSTUFF_PIPELINE_DEPTH", cfg.PipelineDepth)
	cfg.EnableThresholdSig = readEnvBool("HOTSTUFF_ENABLE_THRESHOLD_SIG", cfg.EnableThresholdSig)
	cfg.MaxValidators = readEnvInt("HOTSTUFF_MAX_VALIDATORS", cfg.MaxValidators)

	if cfg.NodeCount <= 0 {
		cfg.NodeCount = 4
	}
	if cfg.ViewTimeoutMs <= 0 {
		cfg.ViewTimeoutMs = 5000
	}
	if cfg.TimeoutExponent <= 0 {
		cfg.TimeoutExponent = 2.0
	}
	if cfg.BaseLatencyMs <= 0 {
		cfg.BaseLatencyMs = 1.0
	}
	if cfg.JitterMs < 0 {
		cfg.JitterMs = 0
	}
	if cfg.MessageBytes <= 0 {
		cfg.MessageBytes = 256
	}
	if cfg.PipelineDepth <= 0 {
		cfg.PipelineDepth = 3
	}
	if cfg.MaxValidators <= 0 {
		cfg.MaxValidators = 100
	}
	return cfg
}

type HotStuffConsensus struct {
	mu      sync.RWMutex
	running bool

	Node              *HotStuffNode
	TrustScorer       *TrustScorer
	ValidatorSelector *ValidatorSelector

	Config        Config
	peers         []string
	stakingKeeper StakingKeeper
}

func NewHotStuffConsensus(cfg Config) *HotStuffConsensus {
	cfg = normalizeConfig(cfg)

	peers := make([]string, cfg.NodeCount-1)
	for i := range peers {
		peers[i] = fmt.Sprintf("node%d", i+1)
	}

	scorer := NewTrustScorer(cfg)
	selector := NewValidatorSelector(cfg)
	node := NewHotStuffNode(cfg, scorer, selector)

	return &HotStuffConsensus{
		Node:              node,
		TrustScorer:       scorer,
		ValidatorSelector: selector,
		Config:            cfg,
		peers:             peers,
	}
}

func (h *HotStuffConsensus) SetStakingKeeper(k StakingKeeper) {
	h.stakingKeeper = k
}

func (h *HotStuffConsensus) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.running {
		return nil
	}

	h.running = true
	go h.runLoop()
	return nil
}

func (h *HotStuffConsensus) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running {
		return nil
	}

	h.running = false
	return nil
}

func (h *HotStuffConsensus) runLoop() {
	viewTicker := time.NewTicker(time.Duration(h.Config.ViewTimeoutMs) * time.Millisecond)
	defer viewTicker.Stop()

	for h.running {
		select {
		case <-viewTicker.C:
			h.handleViewTimeout()
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (h *HotStuffConsensus) handleViewTimeout() {
	h.mu.Lock()
	defer h.mu.Unlock()

	leader := h.ValidatorSelector.GetLeader(h.Node.View)
	if h.Node.ID != leader {
		nextView := h.Node.View + 1
		nextLeader := h.ValidatorSelector.GetLeader(nextView)

		fmt.Printf("[Timeout] Node %s timeouts view %d, sending to next leader %s\n",
			h.Node.ID, h.Node.View, nextLeader)

		h.Node.TimeoutCount++
		h.Node.LastViewChange = time.Now()
	} else {
		fmt.Printf("[Timeout] Leader %s view %d timeout\n", h.Node.ID, h.Node.View)
	}
}

func (h *HotStuffConsensus) NewView() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.Node.View++
	currentView := h.Node.View

	leader := h.ValidatorSelector.GetLeader(currentView)
	fmt.Printf("[NewView] Starting View %d, Leader: %s\n", currentView, leader)

	msg := &ConsensusMessage{
		Type:          MessageTypeNewView,
		View:          currentView,
		NodeID:        h.Node.ID,
		Justification: h.Node.JustifiedQC,
	}

	if h.Node.ID == leader {
		h.Node.HandleMessage(msg)
	} else {
		fmt.Printf("[NewView] Node %s sends NewView to leader %s\n", h.Node.ID, leader)
	}
}

func (h *HotStuffConsensus) Propose() {
	h.mu.Lock()
	defer h.mu.Unlock()

	leader := h.ValidatorSelector.GetLeader(h.Node.View)
	if h.Node.ID != leader {
		fmt.Printf("[Propose] Node %s is not leader for view %d\n", h.Node.ID, h.Node.View)
		return
	}

	h.Node.Propose(h.Node.View)
}

func (h *HotStuffConsensus) BeginBlock(ctx sdk.Context) {
}

func (h *HotStuffConsensus) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	metrics := h.Node.ComputeMetrics(ctx.BlockHeight())
	fmt.Printf(
		"hotstuff_metrics block_time_ms=%.6f prepare_ms=%.6f precommit_ms=%.6f commit_ms=%.6f view_changes=%d total_messages=%d comm_bytes=%.0f node_count=%d f=%d quorum=%d faulty_ratio=%.4f view_timeout_ms=%.6f base_latency_ms=%.6f height=%d\n",
		metrics.BlockTimeMs,
		metrics.PrepareMs,
		metrics.PreCommitMs,
		metrics.CommitMs,
		metrics.ViewChanges,
		metrics.TotalMessages,
		metrics.CommBytes,
		metrics.NodeCount,
		metrics.F,
		metrics.Quorum,
		metrics.FaultyRatio,
		metrics.ViewTimeoutMs,
		metrics.BaseLatencyMs,
		ctx.BlockHeight(),
	)

	if h.stakingKeeper != nil {
		h.updateValidatorSet(ctx)
	}
	return nil
}

func (h *HotStuffConsensus) updateValidatorSet(ctx sdk.Context) {
	validators, err := h.stakingKeeper.GetAllValidators(ctx)
	if err != nil {
		return
	}

	var addrs []string
	for _, v := range validators {
		if len(addrs) >= h.Config.MaxValidators {
			break
		}
		addrs = append(addrs, v.OperatorAddress)
	}

	if len(addrs) > 0 {
		h.ValidatorSelector.UpdateValidators(addrs)
	}
}

func (h *HotStuffConsensus) GetNode() *HotStuffNode {
	return h.Node
}

func (h *HotStuffConsensus) GetStatus() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return map[string]interface{}{
		"running":         h.running,
		"view":            h.Node.View,
		"height":          h.Node.Height,
		"leader":          h.ValidatorSelector.GetLeader(h.Node.View),
		"locked_qc":       safeView(h.Node.LockedQC),
		"prepare_qc":      safeView(h.Node.PrepareQC),
		"commit_qc":       safeView(h.Node.CommitQC),
		"justified_qc":    safeView(h.Node.JustifiedQC),
		"timeout_count":   h.Node.TimeoutCount,
		"total_nodes":     h.Node.Total,
		"fault_tolerance": h.Node.F,
	}
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

func readEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
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
