package consensus

import (
	"math"
	"sort"
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

// UpdateTrustScore updates trust score based on node performance
// Formula: TrustValue = (SuccessRate * 0.4) + (EquityWeight * 0.3) + (ResponseWeight * 0.3)
func (tm *TrustManager) UpdateTrustScore(nodeID string) {
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
	equityWeight := math.Min(float64(score.EquityScore)/1000000.0, 1.0)

	// Calculate response time weight (inverse, lower is better)
	responseWeight := 1.0
	if score.ResponseTime > 0 {
		// Normalize: 1ms = 1.0, 1000ms = 0.0
		responseWeight = math.Max(0.0, 1.0-(float64(score.ResponseTime)/1000.0))
	}

	// Weighted trust calculation
	score.TrustValue = (successRate * 0.4) + (equityWeight * 0.3) + (responseWeight * 0.3)
	score.LastUpdateTime = time.Now()
}

// RecordTransaction records a transaction result for a validator
func (tm *TrustManager) RecordTransaction(nodeID string, success bool, responseTimeMs int64) {
	score, exists := tm.scores[nodeID]
	if !exists {
		return
	}

	if success {
		score.SuccessfulTxs++
	} else {
		score.FailedTxs++
	}

	// Update average response time
	if score.ResponseTime == 0 {
		score.ResponseTime = responseTimeMs
	} else {
		score.ResponseTime = (score.ResponseTime + responseTimeMs) / 2
	}

	tm.UpdateTrustScore(nodeID)
}

// SelectValidators selects top N validators based on trust scores
// This is the key optimization: only trusted nodes participate in consensus
func (tm *TrustManager) SelectValidators(count int) []string {
	type scorePair struct {
		nodeID string
		score  float64
	}

	var pairs []scorePair
	for nodeID, score := range tm.scores {
		pairs = append(pairs, scorePair{nodeID, score.TrustValue})
	}

	// Sort by trust score descending
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].score > pairs[j].score
	})

	// Select top N
	result := make([]string, 0, count)
	for i := 0; i < count && i < len(pairs); i++ {
		result = append(result, pairs[i].nodeID)
	}

	return result
}

// GetTrustScore returns trust score for a specific node
func (tm *TrustManager) GetTrustScore(nodeID string) (*NodeTrustScore, bool) {
	score, exists := tm.scores[nodeID]
	return score, exists
}

// GetAllScores returns all trust scores
func (tm *TrustManager) GetAllScores() map[string]*NodeTrustScore {
	return tm.scores
}

// ConsensusConfig holds tPBFT consensus configuration
type ConsensusConfig struct {
	TimeoutPropose   time.Duration `json:"timeout_propose"`
	TimeoutPrevote   time.Duration `json:"timeout_prevote"`
	TimeoutPrecommit time.Duration `json:"timeout_precommit"`
	TimeoutCommit    time.Duration `json:"timeout_commit"`
	MinValidators    int           `json:"min_validators"`
	MaxValidators    int           `json:"max_validators"`
}

// DefaultTPBFTConfig returns optimized tPBFT configuration for HFT
func DefaultTPBFTConfig() *ConsensusConfig {
	return &ConsensusConfig{
		TimeoutPropose:   1000 * time.Millisecond, // 1s propose timeout
		TimeoutPrevote:   500 * time.Millisecond,  // 500ms prevote
		TimeoutPrecommit: 500 * time.Millisecond,  // 500ms precommit
		TimeoutCommit:    500 * time.Millisecond,  // 500ms commit
		MinValidators:    4,                        // Minimum 4 validators
		MaxValidators:    7,                        // Maximum 7 validators
	}
}

// RaftConfig returns Raft-style configuration for comparison
func RaftConfig() *ConsensusConfig {
	return &ConsensusConfig{
		TimeoutPropose:   3000 * time.Millisecond, // Longer propose timeout
		TimeoutPrevote:   1000 * time.Millisecond,
		TimeoutPrecommit: 1000 * time.Millisecond,
		TimeoutCommit:    5000 * time.Millisecond, // Much longer commit
		MinValidators:    3,
		MaxValidators:    7,
	}
}

// HotStuffConfig returns HotStuff-style configuration
func HotStuffConfig() *ConsensusConfig {
	return &ConsensusConfig{
		TimeoutPropose:   2000 * time.Millisecond,
		TimeoutPrevote:   800 * time.Millisecond,
		TimeoutPrecommit: 800 * time.Millisecond,
		TimeoutCommit:    2000 * time.Millisecond,
		MinValidators:    4,
		MaxValidators:    7,
	}
}
