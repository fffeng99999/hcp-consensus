package hierarchical

import (
	"fmt"
	"sync"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Config 定义分层共识实验模型参数。
type Config struct {
	NodeCount        int
	GroupCount       int
	GroupSize        int
	MessageBytes     int
	BaseLatencyMs    float64
	PhaseWeightInner float64
	PhaseWeightOuter float64
}

// HierarchicalConsensus 是分层共识引擎的实现。
type HierarchicalConsensus struct {
	mu      sync.RWMutex
	running bool
	cfg     Config
	node    *Node
}

// NewHierarchicalConsensus 创建分层共识引擎。
func NewHierarchicalConsensus(cfg Config) *HierarchicalConsensus {
	cfg = normalizeConfig(cfg)
	scorer := NewTrustScorer(cfg)
	selector := NewValidatorSelector(cfg)
	node := NewNode(cfg, scorer, selector)
	return &HierarchicalConsensus{
		cfg:  cfg,
		node: node,
	}
}

// Start 启动分层共识引擎。
func (h *HierarchicalConsensus) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return nil
	}
	h.running = true
	return nil
}

// Stop 停止分层共识引擎。
func (h *HierarchicalConsensus) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.running = false
	return nil
}

// BeginBlock 在区块开始时执行。
func (h *HierarchicalConsensus) BeginBlock(ctx sdk.Context) {
}

// EndBlock 在区块结束时输出本轮估算指标。
func (h *HierarchicalConsensus) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	metrics := h.node.ComputeMetrics()
	fmt.Printf(
		"hierarchical_metrics pre_prepare_ms=%.6f prepare_ms=%.6f commit_ms=%.6f comm_bytes=%.0f group_count=%d group_size=%d node_count=%d\n",
		metrics.PrePrepareMs,
		metrics.PrepareMs,
		metrics.CommitMs,
		metrics.CommBytes,
		h.cfg.GroupCount,
		h.cfg.GroupSize,
		h.cfg.NodeCount,
	)
	return nil
}
