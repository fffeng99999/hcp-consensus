package tpbft_parallel

// ConsensusMessage 描述并行 TPBFT 的抽象消息。
type ConsensusMessage struct {
	Phase      string
	FromNodeID string
	ToNodeID   string
	K          int
}
