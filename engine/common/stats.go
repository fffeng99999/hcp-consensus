package common

import "math"

// ComputeLatencyStats 计算延迟统计（P50/P95/P99）
func ComputeLatencyStats(latencies []float64) (p50, p95, p99 float64) {
	if len(latencies) == 0 {
		return 0, 0, 0
	}
	sorted := make([]float64, len(latencies))
	copy(sorted, latencies)
	// 冒泡排序（数据量小）
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	p50 = percentile(sorted, 0.50)
	p95 = percentile(sorted, 0.95)
	p99 = percentile(sorted, 0.99)
	return
}

// percentile 计算已排序数组的指定分位数
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := math.Ceil(float64(len(sorted)-1) * p)
	return sorted[int(idx)]
}
