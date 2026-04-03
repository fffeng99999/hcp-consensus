package ibft

import (
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
)

type TrustScorer struct {
	baseLatencyMs float64
	jitterMs      float64
}

func NewTrustScorer(cfg Config) *TrustScorer {
	return &TrustScorer{
		baseLatencyMs: cfg.BaseLatencyMs,
		jitterMs:      cfg.JitterMs,
	}
}

func (s *TrustScorer) SampleNetworkDelayMs(rng *rand.Rand) float64 {
	if rng == nil {
		return s.baseLatencyMs
	}
	if s.jitterMs <= 0 {
		return s.baseLatencyMs
	}
	return s.baseLatencyMs + rng.Float64()*s.jitterMs
}

func normalizeConfig(cfg Config) Config {
	cfg.NodeCount = maxInt(1, readEnvInt("IBFT_NODE_COUNT", cfg.NodeCount))
	cfg.FaultyRatio = clamp01(readEnvFloat("IBFT_FAULTY_RATIO", cfg.FaultyRatio))
	cfg.BaseLatencyMs = readEnvFloat("IBFT_BASE_LATENCY_MS", cfg.BaseLatencyMs)
	cfg.JitterMs = readEnvFloat("IBFT_JITTER_MS", cfg.JitterMs)
	cfg.TimeoutMs = readEnvFloat("IBFT_TIMEOUT_MS", cfg.TimeoutMs)
	cfg.MessageBytes = readEnvInt("IBFT_MESSAGE_BYTES", cfg.MessageBytes)
	cfg.MaxRounds = readEnvInt("IBFT_MAX_ROUNDS", cfg.MaxRounds)

	if cfg.NodeCount <= 0 {
		cfg.NodeCount = 4
	}
	if cfg.BaseLatencyMs <= 0 {
		cfg.BaseLatencyMs = 5
	}
	if cfg.JitterMs < 0 {
		cfg.JitterMs = 0
	}
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = math.Max(50, cfg.BaseLatencyMs*4)
	}
	if cfg.MessageBytes <= 0 {
		cfg.MessageBytes = 256
	}
	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = 8
	}
	return cfg
}

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

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

