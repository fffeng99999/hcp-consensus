package parallelmerkle

// ConsensusMessage 描述并行 Merkle 的抽象消息。
type ConsensusMessage struct {
	Phase      string
	FromNodeID string
	ToNodeID   string
	K          int
}
