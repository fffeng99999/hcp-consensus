package raft

import (
	"math/rand"
	"sync"
	"time"
)

type TrustScore struct {
	Address        string
	Score          float64
	SuccessCount   int64
	FailureCount   int64
	TimeoutCount   int64
	HeartbeatCount int64
	LastHeartbeat  time.Time
	AvgLatencyMs   float64
	Available      bool
}

type TrustScorer struct {
	baseLatencyMs float64
	jitterMs      float64
	mu            sync.RWMutex
	scores        map[string]*TrustScore
}

func NewTrustScorer(cfg Config) *TrustScorer {
	return &TrustScorer{
		baseLatencyMs: 1.0,
		jitterMs:      0.5,
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

func (ts *TrustScorer) RecordHeartbeat(addr string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	s := ts.getOrCreate(addr)
	s.HeartbeatCount++
	s.LastHeartbeat = time.Now()
	s.Available = true
}

func (ts *TrustScorer) RecordSuccess(addr string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	s := ts.getOrCreate(addr)
	s.SuccessCount++
	s.Score += 0.1
	s.Available = true
}

func (ts *TrustScorer) RecordFailure(addr string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	s := ts.getOrCreate(addr)
	s.FailureCount++
	s.Score -= 1.0
}

func (ts *TrustScorer) RecordTimeout(addr string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	s := ts.getOrCreate(addr)
	s.TimeoutCount++
	s.Score -= 2.0
	s.Available = false
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

func (ts *TrustScorer) IsAvailable(addr string) bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if s, ok := ts.scores[addr]; ok {
		return s.Available
	}
	return false
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
		Address:       addr,
		Score:         1.0,
		LastHeartbeat: time.Now(),
		Available:     true,
	}
	ts.scores[addr] = s
	return s
}
