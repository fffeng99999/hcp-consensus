package raft

import (
	"fmt"
	"sync"
)

type ValidatorEntry struct {
	Address string
	Power   int
}

type ValidatorSelector struct {
	validators []ValidatorEntry
	sorted     bool
	mu         sync.RWMutex
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

func (vs *ValidatorSelector) GetValidators() []string {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	result := make([]string, len(vs.validators))
	for i, v := range vs.validators {
		result[i] = v.Address
	}
	return result
}

func (vs *ValidatorSelector) UpdateValidators(validators []string) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	vs.validators = make([]ValidatorEntry, 0, len(validators))
	for _, addr := range validators {
		vs.validators = append(vs.validators, ValidatorEntry{Address: addr, Power: 1})
	}
	vs.sorted = false
}

func (vs *ValidatorSelector) AddValidator(addr string) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	for _, v := range vs.validators {
		if v.Address == addr {
			return
		}
	}
	vs.validators = append(vs.validators, ValidatorEntry{Address: addr, Power: 1})
	vs.sorted = false
}

func (vs *ValidatorSelector) RemoveValidator(addr string) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	newValidators := make([]ValidatorEntry, 0, len(vs.validators))
	for _, v := range vs.validators {
		if v.Address != addr {
			newValidators = append(newValidators, v)
		}
	}
	vs.validators = newValidators
	vs.sorted = false
}

func (vs *ValidatorSelector) Contains(addr string) bool {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	for _, v := range vs.validators {
		if v.Address == addr {
			return true
		}
	}
	return false
}

func (vs *ValidatorSelector) Count() int {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return len(vs.validators)
}
