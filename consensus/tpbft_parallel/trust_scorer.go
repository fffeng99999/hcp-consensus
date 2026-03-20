package tpbft_parallel

// TrustScorer 复用命名以对齐 tpbft 目录结构，这里保存并行参数。
type TrustScorer struct {
	txCount int
	txSize  int
	repeat  int
}

// NewTrustScorer 创建并行参数评分器。
func NewTrustScorer(cfg Config) *TrustScorer {
	return &TrustScorer{
		txCount: cfg.TxCount,
		txSize:  cfg.TxSize,
		repeat:  cfg.Repeat,
	}
}

// TxCount 返回交易数量。
func (s *TrustScorer) TxCount() int {
	return s.txCount
}

// TxSize 返回交易大小。
func (s *TrustScorer) TxSize() int {
	return s.txSize
}

// Repeat 返回重复次数。
func (s *TrustScorer) Repeat() int {
	return s.repeat
}
