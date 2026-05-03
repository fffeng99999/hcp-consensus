package hierarchical_tpbft_parallel_block

import (
	"fmt"
	"runtime"
	"sync"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/fffeng99999/hcp-consensus/consensus/tpbft"
)

// Config 包含分层 TPBFT 与并行 Merkle 块的全部配置参数。
type Config struct {
	// 分层 TPBFT 参数
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

	// 并行 Merkle 块参数
	SubBlockK int
}

// HierarchicalTPBFTParallelBlock 分层 TPBFT + 并行 Merkle 块共识引擎。
type HierarchicalTPBFTParallelBlock struct {
	base       *tpbft.TPBFT
	cfg        Config
	running    bool
	mu         sync.Mutex
	lastHeight int64
	node       *Node
	scorer     *TrustScorer
	validator  *ValidatorSelector
	pendingTxs [][]byte
}

// NewHierarchicalTPBFTParallelBlock 创建分层 TPBFT 并行块引擎。
func NewHierarchicalTPBFTParallelBlock(cfg Config) *HierarchicalTPBFTParallelBlock {
	cfg = normalizeConfig(cfg)
	scorer := NewTrustScorer(cfg)
	selector := NewValidatorSelector(cfg)
	node := NewNode(cfg, scorer, selector)
	return &HierarchicalTPBFTParallelBlock{
		cfg:       cfg,
		base:      tpbft.NewTPBFT(),
		node:      node,
		scorer:    scorer,
		validator: selector,
	}
}

// SetStakingKeeper 设置质押 keeper。
func (h *HierarchicalTPBFTParallelBlock) SetStakingKeeper(k tpbft.StakingKeeper) {
	h.base.SetStakingKeeper(k)
}

// Start 启动共识引擎。
func (h *HierarchicalTPBFTParallelBlock) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return nil
	}
	h.running = true
	if h.cfg.SubBlockK > 1 {
		runtime.GOMAXPROCS(h.cfg.SubBlockK)
	}
	return h.base.Start()
}

// Stop 停止共识引擎。
func (h *HierarchicalTPBFTParallelBlock) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.running = false
	return h.base.Stop()
}

// BeginBlock 开始区块。
func (h *HierarchicalTPBFTParallelBlock) BeginBlock(ctx sdk.Context) {
	h.base.BeginBlock(ctx)
}

// EndBlock 结束区块并输出综合指标。
func (h *HierarchicalTPBFTParallelBlock) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	h.mu.Lock()
	txs := h.pendingTxs
	h.pendingTxs = nil
	h.mu.Unlock()

	metrics := h.node.ComputeMetrics(txs)
	fmt.Printf(
		"hierarchical_tpbft_parallel_block_metrics pre_prepare_ms=%.6f prepare_ms=%.6f commit_ms=%.6f comm_bytes=%.0f total_messages=%.0f sig_gen_count=%.0f sig_verify_count=%.0f sig_gen_time_ms=%.6f sig_verify_time_ms=%.6f aggregation_time_ms=%.6f verify_time_ms=%.6f sig_per_node=%.6f sig_ops_per_tx=%.6f batch_size=%d batch_verify=%.0f verify_gain=%.2f sig_gen_parallelism=%.2f sig_verify_parallelism=%.2f sig_agg_parallelism=%.2f outer_mode=%s algo=%s outer_algo=%s group_count=%d group_size=%d node_count=%d block_time_ms=%.4f subblock_time_ms=%.4f merge_time_ms=%.4f k=%d txs=%d bytes=%d\n",
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
		metrics.BlockTimeMs,
		metrics.SubBlockTimeMs,
		metrics.MergeTimeMs,
		metrics.SubBlockK,
		metrics.TxCount,
		metrics.TxBytes,
	)
	return h.base.EndBlock(ctx)
}

// ObserveProposal 监听提案事件，缓存交易并触发并行 Merkle 计算。
func (h *HierarchicalTPBFTParallelBlock) ObserveProposal(height int64, txs [][]byte) {
	h.mu.Lock()
	if !h.running || height <= h.lastHeight {
		h.mu.Unlock()
		return
	}
	h.lastHeight = height
	h.pendingTxs = make([][]byte, len(txs))
	copy(h.pendingTxs, txs)
	h.mu.Unlock()
}
