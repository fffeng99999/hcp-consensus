package hierarchical_lightweight_tpbft

import (
	"fmt"
	"sync"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Config 包含分层轻量 TPBFT 的全部配置参数。
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
	// 子层轻量共识新增参数
	SubConsensusType    string
	RaftHeartbeatMs     float64
	RaftElectionMs      float64
	FaultInject         bool
	FaultAfterSec       int
}

// HierarchicalLightweightTPBFT 分层轻量 TPBFT 共识引擎。
type HierarchicalLightweightTPBFT struct {
	mu      sync.RWMutex
	running bool
	cfg     Config
	node    *Node
}

// NewHierarchicalLightweightTPBFT 创建分层轻量 TPBFT 引擎。
func NewHierarchicalLightweightTPBFT(cfg Config) *HierarchicalLightweightTPBFT {
	cfg = normalizeConfig(cfg)
	scorer := NewTrustScorer(cfg)
	selector := NewValidatorSelector(cfg)
	node := NewNode(cfg, scorer, selector)
	return &HierarchicalLightweightTPBFT{
		cfg:  cfg,
		node: node,
	}
}

// Start 启动共识引擎。
func (h *HierarchicalLightweightTPBFT) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return nil
	}
	h.running = true
	return nil
}

// Stop 停止共识引擎。
func (h *HierarchicalLightweightTPBFT) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.running = false
	return nil
}

// BeginBlock 开始区块。
func (h *HierarchicalLightweightTPBFT) BeginBlock(ctx sdk.Context) {
}

// EndBlock 结束区块并输出轻量共识综合指标。
func (h *HierarchicalLightweightTPBFT) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	metrics := h.node.ComputeMetrics()
	fmt.Printf(
		"hierarchical_lightweight_tpbft_metrics pre_prepare_ms=%.6f prepare_ms=%.6f commit_ms=%.6f comm_bytes=%.0f total_messages=%.0f sig_gen_count=%.0f sig_verify_count=%.0f sig_gen_time_ms=%.6f sig_verify_time_ms=%.6f aggregation_time_ms=%.6f verify_time_ms=%.6f sig_per_node=%.6f sig_ops_per_tx=%.6f batch_size=%d batch_verify=%.0f verify_gain=%.2f sig_gen_parallelism=%.2f sig_verify_parallelism=%.2f sig_agg_parallelism=%.2f outer_mode=%s algo=%s outer_algo=%s group_count=%d group_size=%d node_count=%d sub_consensus=%s sub_pre_prepare_ms=%.6f sub_prepare_ms=%.6f sub_append_ms=%.6f sub_messages=%.0f recovery_time_ms=%.6f fault_injected=%.0f\n",
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
		metrics.SubConsensusType,
		metrics.SubPrePrepareMs,
		metrics.SubPrepareMs,
		metrics.SubAppendMs,
		metrics.SubMessages,
		metrics.RecoveryTimeMs,
		metrics.FaultInjected,
	)
	return nil
}
