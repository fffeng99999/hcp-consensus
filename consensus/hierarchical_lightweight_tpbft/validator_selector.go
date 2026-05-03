package hierarchical_lightweight_tpbft

// ValidatorSelector 复用命名以对齐 tpbft 目录结构，这里用于分组形态推导。
type ValidatorSelector struct {
	nodeCount  int
	groupCount int
	groupSize  int
}

// NewValidatorSelector 创建分层验证者选择器。
func NewValidatorSelector(cfg Config) *ValidatorSelector {
	return &ValidatorSelector{
		nodeCount:  cfg.NodeCount,
		groupCount: cfg.GroupCount,
		groupSize:  cfg.GroupSize,
	}
}

// ResolveGroupShape 返回规范化后的分组数量与组大小。
func (s *ValidatorSelector) ResolveGroupShape() (int, int) {
	nodeCount := s.nodeCount
	groupCount := s.groupCount
	groupSize := s.groupSize
	if nodeCount <= 0 {
		nodeCount = 1
	}
	if groupCount <= 0 && groupSize > 0 {
		groupCount = maxInt(1, nodeCount/groupSize)
	}
	if groupSize <= 0 && groupCount > 0 {
		groupSize = maxInt(1, nodeCount/groupCount)
	}
	if groupCount <= 0 {
		groupCount = nodeCount
	}
	if groupSize <= 0 {
		groupSize = 1
	}
	return groupCount, groupSize
}

// maxInt 返回两个整数中的较大值。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
