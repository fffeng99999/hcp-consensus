package tpbft

import (
	"math"
	"sync"
	"time"
)

// NodeTrustScore represents trust evaluation for a validator node
// This is the core innovation of tPBFT algorithm
type NodeTrustScore struct {
	NodeID         string    `json:"node_id"`
	TrustValue     float64   `json:"trust_value"` // Range: 0.0 - 1.0
	EquityScore    int64     `json:"equity_score"`
	SuccessfulTxs  int64     `json:"successful_txs"`
	FailedTxs      int64     `json:"failed_txs"`
	ResponseTime   int64     `json:"response_time_ms"`
	LastUpdateTime time.Time `json:"last_update"`
}

// TrustManager manages trust scores for all validators
type TrustManager struct {
	mu     sync.RWMutex
	scores map[string]*NodeTrustScore
}

// NewTrustManager creates a new trust manager
func NewTrustManager() *TrustManager {
	return &TrustManager{
		scores: make(map[string]*NodeTrustScore),
	}
}

// InitializeNode initializes trust score for a new node
func (tm *TrustManager) InitializeNode(nodeID string, initialEquity int64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.scores[nodeID] = &NodeTrustScore{
		NodeID:         nodeID,
		TrustValue:     1.0, // Start with full trust
		EquityScore:    initialEquity,
		SuccessfulTxs:  0,
		FailedTxs:      0,
		ResponseTime:   0,
		LastUpdateTime: time.Now(),
	}
}

// updateTrustScore calculates trust score based on performance (internal, no lock)
func (tm *TrustManager) updateTrustScore(nodeID string) {
	score, exists := tm.scores[nodeID]
	if !exists {
		return
	}

	// Calculate success rate
	totalTxs := score.SuccessfulTxs + score.FailedTxs
	successRate := 0.0
	if totalTxs > 0 {
		successRate = float64(score.SuccessfulTxs) / float64(totalTxs)
	}

	// Calculate equity weight (normalized to 0-1)
	// Assuming 1,000,000 tokens is max score
	equityWeight := math.Min(float64(score.EquityScore)/1000000.0, 1.0)

	// Calculate response time weight (inverse, lower is better)
	responseWeight := 1.0
	if score.ResponseTime > 0 {
		// Normalize: 1ms = 1.0, 1000ms = 0.0
		responseWeight = math.Max(0.0, 1.0-(float64(score.ResponseTime)/1000.0))
	}

	// Weighted trust calculation
	// Weights: Success(40%) + Equity(30%) + Response(30%)
	score.TrustValue = (successRate * 0.4) + (equityWeight * 0.3) + (responseWeight * 0.3)
	score.LastUpdateTime = time.Now()
}

// UpdateTrustScore updates trust score based on node performance (thread-safe)
func (tm *TrustManager) UpdateTrustScore(nodeID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.updateTrustScore(nodeID)
}

// RecordTransaction records a transaction result for a validator
func (tm *TrustManager) RecordTransaction(nodeID string, success bool, responseTimeMs int64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	score, exists := tm.scores[nodeID]
	if !exists {
		return
	}

	if success {
		score.SuccessfulTxs++
	} else {
		score.FailedTxs++
	}

	// Simple moving average for response time
	if score.ResponseTime == 0 {
		score.ResponseTime = responseTimeMs
	} else {
		score.ResponseTime = (score.ResponseTime + responseTimeMs) / 2
	}

	// Recalculate trust score
	tm.updateTrustScore(nodeID)
}

// GetTrustScore returns the current trust score for a node
func (tm *TrustManager) GetTrustScore(nodeID string) float64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if score, exists := tm.scores[nodeID]; exists {
		return score.TrustValue
	}
	return 0.0
}
