package tpbft_parallel_block

// ConsensusMessage 描述按区块并行 Merkle 的抽象消息。
type ConsensusMessage struct {
	Height     int64
	SubBlockK  int
	FromNodeID string
}
