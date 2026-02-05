package tpbft

import (
	"math/rand"
	"sort"
)

// ValidatorSelector selects validators for consensus
type ValidatorSelector struct {
	trustScorer   *TrustScorer
	minTrustScore float64 // Minimum trust score threshold (default 0.6)
	maxValidators int     // Maximum number of validators
}

// NewValidatorSelector creates a new validator selector
func NewValidatorSelector(scorer *TrustScorer, minTrust float64, maxVals int) *ValidatorSelector {
	return &ValidatorSelector{
		trustScorer:   scorer,
		minTrustScore: minTrust,
		maxValidators: maxVals,
	}
}

// SelectValidators selects validators for consensus participation
func (vs *ValidatorSelector) SelectValidators(
	allValidators []string,
	requiredCount int,
) []string {

	// 1. Filter: keep only qualified validators
	qualified := vs.filterQualifiedValidators(allValidators)

	// 2. If not enough qualified, lower threshold or use all
	if len(qualified) < requiredCount {
		qualified = allValidators
	}

	// 3. Sort by trust score
	sortedVals := vs.sortByTrustScore(qualified)

	// 4. Select top N high trust validators
	if len(sortedVals) <= requiredCount {
		return sortedVals
	}

	// 5. Introduce randomness to avoid selecting same validators always
	return vs.selectWithRandomness(sortedVals, requiredCount)
}

// filterQualifiedValidators filters validators meeting trust threshold
func (vs *ValidatorSelector) filterQualifiedValidators(validators []string) []string {
	var qualified []string

	for _, val := range validators {
		score := vs.trustScorer.GetScore(val)
		if score.TotalScore >= vs.minTrustScore {
			qualified = append(qualified, val)
		}
	}

	return qualified
}

// sortByTrustScore sorts validators by trust score
func (vs *ValidatorSelector) sortByTrustScore(validators []string) []string {
	type valWithScore struct {
		addr  string
		score float64
	}

	valsWithScores := make([]valWithScore, len(validators))
	for i, val := range validators {
		score := vs.trustScorer.GetScore(val)
		valsWithScores[i] = valWithScore{val, score.TotalScore}
	}

	// Descending sort
	sort.Slice(valsWithScores, func(i, j int) bool {
		return valsWithScores[i].score > valsWithScores[j].score
	})

	result := make([]string, len(validators))
	for i, v := range valsWithScores {
		result[i] = v.addr
	}

	return result
}

// selectWithRandomness selects validators with some randomness
// 70% from top scores, 30% random from remaining
func (vs *ValidatorSelector) selectWithRandomness(
	sortedValidators []string,
	count int,
) []string {
	selected := make([]string, 0, count)

	// 70% from high score validators
	highScoreCount := int(float64(count) * 0.7)
	for i := 0; i < highScoreCount && i < len(sortedValidators); i++ {
		selected = append(selected, sortedValidators[i])
	}

	// 30% random selection (from remaining validators)
	remaining := sortedValidators[highScoreCount:]
	if len(remaining) > 0 {
		rand.Shuffle(len(remaining), func(i, j int) {
			remaining[i], remaining[j] = remaining[j], remaining[i]
		})

		randomCount := count - highScoreCount
		for i := 0; i < randomCount && i < len(remaining); i++ {
			selected = append(selected, remaining[i])
		}
	}

	return selected
}
