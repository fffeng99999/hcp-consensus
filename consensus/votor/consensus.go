package votor

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Config struct {
	NodeCount       int
	FaultyRatio     float64
	FastThreshold   float64
	SlowThreshold   float64
	LocalTimeoutMs  float64
	BaseLatencyMs   float64
	SignatureBytes  int
	HeaderBytes     int
	CertFixedBytes  int
	CertBitmapBytes int
}

type Votor struct {
	mu      sync.RWMutex
	running bool
	cfg     Config
}

func NewVotor(cfg Config) *Votor {
	cfg = normalizeConfig(cfg)
	return &Votor{cfg: cfg}
}

func (v *Votor) Start() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.running {
		return nil
	}
	v.running = true
	return nil
}

func (v *Votor) Stop() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.running = false
	return nil
}

func (v *Votor) BeginBlock(ctx sdk.Context) {
}

func (v *Votor) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	metrics := v.computeMetrics()
	fmt.Printf(
		"votor_metrics notarize_latency_ms=%.6f finalize_latency_ms=%.6f path_type=%s bls_agg_ms=%.6f p2p_vote_bytes=%.0f gossip_vote_bytes=%.0f certificate_bytes=%.0f node_count=%d faulty_ratio=%.4f fast_threshold=%.4f slow_threshold=%.4f local_timeout_ms=%.6f height=%d\n",
		metrics.notarizeLatencyMs,
		metrics.finalizeLatencyMs,
		metrics.pathType,
		metrics.blsAggMs,
		metrics.p2pVoteBytes,
		metrics.gossipVoteBytes,
		metrics.certificateBytes,
		v.cfg.NodeCount,
		v.cfg.FaultyRatio,
		v.cfg.FastThreshold,
		v.cfg.SlowThreshold,
		v.cfg.LocalTimeoutMs,
		ctx.BlockHeight(),
	)
	return nil
}

type votorMetrics struct {
	notarizeLatencyMs float64
	finalizeLatencyMs float64
	blsAggMs          float64
	p2pVoteBytes      float64
	gossipVoteBytes   float64
	certificateBytes  float64
	pathType          string
}

func (v *Votor) computeMetrics() votorMetrics {
	n := float64(maxInt(1, v.cfg.NodeCount))
	faulty := clamp01(v.cfg.FaultyRatio)
	honest := clamp01(1.0 - faulty)

	fastOk := honest > clamp01(v.cfg.FastThreshold)
	slowOk := honest >= clamp01(v.cfg.SlowThreshold)

	pathType := "fail"
	if fastOk {
		pathType = "fast"
	} else if slowOk {
		pathType = "slow"
	}

	base := v.cfg.BaseLatencyMs
	if base <= 0 {
		base = math.Max(5, v.cfg.LocalTimeoutMs/5)
	}

	blsAgg := v.simulateBlsAggregationMs(int(n))

	notarize := 0.0
	finalize := 0.0
	if pathType == "fast" {
		notarize = base + blsAgg
		finalize = 1.5*base + blsAgg
	} else if pathType == "slow" {
		notarize = base + v.cfg.LocalTimeoutMs + blsAgg
		finalize = 2*base + v.cfg.LocalTimeoutMs + 2*blsAgg
	}

	sigBytes := float64(maxInt(0, v.cfg.SignatureBytes))
	headerBytes := float64(maxInt(0, v.cfg.HeaderBytes))
	voteBytes := sigBytes + headerBytes
	p2pVotes := n * voteBytes
	gossipVotes := n * n * voteBytes

	cert := float64(maxInt(0, v.cfg.CertFixedBytes))
	if v.cfg.CertBitmapBytes > 0 {
		cert += float64(v.cfg.CertBitmapBytes)
	} else {
		cert += math.Ceil(n / 8.0)
	}

	return votorMetrics{
		notarizeLatencyMs: notarize,
		finalizeLatencyMs: finalize,
		blsAggMs:          blsAgg,
		p2pVoteBytes:      p2pVotes,
		gossipVoteBytes:   gossipVotes,
		certificateBytes:  cert,
		pathType:          pathType,
	}
}

func (v *Votor) simulateBlsAggregationMs(n int) float64 {
	if n <= 1 {
		return 0.05
	}
	logPart := 0.18 * math.Log2(float64(n)+1)
	linearPart := 0.02 * float64(n) / 32.0
	return math.Max(0.05, logPart+linearPart)
}

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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
