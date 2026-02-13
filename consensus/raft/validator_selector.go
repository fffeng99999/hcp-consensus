package raft

// ValidatorSelector selects validators for consensus
// For Raft, the set is typically static or managed via joint consensus.
type ValidatorSelector struct {
	validators []string
}

// NewValidatorSelector creates a new validator selector
func NewValidatorSelector(validators []string) *ValidatorSelector {
	return &ValidatorSelector{
		validators: validators,
	}
}

// GetValidators returns the current validator set
func (vs *ValidatorSelector) GetValidators() []string {
	return vs.validators
}

// UpdateValidators updates the validator set
func (vs *ValidatorSelector) UpdateValidators(validators []string) {
	vs.validators = validators
}
