package hierarchical_lightweight_tpbft

// ConsensusMessage 描述分层轻量 TPBFT 的抽象阶段消息。
type ConsensusMessage struct {
	Phase      string
	FromNodeID string
	ToNodeID   string
	GroupID    int
	BatchSize  int
}

// Metrics 描述分层轻量 TPBFT 每轮输出的综合指标。
type Metrics struct {
	PrePrepareMs         float64
	PrepareMs            float64
	CommitMs             float64
	CommBytes            float64
	TotalMessages        float64
	SigGenCount          float64
	SigVerifyCount       float64
	SigGenTimeMs         float64
	SigVerifyTimeMs      float64
	AggregationTimeMs    float64
	VerifyTimeMs         float64
	SigPerNode           float64
	SigOpsPerTx          float64
	BatchSize            int
	BatchVerify          float64
	VerifyGain           float64
	SigGenParallelism    float64
	SigVerifyParallelism float64
	SigAggParallelism    float64
	OuterMode            string
	SigAlgo              string
	OuterAlgo            string
	// 子层轻量共识新增指标
	SubConsensusType string
	SubPrePrepareMs  float64
	SubPrepareMs     float64
	SubAppendMs      float64
	SubMessages      float64
	RecoveryTimeMs   float64
	FaultInjected    float64
}
