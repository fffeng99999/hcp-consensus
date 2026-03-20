package hierarchical_tpbft

import (
	"math"
	"strings"
)

// Node 封装分层 TPBFT 单轮指标计算流程。
type Node struct {
	cfg      Config
	scorer   *TrustScorer
	selector *ValidatorSelector
}

// NewNode 创建分层 TPBFT 节点模型。
func NewNode(cfg Config, scorer *TrustScorer, selector *ValidatorSelector) *Node {
	return &Node{
		cfg:      cfg,
		scorer:   scorer,
		selector: selector,
	}
}

// ComputeMetrics 计算当前配置下的分层 TPBFT 指标。
func (n *Node) ComputeMetrics() Metrics {
	cfg := n.scorer.Config()
	nodeCount := float64(cfg.NodeCount)
	groupCount, groupSize := n.selector.ResolveGroupShape()
	g := float64(groupCount)
	s := float64(groupSize)
	base := cfg.BaseLatencyMs
	innerWeight := cfg.PhaseWeightInner
	outerWeight := cfg.PhaseWeightOuter
	if g <= 0 {
		g = 1
	}
	if s <= 0 {
		s = math.Max(1, math.Floor(nodeCount/g))
	}

	totalMessages := nodeCount + g
	commBytes := totalMessages * float64(cfg.MessageBytes)
	outerMode := strings.ToLower(cfg.OuterSigMode)
	if outerMode == "" {
		outerMode = "threshold"
	}
	outerAlgo := strings.ToLower(cfg.OuterSigAlgorithm)
	if outerAlgo == "" {
		outerAlgo = cfg.SigAlgorithm
	}
	outerSigGenMs, outerSigVerifyMs, outerSigAggMs := n.resolveOuterSigCosts(cfg, outerMode, outerAlgo)

	innerSigGenOps := s
	innerSigVerifyOps := s
	innerAggOps := 1.0
	outerSigGenOps, outerSigVerifyOps, outerAggOps := resolveOuterOps(g, outerMode)

	sigGenCount := innerSigGenOps + outerSigGenOps
	sigVerifyCount := innerSigVerifyOps + outerSigVerifyOps
	sigGenParallelism := math.Max(1, cfg.SigGenParallelism)
	sigVerifyParallelism := math.Max(1, cfg.SigVerifyParallelism)
	sigAggParallelism := math.Max(1, cfg.SigAggParallelism)
	verifyGain := math.Max(1, cfg.BatchVerifyGain)
	batchVerify := 0.0
	if cfg.BatchVerify && verifyGain > 1 {
		batchVerify = 1
	}

	sigGenTime := (innerSigGenOps*cfg.SigGenMs + outerSigGenOps*outerSigGenMs) / sigGenParallelism
	sigVerifyTime := (innerSigVerifyOps*cfg.SigVerifyMs + outerSigVerifyOps*outerSigVerifyMs) / sigVerifyParallelism
	if batchVerify > 0 {
		sigVerifyTime /= verifyGain
	}
	aggTime := (innerAggOps*cfg.SigAggMs + outerAggOps*outerSigAggMs) / sigAggParallelism

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	sigOpsPerTx := (sigGenCount + sigVerifyCount + innerAggOps + outerAggOps) / float64(batchSize)

	pre := base * innerWeight * s
	prepare := base * innerWeight * s
	commit := base * outerWeight * g

	sigPerNode := 0.0
	if nodeCount > 0 {
		sigPerNode = 2 + 2/g
	}

	return Metrics{
		PrePrepareMs:         pre,
		PrepareMs:            prepare,
		CommitMs:             commit,
		CommBytes:            commBytes,
		TotalMessages:        totalMessages,
		SigGenCount:          sigGenCount,
		SigVerifyCount:       sigVerifyCount,
		SigGenTimeMs:         sigGenTime,
		SigVerifyTimeMs:      sigVerifyTime,
		AggregationTimeMs:    aggTime,
		VerifyTimeMs:         sigVerifyTime,
		SigPerNode:           sigPerNode,
		SigOpsPerTx:          sigOpsPerTx,
		BatchSize:            batchSize,
		BatchVerify:          batchVerify,
		VerifyGain:           verifyGain,
		SigGenParallelism:    sigGenParallelism,
		SigVerifyParallelism: sigVerifyParallelism,
		SigAggParallelism:    sigAggParallelism,
		OuterMode:            outerMode,
		SigAlgo:              cfg.SigAlgorithm,
		OuterAlgo:            outerAlgo,
	}
}

// resolveOuterSigCosts 解析外层签名阶段的耗时参数。
func (n *Node) resolveOuterSigCosts(cfg Config, outerMode string, outerAlgo string) (float64, float64, float64) {
	outerSigGenMs := cfg.OuterSigGenMs
	outerSigVerifyMs := cfg.OuterSigVerifyMs
	outerSigAggMs := cfg.OuterSigAggMs
	if outerSigGenMs > 0 && outerSigVerifyMs > 0 && outerSigAggMs > 0 {
		return outerSigGenMs, outerSigVerifyMs, outerSigAggMs
	}
	defaultGen, defaultVerify, defaultAgg := defaultSigProfile(outerAlgo)
	if outerSigGenMs <= 0 {
		outerSigGenMs = defaultGen
	}
	if outerSigVerifyMs <= 0 {
		outerSigVerifyMs = defaultVerify
	}
	if outerSigAggMs <= 0 {
		outerSigAggMs = defaultAgg
	}
	if outerMode == "none" {
		outerSigGenMs = 0
		outerSigAggMs = 0
	}
	return outerSigGenMs, outerSigVerifyMs, outerSigAggMs
}

// resolveOuterOps 计算外层签名各类操作次数。
func resolveOuterOps(groupCount float64, outerMode string) (float64, float64, float64) {
	outerSigGenOps := 0.0
	outerSigVerifyOps := 0.0
	outerAggOps := 0.0
	switch outerMode {
	case "ed25519":
		outerSigGenOps = groupCount
		outerSigVerifyOps = groupCount
	case "none":
		outerSigVerifyOps = groupCount
	default:
		outerSigGenOps = groupCount
		outerSigVerifyOps = groupCount
		outerAggOps = 1
	}
	return outerSigGenOps, outerSigVerifyOps, outerAggOps
}
