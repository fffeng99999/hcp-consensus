package hierarchical_tpbft

import (
	"fmt"
	"sync"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Config struct {
	NodeCount            int
	GroupCount           int
	GroupSize            int
	MessageBytes         int
	BaseLatencyMs        float64
	PhaseWeightInner     float64
	PhaseWeightOuter     float64
	SigAlgorithm         string
	SigGenMs             float64
	SigVerifyMs          float64
	SigAggMs             float64
	OuterSigMode         string
	OuterSigAlgorithm    string
	OuterSigGenMs        float64
	OuterSigVerifyMs     float64
	OuterSigAggMs        float64
	BatchVerify          bool
	BatchVerifyGain      float64
	SigGenParallelism    float64
	SigVerifyParallelism float64
	SigAggParallelism    float64
	BatchSize            int
}

type HierarchicalTPBFT struct {
	mu      sync.RWMutex
	running bool
	cfg     Config
	node    *Node
}

func NewHierarchicalTPBFT(cfg Config) *HierarchicalTPBFT {
	cfg = normalizeConfig(cfg)
	scorer := NewTrustScorer(cfg)
	selector := NewValidatorSelector(cfg)
	node := NewNode(cfg, scorer, selector)
	return &HierarchicalTPBFT{
		cfg:  cfg,
		node: node,
	}
}

func (h *HierarchicalTPBFT) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return nil
	}
	h.running = true
	return nil
}

func (h *HierarchicalTPBFT) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.running = false
	return nil
}

func (h *HierarchicalTPBFT) BeginBlock(ctx sdk.Context) {
}

func (h *HierarchicalTPBFT) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	metrics := h.node.ComputeMetrics()
	fmt.Printf(
		"hierarchical_tpbft_metrics pre_prepare_ms=%.6f prepare_ms=%.6f commit_ms=%.6f comm_bytes=%.0f total_messages=%.0f sig_gen_count=%.0f sig_verify_count=%.0f sig_gen_time_ms=%.6f sig_verify_time_ms=%.6f aggregation_time_ms=%.6f verify_time_ms=%.6f sig_per_node=%.6f sig_ops_per_tx=%.6f batch_size=%d batch_verify=%.0f verify_gain=%.2f sig_gen_parallelism=%.2f sig_verify_parallelism=%.2f sig_agg_parallelism=%.2f outer_mode=%s algo=%s outer_algo=%s group_count=%d group_size=%d node_count=%d\n",
		metrics.PrePrepareMs,
		metrics.PrepareMs,
		metrics.CommitMs,
		metrics.CommBytes,
		metrics.TotalMessages,
		metrics.SigGenCount,
		metrics.SigVerifyCount,
		metrics.SigGenTimeMs,
		metrics.SigVerifyTimeMs,
		metrics.AggregationTimeMs,
		metrics.VerifyTimeMs,
		metrics.SigPerNode,
		metrics.SigOpsPerTx,
		metrics.BatchSize,
		metrics.BatchVerify,
		metrics.VerifyGain,
		metrics.SigGenParallelism,
		metrics.SigVerifyParallelism,
		metrics.SigAggParallelism,
		metrics.OuterMode,
		metrics.SigAlgo,
		metrics.OuterAlgo,
		h.cfg.GroupCount,
		h.cfg.GroupSize,
		h.cfg.NodeCount,
	)
	return nil
}
