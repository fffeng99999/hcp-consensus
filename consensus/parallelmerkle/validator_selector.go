package parallelmerkle

// ValidatorSelector 复用命名以对齐 tpbft 目录结构，这里负责并行切分参数。
type ValidatorSelector struct {
	subBlockK int
}

// NewValidatorSelector 创建并行切分选择器。
func NewValidatorSelector(cfg Config) *ValidatorSelector {
	return &ValidatorSelector{subBlockK: cfg.SubBlockK}
}

// SubBlockK 返回并行子块数量。
func (s *ValidatorSelector) SubBlockK() int {
	if s.subBlockK <= 0 {
		return 1
	}
	return s.subBlockK
}
