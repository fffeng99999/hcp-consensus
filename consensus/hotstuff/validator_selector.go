package hotstuff

import (
	"fmt"
	"sort"
)

type ValidatorEntry struct {
	Address string
	Power   int64
}

type ValidatorSelector struct {
	validators []ValidatorEntry
	sorted     bool
}

func NewValidatorSelector(cfg Config) *ValidatorSelector {
	addresses := make([]string, cfg.NodeCount)
	for i := 0; i < cfg.NodeCount; i++ {
		addresses[i] = fmt.Sprintf("node%d", i)
	}

	entries := make([]ValidatorEntry, 0, len(addresses))
	for _, addr := range addresses {
		entries = append(entries, ValidatorEntry{Address: addr, Power: 1})
	}
	return &ValidatorSelector{
		validators: entries,
		sorted:     false,
	}
}

func NewWeightedSelector(validators []ValidatorEntry) *ValidatorSelector {
	vs := &ValidatorSelector{
		validators: validators,
		sorted:     false,
	}
	vs.ensureSorted()
	return vs
}

func (vs *ValidatorSelector) GetLeader(view uint64) string {
	vs.ensureSorted()

	if len(vs.validators) == 0 {
		return ""
	}

	var totalPower int64
	for _, v := range vs.validators {
		totalPower += v.Power
	}

	if totalPower == 0 {
		return vs.validators[0].Address
	}

	target := view % uint64(totalPower)
	var cumulative int64
	for _, v := range vs.validators {
		cumulative += v.Power
		if uint64(cumulative) > target {
			return v.Address
		}
	}

	return vs.validators[len(vs.validators)-1].Address
}

func (vs *ValidatorSelector) GetLeaderByRoundRobin(view uint64) string {
	vs.ensureSorted()

	if len(vs.validators) == 0 {
		return ""
	}

	index := view % uint64(len(vs.validators))
	return vs.validators[index].Address
}

func (vs *ValidatorSelector) UpdateValidators(addresses []string) {
	entries := make([]ValidatorEntry, 0, len(addresses))
	for _, addr := range addresses {
		entries = append(entries, ValidatorEntry{Address: addr, Power: 1})
	}
	vs.validators = entries
	vs.sorted = false
}

func (vs *ValidatorSelector) UpdateValidatorsWithPower(validators []ValidatorEntry) {
	vs.validators = validators
	vs.sorted = false
}

func (vs *ValidatorSelector) GetValidators() []ValidatorEntry {
	vs.ensureSorted()
	return vs.validators
}

func (vs *ValidatorSelector) ValidatorCount() int {
	return len(vs.validators)
}

func (vs *ValidatorSelector) TotalPower() int64 {
	vs.ensureSorted()
	var total int64
	for _, v := range vs.validators {
		total += v.Power
	}
	return total
}

func (vs *ValidatorSelector) ensureSorted() {
	if vs.sorted {
		return
	}
	sort.Slice(vs.validators, func(i, j int) bool {
		return vs.validators[i].Address < vs.validators[j].Address
	})
	vs.sorted = true
}
