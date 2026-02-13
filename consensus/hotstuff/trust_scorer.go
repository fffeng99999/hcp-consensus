package hotstuff

import (
	"sync"
)

// TrustScore represents a simple trust score
type TrustScore struct {
	ValidatorAddress string
	Score            float64
}

// TrustScorer is a simplified trust scorer for HotStuff
// HotStuff typically relies on rotating leaders rather than weighted trust,
// but we can keep track of performance.
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

	if _, ok := ts.scores[addr]; !ok {
		ts.scores[addr] = &TrustScore{ValidatorAddress: addr, Score: 0}
	}
	ts.scores[addr].Score += 1.0
}

func (ts *TrustScorer) RecordFailure(addr string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if _, ok := ts.scores[addr]; !ok {
		ts.scores[addr] = &TrustScore{ValidatorAddress: addr, Score: 0}
	}
	ts.scores[addr].Score -= 1.0
}

// GetScore returns the score for a validator
func (ts *TrustScorer) GetScore(addr string) float64 {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if s, ok := ts.scores[addr]; ok {
		return s.Score
	}
	return 0.0
}
