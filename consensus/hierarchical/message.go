package hierarchical

// ConsensusMessage 描述分层共识中的抽象消息。
type ConsensusMessage struct {
	Phase        string
	GroupID      int
	FromNodeID   string
	ToNodeID     string
	PayloadBytes int
}

// Metrics 是分层共识每轮估算结果。
type Metrics struct {
	PrePrepareMs float64
	PrepareMs    float64
	CommitMs     float64
	CommBytes    float64
}
