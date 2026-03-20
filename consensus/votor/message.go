package votor

// ConsensusMessage 描述 Votor 协议中的抽象投票消息。
type ConsensusMessage struct {
	Phase      string
	FromNodeID string
	ToNodeID   string
	Height     int64
}

// Metrics 描述 Votor 每轮输出指标。
type Metrics struct {
	NotarizeLatencyMs float64
	FinalizeLatencyMs float64
	BlsAggMs          float64
	P2PVoteBytes      float64
	GossipVoteBytes   float64
	CertificateBytes  float64
	PathType          string
}
