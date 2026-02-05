package tpbft

import (
	"fmt"
	"testing"
)

func TestValidatorSelector_SelectValidators(t *testing.T) {
	ts := NewTrustScorer()
	vs := NewValidatorSelector(ts, 0.5, 10)
	
	// Create 10 validators with different scores
	for i := 0; i < 10; i++ {
		addr := fmt.Sprintf("val%d", i)
		// Set scores manually via UpdateScore
		// val0..val9.
		// val0-val6: High score (success)
		// val7-val9: Low score (fail)
		
		success := true
		if i >= 7 {
			success = false
		}
		
		// Update multiple times to stabilize score
		for j := 0; j < 5; j++ {
			ts.UpdateScore(addr, success, 0, 1000, 10000)
		}
	}
	
	allVals := make([]string, 10)
	for i := 0; i < 10; i++ {
		allVals[i] = fmt.Sprintf("val%d", i)
	}
	
	// Select 5 validators
	selected := vs.SelectValidators(allVals, 5)
	
	if len(selected) != 5 {
		t.Errorf("Expected 5 validators, got %d", len(selected))
	}
	
	// Check if top validators are included (due to 70% rule)
	// 5 * 0.7 = 3.5 -> 3 validators should be from top.
	// Top validators are val0..val6 (all have high score).
	// Verify at least some high score validators are present.
	
	highScoreCount := 0
	for _, val := range selected {
		score := ts.GetScore(val)
		if score.TotalScore > 0.6 {
			highScoreCount++
		}
	}
	
	if highScoreCount < 3 {
		t.Errorf("Expected at least 3 high score validators, got %d", highScoreCount)
	}
}
