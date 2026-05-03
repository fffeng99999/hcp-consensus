package hierarchical_lightweight_tpbft

import (
	"math"
	"strings"
)

// Node 封装分层轻量 TPBFT 单轮指标计算流程。
type Node struct {
	cfg      Config
	scorer   *TrustScorer
	selector *ValidatorSelector
}

// NewNode 创建分层轻量 TPBFT 节点模型。
func NewNode(cfg Config, scorer *TrustScorer, selector *ValidatorSelector) *Node {
	return &Node{
		cfg:      cfg,
		scorer:   scorer,
		selector: selector,
	}
}

// ComputeMetrics 计算当前配置下的分层轻量 TPBFT 指标。
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

	// 子层共识类型
	subType := cfg.SubConsensusType
	isRaft := subType == "raft"

	// 子层消息数计算
	var subMessages float64
	var subPrePrepareMs, subPrepareMs, subAppendMs float64
	if isRaft {
		// Raft: Leader -> Followers AppendEntries + Followers Ack
		// 消息数 ≈ s * 2, 时延 ≈ base * innerWeight * s * 0.35 (单次复制)
		subMessages = s * 2.0
		subAppendMs = base * innerWeight * s * 0.35
		// Raft 无 Pre-prepare/Prepare 广播阶段
		subPrePrepareMs = 0
		subPrepareMs = 0
	} else {
		// PBFT: 子层完整三阶段
		// PrePrepare: Leader -> s-1 followers
		// Prepare: 所有 s 节点互发
		subMessages = s*s + s // PrePrepare广播 + Prepare全网
		subPrePrepareMs = base * innerWeight * s
		subPrepareMs = base * innerWeight * s
		subAppendMs = 0
	}

	totalMessages := nodeCount + g + subMessages*g
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

	// PrePrepare: 若子层是PBFT则包含子层pre_prepare；Raft则无
	pre := base * innerWeight * s
	if !isRaft {
		pre = subPrePrepareMs
	}
	prepare := base * innerWeight * s
	if !isRaft {
		prepare = subPrepareMs
	}
	commit := base * outerWeight * g

	sigPerNode := 0.0
	if nodeCount > 0 {
		sigPerNode = 2 + 2/g
	}

	// 故障注入恢复时间
	recoveryTimeMs := 0.0
	faultInjected := 0.0
	if cfg.FaultInject && isRaft {
		faultInjected = 1.0
		// 恢复时间 = 选举超时 + 日志同步
		recoveryTimeMs = cfg.RaftElectionMs + base*s
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
		SubConsensusType:     subType,
		SubPrePrepareMs:      subPrePrepareMs,
		SubPrepareMs:         subPrepareMs,
		SubAppendMs:          subAppendMs,
		SubMessages:          subMessages * g,
		RecoveryTimeMs:       recoveryTimeMs,
		FaultInjected:        faultInjected,
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
