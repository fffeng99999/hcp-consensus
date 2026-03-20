package votor

import "math"

// ValidatorSelector 复用命名以对齐 tpbft 目录结构，这里负责路径选择。
type ValidatorSelector struct {
	faultyRatio   float64
	fastThreshold float64
	slowThreshold float64
}

// NewValidatorSelector 创建路径选择器。
func NewValidatorSelector(cfg Config) *ValidatorSelector {
	return &ValidatorSelector{
		faultyRatio:   cfg.FaultyRatio,
		fastThreshold: cfg.FastThreshold,
		slowThreshold: cfg.SlowThreshold,
	}
}

// DecidePath 根据诚实节点比例决定 fast/slow/fail 路径。
func (s *ValidatorSelector) DecidePath() string {
	honestRatio := clamp01(1.0 - clamp01(s.faultyRatio))
	if honestRatio > clamp01(s.fastThreshold) {
		return "fast"
	}
	if honestRatio >= clamp01(s.slowThreshold) {
		return "slow"
	}
	return "fail"
}

// EstimateCertificateBytes 估算证书字节数。
func (s *ValidatorSelector) EstimateCertificateBytes(nodeCount int, fixedBytes int, bitmapBytes int) float64 {
	cert := float64(maxInt(0, fixedBytes))
	if bitmapBytes > 0 {
		return cert + float64(bitmapBytes)
	}
	return cert + math.Ceil(float64(maxInt(1, nodeCount))/8.0)
}
