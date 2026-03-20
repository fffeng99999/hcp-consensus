package hierarchical

import "math"

// Node 封装单轮分层共识的指标计算逻辑。
type Node struct {
	cfg      Config
	scorer   *TrustScorer
	selector *ValidatorSelector
}

// NewNode 创建分层共识节点模型。
func NewNode(cfg Config, scorer *TrustScorer, selector *ValidatorSelector) *Node {
	return &Node{
		cfg:      cfg,
		scorer:   scorer,
		selector: selector,
	}
}

// ComputeMetrics 计算当前配置下的通信与阶段时延估算值。
func (n *Node) ComputeMetrics() Metrics {
	nodeCount := float64(n.cfg.NodeCount)
	groupCount, groupSize := n.selector.ResolveGroupShape()
	g := float64(groupCount)
	s := float64(groupSize)

	baseLatency := n.scorer.BaseLatencyMs()
	innerWeight := n.scorer.InnerWeight()
	outerWeight := n.scorer.OuterWeight()
	messageBytes := n.scorer.MessageBytes()

	if g <= 0 {
		g = 1
	}
	if s <= 0 {
		s = math.Max(1, math.Floor(nodeCount/g))
	}

	commCost := (nodeCount*nodeCount)/g + g*g
	prePrepare := baseLatency * innerWeight * s
	prepare := baseLatency * innerWeight * (nodeCount * nodeCount) / g
	commit := baseLatency * outerWeight * (g * g)

	return Metrics{
		PrePrepareMs: prePrepare,
		PrepareMs:    prepare,
		CommitMs:     commit,
		CommBytes:    commCost * float64(messageBytes),
	}
}
