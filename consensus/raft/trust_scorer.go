package raft

import (
	"sync"
)

// TrustScore represents a simple trust score
type TrustScore struct {
	ValidatorAddress string
	Score            float64
}

// TrustScorer is a placeholder trust scorer for Raft
// Raft relies on leader election, not weighted trust scores.
type TrustScorer struct {
	mu     sync.RWMutex
	scores map[string]*TrustScore
}

func NewTrustScorer() *TrustScorer {
	return &TrustScorer{
		scores: make(map[string]*TrustScore),
	}
}

func (ts *TrustScorer) RecordSuccess(addr string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	// No-op or metrics
}

func (ts *TrustScorer) GetScore(addr string) float64 {
	return 1.0
}
