package ibft

import (
	"fmt"
	"sync"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Config struct {
	NodeCount     int
	FaultyRatio   float64
	BaseLatencyMs float64
	JitterMs      float64
	TimeoutMs     float64
	MessageBytes  int
	MaxRounds     int
}

type IBFT struct {
	mu      sync.RWMutex
	running bool
	cfg     Config
	node    *Node
}

func NewIBFT(cfg Config) *IBFT {
	cfg = normalizeConfig(cfg)
	scorer := NewTrustScorer(cfg)
	selector := NewValidatorSelector(cfg)
	node := NewNode(cfg, scorer, selector)
	return &IBFT{
		cfg:  cfg,
		node: node,
	}
}

func (i *IBFT) Start() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.running {
		return nil
	}
	i.running = true
	return nil
}

func (i *IBFT) Stop() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.running = false
	return nil
}

func (i *IBFT) BeginBlock(ctx sdk.Context) {
}

func (i *IBFT) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	metrics := i.node.ComputeMetrics(ctx.BlockHeight())
	fmt.Printf(
		"ibft_metrics block_time_ms=%.6f pre_prepare_ms=%.6f prepare_ms=%.6f commit_ms=%.6f round_changes=%d total_messages=%d comm_bytes=%.0f node_count=%d f=%d quorum=%d faulty_ratio=%.4f timeout_ms=%.6f base_latency_ms=%.6f height=%d\n",
		metrics.BlockTimeMs,
		metrics.PrePrepareMs,
		metrics.PrepareMs,
		metrics.CommitMs,
		metrics.RoundChanges,
		metrics.TotalMessages,
		metrics.CommBytes,
		metrics.NodeCount,
		metrics.F,
		metrics.Quorum,
		metrics.FaultyRatio,
		metrics.TimeoutMs,
		metrics.BaseLatencyMs,
		ctx.BlockHeight(),
	)
	return nil
}
