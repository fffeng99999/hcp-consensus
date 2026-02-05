package tpbft

import (
	"testing"
	"time"
)

func TestTrustScorer_UpdateScore(t *testing.T) {
	ts := NewTrustScorer()
	valAddr := "validator1"

	// Initial score should be default (medium trust)
	initialScore := ts.GetScore(valAddr)
	if initialScore.TotalScore != 0.7 {
		t.Errorf("Expected initial score 0.7, got %f", initialScore.TotalScore)
	}

	// Update with success
	ts.UpdateScore(valAddr, true, 100*time.Millisecond, 1000, 10000)

	score := ts.GetScore(valAddr)
	if score.SuccessRate != 1.0 {
		t.Errorf("Expected success rate 1.0, got %f", score.SuccessRate)
	}
	
	// Check calculation
	// SuccessRate: 1.0 * 0.4 = 0.4
	// Stake: 0.1 * 0.3 = 0.03
	// Speed: 1.0 * 0.3 = 0.3
	// Total: 0.73
	expectedScore := 0.4 + 0.03 + 0.3
	if score.TotalScore != expectedScore {
		t.Errorf("Expected total score %f, got %f", expectedScore, score.TotalScore)
	}
}

func TestTrustScorer_HistoryWindow(t *testing.T) {
	ts := NewTrustScorer()
	ts.historyWindow = 5
	valAddr := "validator1"

	// Add 5 successes
	for i := 0; i < 5; i++ {
		ts.UpdateScore(valAddr, true, 100*time.Millisecond, 1000, 10000)
	}
	
	if len(ts.successHistory[valAddr]) != 5 {
		t.Errorf("Expected history length 5, got %d", len(ts.successHistory[valAddr]))
	}

	// Add 1 failure
	ts.UpdateScore(valAddr, false, 100*time.Millisecond, 1000, 10000)

	if len(ts.successHistory[valAddr]) != 5 {
		t.Errorf("Expected history length 5 (window size), got %d", len(ts.successHistory[valAddr]))
	}
	
	// Last one should be false
	history := ts.successHistory[valAddr]
	if history[4] != false {
		t.Errorf("Expected last entry to be false")
	}
	// First one (oldest) should have been removed. 
	// Before: T T T T T
	// After: T T T T F
}
