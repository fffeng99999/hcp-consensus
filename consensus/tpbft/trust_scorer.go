package tpbft

import (
	"sort"
	"sync"
	"time"
)

// TrustScore 表示对验证人节点的信任度评估
type TrustScore struct {
	ValidatorAddress string    // 验证人地址
	SuccessRate      float64   // 成功率（0-1）
	StakeWeight      float64   // 质押权重（0-1）
	ResponseSpeed    float64   // 响应速度评分（0-1）
	TotalScore       float64   // 总体评分（0-1）
	LastUpdated      time.Time // 最近更新时间
}

// TrustScorer 负责计算并管理验证人的信任分数
type TrustScorer struct {
	mu              sync.RWMutex
	scores          map[string]*TrustScore     // Validator address -> TrustScore
	successHistory  map[string][]bool          // Success history
	responseHistory map[string][]time.Duration // Response time history

	// 权重配置
	successWeight float64 // 成功率权重（默认 0.4）
	stakeWeight   float64 // 质押权重（默认 0.3）
	speedWeight   float64 // 速度权重（默认 0.3）

	// 历史窗口大小
	historyWindow int // 默认 100
}

// NewTrustScorer 创建一个新的信任评分器
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

// UpdateScore 更新指定验证人的信任分数
func (ts *TrustScorer) UpdateScore(
	validatorAddr string,
	success bool,
	responseTime time.Duration,
	stakeAmount float64,
	totalStake float64,
) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// 1. 记录历史
	ts.recordHistory(validatorAddr, success, responseTime)

	// 2. 计算成功率
	successRate := ts.calculateSuccessRate(validatorAddr)

	// 3. 计算质押权重
	stakeWeight := 0.0
	if totalStake > 0 {
		stakeWeight = stakeAmount / totalStake
	}

	// 4. 计算响应速度评分
	speedScore := ts.calculateSpeedScore(validatorAddr)

	// 5. 计算总体评分
	totalScore := (successRate * ts.successWeight) +
		(stakeWeight * ts.stakeWeight) +
		(speedScore * ts.speedWeight)

	// 6. 更新评分记录
	ts.scores[validatorAddr] = &TrustScore{
		ValidatorAddress: validatorAddr,
		SuccessRate:      successRate,
		StakeWeight:      stakeWeight,
		ResponseSpeed:    speedScore,
		TotalScore:       totalScore,
		LastUpdated:      time.Now(),
	}
}

// recordHistory 记录成功与响应时间的历史数据
func (ts *TrustScorer) recordHistory(
	validatorAddr string,
	success bool,
	responseTime time.Duration,
) {
	// 记录成功/失败历史
	history := ts.successHistory[validatorAddr]
	history = append(history, success)
	if len(history) > ts.historyWindow {
		history = history[1:] // Keep window size
	}
	ts.successHistory[validatorAddr] = history

	// 记录响应时间历史
	timeHistory := ts.responseHistory[validatorAddr]
	timeHistory = append(timeHistory, responseTime)
	if len(timeHistory) > ts.historyWindow {
		timeHistory = timeHistory[1:]
	}
	ts.responseHistory[validatorAddr] = timeHistory
}

// calculateSuccessRate 计算成功率
func (ts *TrustScorer) calculateSuccessRate(validatorAddr string) float64 {
	history := ts.successHistory[validatorAddr]
	if len(history) == 0 {
		return 1.0 // 新节点默认被认为可信
	}

	successCount := 0
	for _, success := range history {
		if success {
			successCount++
		}
	}

	return float64(successCount) / float64(len(history))
}

// calculateSpeedScore 计算响应速度评分
func (ts *TrustScorer) calculateSpeedScore(validatorAddr string) float64 {
	history := ts.responseHistory[validatorAddr]
	if len(history) == 0 {
		return 1.0
	}

	// 计算平均响应时间
	var totalTime time.Duration
	for _, t := range history {
		totalTime += t
	}
	avgTime := totalTime / time.Duration(len(history))

	// 将平均响应时间转换为评分（越快越好）
	// 理想值：100ms，最大容忍：1000ms
	idealTime := 100 * time.Millisecond
	maxTime := 1000 * time.Millisecond

	if avgTime <= idealTime {
		return 1.0
	} else if avgTime >= maxTime {
		return 0.1 // 最低评分
	} else {
		// 线性衰减模型
		ratio := float64(avgTime-idealTime) / float64(maxTime-idealTime)
		return 1.0 - (0.9 * ratio)
	}
}

// GetTopValidators 返回信任分数最高的前 N 个验证人地址
func (ts *TrustScorer) GetTopValidators(n int) []string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// 按总评分排序
	type validatorScore struct {
		addr  string
		score float64
	}

	var scores []validatorScore
	for addr, score := range ts.scores {
		scores = append(scores, validatorScore{addr, score.TotalScore})
	}

	// 按分数降序排序
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// 返回前 N 项
	result := make([]string, 0, n)
	for i := 0; i < n && i < len(scores); i++ {
		result = append(result, scores[i].addr)
	}

	return result
}

// GetScore 返回指定验证人的信任分数
func (ts *TrustScorer) GetScore(validatorAddr string) *TrustScore {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if score, exists := ts.scores[validatorAddr]; exists {
		// 返回副本以避免数据竞争
		scoreCopy := *score
		return &scoreCopy
	}

	// 新节点的默认评分
	return &TrustScore{
		ValidatorAddress: validatorAddr,
		SuccessRate:      1.0,
		StakeWeight:      0.0,
		ResponseSpeed:    1.0,
		TotalScore:       0.7, // Default medium trust
		LastUpdated:      time.Now(),
	}
}
