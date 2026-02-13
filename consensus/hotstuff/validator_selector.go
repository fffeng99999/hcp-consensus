package hotstuff

import (
	"sort"
)

// ValidatorSelector selects validators for consensus
// For HotStuff, we often rotate leaders.
type ValidatorSelector struct {
	validators []string
}

// NewValidatorSelector creates a new validator selector
func NewValidatorSelector(validators []string) *ValidatorSelector {
	return &ValidatorSelector{
		validators: validators,
	}
}

// GetLeader returns the leader for a given view
func (vs *ValidatorSelector) GetLeader(view uint64) string {
	if len(vs.validators) == 0 {
		return ""
	}
	// Simple Round-Robin
	index := view % uint64(len(vs.validators))
	// Ensure deterministic order
	sort.Strings(vs.validators)
	return vs.validators[index]
}

// UpdateValidators updates the validator set
func (vs *ValidatorSelector) UpdateValidators(validators []string) {
	vs.validators = validators
}
