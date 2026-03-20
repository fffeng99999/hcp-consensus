package tpbft_parallel_block

// ValidatorSelector 复用命名以对齐 tpbft 目录结构，这里返回区块并行策略。
type ValidatorSelector struct {
	subBlockK int
}

// NewValidatorSelector 创建并行策略选择器。
func NewValidatorSelector(cfg Config) *ValidatorSelector {
	return &ValidatorSelector{subBlockK: cfg.SubBlockK}
}

// SelectParallelism 返回当前轮并行度。
func (s *ValidatorSelector) SelectParallelism() int {
	if s.subBlockK <= 0 {
		return 1
	}
	return s.subBlockK
}
