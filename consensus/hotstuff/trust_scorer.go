package hotstuff

import (
	"math/rand"
	"sync"
	"time"
)

type TrustScore struct {
	ValidatorAddress string
	Score            float64
	SuccessCount     int64
	FailureCount     int64
	TimeoutCount     int64
	LastActive       time.Time
	AvgLatencyMs     float64
}

type TrustScorer struct {
	baseLatencyMs float64
	jitterMs      float64
	mu            sync.RWMutex
	scores        map[string]*TrustScore
}

func NewTrustScorer(cfg Config) *TrustScorer {
	return &TrustScorer{
		baseLatencyMs: cfg.BaseLatencyMs,
		jitterMs:      cfg.JitterMs,
		scores:        make(map[string]*TrustScore),
	}
}

func (s *TrustScorer) SampleNetworkDelayMs(rng *rand.Rand) float64 {
	if rng == nil {
		return s.baseLatencyMs
	}
	if s.jitterMs <= 0 {
		return s.baseLatencyMs
	}
	return s.baseLatencyMs + rng.Float64()*s.jitterMs
}

func (ts *TrustScorer) RecordSuccess(addr string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	s := ts.getOrCreate(addr)
	s.SuccessCount++
	s.Score += 1.0
	s.LastActive = time.Now()
}

func (ts *TrustScorer) RecordFailure(addr string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	s := ts.getOrCreate(addr)
	s.FailureCount++
	s.Score -= 1.0
	s.LastActive = time.Now()
}

func (ts *TrustScorer) RecordTimeout(addr string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	s := ts.getOrCreate(addr)
	s.TimeoutCount++
	s.Score -= 2.0
	s.LastActive = time.Now()
}

func (ts *TrustScorer) RecordLatency(addr string, latencyMs float64) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	s := ts.getOrCreate(addr)
	if s.AvgLatencyMs == 0 {
		s.AvgLatencyMs = latencyMs
	} else {
		s.AvgLatencyMs = 0.9*s.AvgLatencyMs + 0.1*latencyMs
	}
	s.LastActive = time.Now()
}

func (ts *TrustScorer) GetScore(addr string) float64 {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if s, ok := ts.scores[addr]; ok {
		return s.Score
	}
	return 0.0
}

func (ts *TrustScorer) GetStats(addr string) *TrustScore {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if s, ok := ts.scores[addr]; ok {
		return s
	}
	return nil
}

func (ts *TrustScorer) GetTopValidators(limit int) []*TrustScore {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	all := make([]*TrustScore, 0, len(ts.scores))
	for _, s := range ts.scores {
		all = append(all, s)
	}

	for i := 0; i < len(all) && i < limit; i++ {
		maxIdx := i
		for j := i + 1; j < len(all); j++ {
			if all[j].Score > all[maxIdx].Score {
				maxIdx = j
			}
		}
		if maxIdx != i {
			all[i], all[maxIdx] = all[maxIdx], all[i]
		}
	}

	if limit < len(all) {
		return all[:limit]
	}
	return all
}

func (ts *TrustScorer) Reset() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.scores = make(map[string]*TrustScore)
}

func (ts *TrustScorer) getOrCreate(addr string) *TrustScore {
	if s, ok := ts.scores[addr]; ok {
		return s
	}
	s := &TrustScore{
		ValidatorAddress: addr,
		Score:            0,
		LastActive:       time.Now(),
	}
	ts.scores[addr] = s
	return s
}
