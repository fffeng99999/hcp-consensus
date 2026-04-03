package ibft

import "strconv"

type ValidatorSelector struct {
	nodeCount int
}

func NewValidatorSelector(cfg Config) *ValidatorSelector {
	return &ValidatorSelector{
		nodeCount: cfg.NodeCount,
	}
}

func (s *ValidatorSelector) GetLeader(round uint64) string {
	if s.nodeCount <= 0 {
		return ""
	}
	index := int(round % uint64(s.nodeCount))
	return "node" + strconv.Itoa(index+1)
}

