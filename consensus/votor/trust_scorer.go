package votor

import (
	"math"
	"os"
	"strconv"
	"strings"
)

// TrustScorer 复用命名以对齐 tpbft 目录结构，这里承载延迟与聚合耗时模型。
type TrustScorer struct {
	baseLatencyMs  float64
	localTimeoutMs float64
}

// NewTrustScorer 创建 Votor 评分器。
func NewTrustScorer(cfg Config) *TrustScorer {
	return &TrustScorer{
		baseLatencyMs:  cfg.BaseLatencyMs,
		localTimeoutMs: cfg.LocalTimeoutMs,
	}
}

// FinalityLatency 估算 notarize/finalize 时延。
func (s *TrustScorer) FinalityLatency(pathType string, blsAggMs float64) (float64, float64) {
	notarize := 0.0
	finalize := 0.0
	switch pathType {
	case "fast":
		notarize = s.baseLatencyMs + blsAggMs
		finalize = 1.5*s.baseLatencyMs + blsAggMs
	case "slow":
		notarize = s.baseLatencyMs + s.localTimeoutMs + blsAggMs
		finalize = 2*s.baseLatencyMs + s.localTimeoutMs + 2*blsAggMs
	}
	return notarize, finalize
}

// SimulateBLSAggregationMs 估算 BLS 聚合时间。
func (s *TrustScorer) SimulateBLSAggregationMs(nodeCount int) float64 {
	if nodeCount <= 1 {
		return 0.05
	}
	logPart := 0.18 * math.Log2(float64(nodeCount)+1)
	linearPart := 0.02 * float64(nodeCount) / 32.0
	return math.Max(0.05, logPart+linearPart)
}

// normalizeConfig 规范化 Votor 配置。
func normalizeConfig(cfg Config) Config {
	cfg.NodeCount = maxInt(1, readEnvInt("VOTOR_NODE_COUNT", cfg.NodeCount))
	cfg.FaultyRatio = readEnvFloat("VOTOR_SIMULATED_FAULT_RATIO", cfg.FaultyRatio)
	cfg.FastThreshold = readEnvFloat("VOTOR_FAST_THRESHOLD", cfg.FastThreshold)
	cfg.SlowThreshold = readEnvFloat("VOTOR_SLOW_THRESHOLD", cfg.SlowThreshold)
	cfg.LocalTimeoutMs = readEnvFloat("VOTOR_LOCAL_TIMEOUT_MS", cfg.LocalTimeoutMs)
	cfg.BaseLatencyMs = readEnvFloat("VOTOR_BASE_LATENCY_MS", cfg.BaseLatencyMs)

	if cfg.NodeCount <= 0 {
		cfg.NodeCount = 4
	}
	if cfg.FastThreshold <= 0 {
		cfg.FastThreshold = 0.8
	}
	if cfg.SlowThreshold <= 0 {
		cfg.SlowThreshold = 0.6
	}
	if cfg.SlowThreshold > cfg.FastThreshold {
		cfg.SlowThreshold = cfg.FastThreshold
	}
	if cfg.LocalTimeoutMs <= 0 {
		cfg.LocalTimeoutMs = 150
	}
	if cfg.BaseLatencyMs <= 0 {
		cfg.BaseLatencyMs = math.Max(5, cfg.LocalTimeoutMs/5)
	}
	if cfg.SignatureBytes <= 0 {
		cfg.SignatureBytes = 96
	}
	if cfg.HeaderBytes <= 0 {
		cfg.HeaderBytes = 32
	}
	if cfg.CertFixedBytes <= 0 {
		cfg.CertFixedBytes = 192
	}
	cfg.FaultyRatio = clamp01(cfg.FaultyRatio)
	cfg.FastThreshold = clamp01(cfg.FastThreshold)
	cfg.SlowThreshold = clamp01(cfg.SlowThreshold)
	return cfg
}

// readEnvFloat 读取浮点环境变量。
func readEnvFloat(key string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return value
}

// readEnvInt 读取整数环境变量。
func readEnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

// clamp01 将浮点数压缩到 0~1 区间。
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// maxInt 返回较大值。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
