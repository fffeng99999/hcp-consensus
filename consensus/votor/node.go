package votor

// Node 封装 Votor 的每轮指标计算。
type Node struct {
	cfg      Config
	scorer   *TrustScorer
	selector *ValidatorSelector
}

// NewNode 创建 Votor 节点模型。
func NewNode(cfg Config, scorer *TrustScorer, selector *ValidatorSelector) *Node {
	return &Node{
		cfg:      cfg,
		scorer:   scorer,
		selector: selector,
	}
}

// ComputeMetrics 计算当前配置下 Votor 指标。
func (n *Node) ComputeMetrics() Metrics {
	nodeCount := maxInt(1, n.cfg.NodeCount)
	pathType := n.selector.DecidePath()
	blsAggMs := n.scorer.SimulateBLSAggregationMs(nodeCount)
	notarize, finalize := n.scorer.FinalityLatency(pathType, blsAggMs)

	voteBytes := float64(maxInt(0, n.cfg.SignatureBytes)+maxInt(0, n.cfg.HeaderBytes))
	p2pVotes := float64(nodeCount) * voteBytes
	gossipVotes := float64(nodeCount*nodeCount) * voteBytes
	certificate := n.selector.EstimateCertificateBytes(nodeCount, n.cfg.CertFixedBytes, n.cfg.CertBitmapBytes)

	return Metrics{
		NotarizeLatencyMs: notarize,
		FinalizeLatencyMs: finalize,
		BlsAggMs:          blsAggMs,
		P2PVoteBytes:      p2pVotes,
		GossipVoteBytes:   gossipVotes,
		CertificateBytes:  certificate,
		PathType:          pathType,
	}
}
