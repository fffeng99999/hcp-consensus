package tpbft

import (
	"sort"
	"sync"
	"time"
)

// TrustScore represents trust evaluation for a validator node
type TrustScore struct {
	ValidatorAddress string    // Validator address
	SuccessRate      float64   // Success rate (0-1)
	StakeWeight      float64   // Stake weight (0-1)
	ResponseSpeed    float64   // Response speed score (0-1)
	TotalScore       float64   // Total score (0-1)
	LastUpdated      time.Time // Last updated time
}

// TrustScorer calculates and manages trust scores
type TrustScorer struct {
	mu              sync.RWMutex
	scores          map[string]*TrustScore     // Validator address -> TrustScore
	successHistory  map[string][]bool          // Success history
	responseHistory map[string][]time.Duration // Response time history

	// Weight configuration
	successWeight float64 // Default 0.4
	stakeWeight   float64 // Default 0.3
	speedWeight   float64 // Default 0.3

	// History window size
	historyWindow int // Default 100
}

// NewTrustScorer creates a new trust scorer
func NewTrustScorer() *TrustScorer {
	return &TrustScorer{
		scores:          make(map[string]*TrustScore),
		successHistory:  make(map[string][]bool),
		responseHistory: make(map[string][]time.Duration),
		successWeight:   0.4,
		stakeWeight:     0.3,
		speedWeight:     0.3,
		historyWindow:   100,
	}
}

// UpdateScore updates trust score for a validator
func (ts *TrustScorer) UpdateScore(
	validatorAddr string,
	success bool,
	responseTime time.Duration,
	stakeAmount float64,
	totalStake float64,
) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// 1. Record history
	ts.recordHistory(validatorAddr, success, responseTime)

	// 2. Calculate success rate
	successRate := ts.calculateSuccessRate(validatorAddr)

	// 3. Calculate stake weight
	stakeWeight := 0.0
	if totalStake > 0 {
		stakeWeight = stakeAmount / totalStake
	}

	// 4. Calculate response speed score
	speedScore := ts.calculateSpeedScore(validatorAddr)

	// 5. Calculate total score
	totalScore := (successRate * ts.successWeight) +
		(stakeWeight * ts.stakeWeight) +
		(speedScore * ts.speedWeight)

	// 6. Update score
	ts.scores[validatorAddr] = &TrustScore{
		ValidatorAddress: validatorAddr,
		SuccessRate:      successRate,
		StakeWeight:      stakeWeight,
		ResponseSpeed:    speedScore,
		TotalScore:       totalScore,
		LastUpdated:      time.Now(),
	}
}

// recordHistory records history data
func (ts *TrustScorer) recordHistory(
	validatorAddr string,
	success bool,
	responseTime time.Duration,
) {
	// Record success/failure
	history := ts.successHistory[validatorAddr]
	history = append(history, success)
	if len(history) > ts.historyWindow {
		history = history[1:] // Keep window size
	}
	ts.successHistory[validatorAddr] = history

	// Record response time
	timeHistory := ts.responseHistory[validatorAddr]
	timeHistory = append(timeHistory, responseTime)
	if len(timeHistory) > ts.historyWindow {
		timeHistory = timeHistory[1:]
	}
	ts.responseHistory[validatorAddr] = timeHistory
}

// calculateSuccessRate calculates success rate
func (ts *TrustScorer) calculateSuccessRate(validatorAddr string) float64 {
	history := ts.successHistory[validatorAddr]
	if len(history) == 0 {
		return 1.0 // New node defaults to trusted
	}

	successCount := 0
	for _, success := range history {
		if success {
			successCount++
		}
	}

	return float64(successCount) / float64(len(history))
}

// calculateSpeedScore calculates speed score
func (ts *TrustScorer) calculateSpeedScore(validatorAddr string) float64 {
	history := ts.responseHistory[validatorAddr]
	if len(history) == 0 {
		return 1.0
	}

	// Calculate average response time
	var totalTime time.Duration
	for _, t := range history {
		totalTime += t
	}
	avgTime := totalTime / time.Duration(len(history))

	// Convert to score (faster is better)
	// Ideal: 100ms, Max tolerance: 1000ms
	idealTime := 100 * time.Millisecond
	maxTime := 1000 * time.Millisecond

	if avgTime <= idealTime {
		return 1.0
	} else if avgTime >= maxTime {
		return 0.1 // Lowest score
	} else {
		// Linear decay
		ratio := float64(avgTime-idealTime) / float64(maxTime-idealTime)
		return 1.0 - (0.9 * ratio)
	}
}

// GetTopValidators returns the top N validators by trust score
func (ts *TrustScorer) GetTopValidators(n int) []string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// Sort by total score
	type validatorScore struct {
		addr  string
		score float64
	}

	var scores []validatorScore
	for addr, score := range ts.scores {
		scores = append(scores, validatorScore{addr, score.TotalScore})
	}

	// Descending sort
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// Return top N
	result := make([]string, 0, n)
	for i := 0; i < n && i < len(scores); i++ {
		result = append(result, scores[i].addr)
	}

	return result
}

// GetScore returns the trust score for a validator
func (ts *TrustScorer) GetScore(validatorAddr string) *TrustScore {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if score, exists := ts.scores[validatorAddr]; exists {
		// Return a copy to avoid race conditions
		scoreCopy := *score
		return &scoreCopy
	}

	// New node default score
	return &TrustScore{
		ValidatorAddress: validatorAddr,
		SuccessRate:      1.0,
		StakeWeight:      0.0,
		ResponseSpeed:    1.0,
		TotalScore:       0.7, // Default medium trust
		LastUpdated:      time.Now(),
	}
}
