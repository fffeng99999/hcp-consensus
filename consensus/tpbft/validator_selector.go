package tpbft

import (
	"math/rand"
	"sort"
)

// ValidatorSelector 负责为共识过程选择参与的验证人
type ValidatorSelector struct {
	trustScorer   *TrustScorer
	minTrustScore float64 // 最小信任分数阈值（默认 0.6）
	maxValidators int     // 最大验证人数量
}

// NewValidatorSelector 创建新的验证人选择器
func NewValidatorSelector(scorer *TrustScorer, minTrust float64, maxVals int) *ValidatorSelector {
	return &ValidatorSelector{
		trustScorer:   scorer,
		minTrustScore: minTrust,
		maxValidators: maxVals,
	}
}

// SelectValidators 为共识挑选指定数量的验证人
func (vs *ValidatorSelector) SelectValidators(
	allValidators []string,
	requiredCount int,
) []string {

	// 1. 过滤：只保留满足信任阈值的验证人
	qualified := vs.filterQualifiedValidators(allValidators)

	// 2. 若合格验证人数量不足，则退化为使用全部验证人
	if len(qualified) < requiredCount {
		qualified = allValidators
	}

	// 3. 按信任分数从高到低排序
	sortedVals := vs.sortByTrustScore(qualified)

	// 4. 选择前 N 名高信任验证人
	if len(sortedVals) <= requiredCount {
		return sortedVals
	}

	// 5. 引入一定随机性，避免每次选择完全相同集合
	return vs.selectWithRandomness(sortedVals, requiredCount)
}

// filterQualifiedValidators 过滤出满足信任分数阈值的验证人
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

// sortByTrustScore 根据信任分数对验证人排序（降序）
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

	// 按分数降序排序
	sort.Slice(valsWithScores, func(i, j int) bool {
		return valsWithScores[i].score > valsWithScores[j].score
	})

	result := make([]string, len(validators))
	for i, v := range valsWithScores {
		result[i] = v.addr
	}

	return result
}

// selectWithRandomness 按一定比例从高分验证人和剩余验证人中随机选择
// 其中 70% 来自高分段，30% 从剩余验证人中随机选择
func (vs *ValidatorSelector) selectWithRandomness(
	sortedValidators []string,
	count int,
) []string {
	selected := make([]string, 0, count)

	// 70% 从高分验证人中直接选取
	highScoreCount := int(float64(count) * 0.7)
	for i := 0; i < highScoreCount && i < len(sortedValidators); i++ {
		selected = append(selected, sortedValidators[i])
	}

	// 30% 从剩余验证人中随机选取
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
