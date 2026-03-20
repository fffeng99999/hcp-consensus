package tpbft_parallel_block

// TrustScorer 复用命名以对齐 tpbft 目录结构，这里保存并行参数。
type TrustScorer struct {
	subBlockK int
}

// NewTrustScorer 创建并行评分器。
func NewTrustScorer(cfg Config) *TrustScorer {
	return &TrustScorer{subBlockK: cfg.SubBlockK}
}

// SubBlockK 返回并行子块数量。
func (s *TrustScorer) SubBlockK() int {
	if s.subBlockK <= 0 {
		return 1
	}
	return s.subBlockK
}
