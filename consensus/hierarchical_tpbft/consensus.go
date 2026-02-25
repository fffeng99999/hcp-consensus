package hierarchical_tpbft

import (
	"fmt"
	"math"
	"strings"
	"sync"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Config struct {
	NodeCount            int
	GroupCount           int
	GroupSize            int
	MessageBytes         int
	BaseLatencyMs        float64
	PhaseWeightInner     float64
	PhaseWeightOuter     float64
	SigAlgorithm         string
	SigGenMs             float64
	SigVerifyMs          float64
	SigAggMs             float64
	OuterSigMode         string
	OuterSigAlgorithm    string
	OuterSigGenMs        float64
	OuterSigVerifyMs     float64
	OuterSigAggMs        float64
	BatchVerify          bool
	BatchVerifyGain      float64
	SigGenParallelism    float64
	SigVerifyParallelism float64
	SigAggParallelism    float64
	BatchSize            int
}

type HierarchicalTPBFT struct {
	mu      sync.RWMutex
	running bool
	cfg     Config
}

func NewHierarchicalTPBFT(cfg Config) *HierarchicalTPBFT {
	cfg = normalizeConfig(cfg)
	return &HierarchicalTPBFT{cfg: cfg}
}

func (h *HierarchicalTPBFT) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return nil
	}
	h.running = true
	return nil
}

func (h *HierarchicalTPBFT) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.running = false
	return nil
}

func (h *HierarchicalTPBFT) BeginBlock(ctx sdk.Context) {
}

func (h *HierarchicalTPBFT) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	metrics := h.computeMetrics()
	fmt.Printf(
		"hierarchical_tpbft_metrics pre_prepare_ms=%.6f prepare_ms=%.6f commit_ms=%.6f comm_bytes=%.0f total_messages=%.0f sig_gen_count=%.0f sig_verify_count=%.0f sig_gen_time_ms=%.6f sig_verify_time_ms=%.6f aggregation_time_ms=%.6f verify_time_ms=%.6f sig_per_node=%.6f sig_ops_per_tx=%.6f batch_size=%d batch_verify=%.0f verify_gain=%.2f sig_gen_parallelism=%.2f sig_verify_parallelism=%.2f sig_agg_parallelism=%.2f outer_mode=%s algo=%s outer_algo=%s group_count=%d group_size=%d node_count=%d\n",
		metrics.prePrepareMs,
		metrics.prepareMs,
		metrics.commitMs,
		metrics.commBytes,
		metrics.totalMessages,
		metrics.sigGenCount,
		metrics.sigVerifyCount,
		metrics.sigGenTimeMs,
		metrics.sigVerifyTimeMs,
		metrics.aggregationTimeMs,
		metrics.verifyTimeMs,
		metrics.sigPerNode,
		metrics.sigOpsPerTx,
		metrics.batchSize,
		metrics.batchVerify,
		metrics.verifyGain,
		metrics.sigGenParallelism,
		metrics.sigVerifyParallelism,
		metrics.sigAggParallelism,
		metrics.outerMode,
		metrics.sigAlgo,
		metrics.outerAlgo,
		h.cfg.GroupCount,
		h.cfg.GroupSize,
		h.cfg.NodeCount,
	)
	return nil
}

type tpbftMetrics struct {
	prePrepareMs         float64
	prepareMs            float64
	commitMs             float64
	commBytes            float64
	totalMessages        float64
	sigGenCount          float64
	sigVerifyCount       float64
	sigGenTimeMs         float64
	sigVerifyTimeMs      float64
	aggregationTimeMs    float64
	verifyTimeMs         float64
	sigPerNode           float64
	sigOpsPerTx          float64
	batchSize            int
	batchVerify          float64
	verifyGain           float64
	sigGenParallelism    float64
	sigVerifyParallelism float64
	sigAggParallelism    float64
	outerMode            string
	sigAlgo              string
	outerAlgo            string
}

func (h *HierarchicalTPBFT) computeMetrics() tpbftMetrics {
	n := float64(h.cfg.NodeCount)
	g := float64(h.cfg.GroupCount)
	s := float64(h.cfg.GroupSize)
	base := h.cfg.BaseLatencyMs
	innerWeight := h.cfg.PhaseWeightInner
	outerWeight := h.cfg.PhaseWeightOuter
	if base <= 0 {
		base = 1
	}
	if innerWeight <= 0 {
		innerWeight = 1
	}
	if outerWeight <= 0 {
		outerWeight = 1
	}
	if g <= 0 {
		g = 1
	}
	if s <= 0 {
		s = math.Max(1, math.Floor(n/g))
	}

	totalMessages := n + g
	commBytes := totalMessages * float64(h.cfg.MessageBytes)
	outerMode := strings.ToLower(h.cfg.OuterSigMode)
	if outerMode == "" {
		outerMode = "threshold"
	}
	outerAlgo := h.cfg.OuterSigAlgorithm
	if outerAlgo == "" {
		if outerMode == "ed25519" {
			outerAlgo = "ed25519"
		} else {
			outerAlgo = h.cfg.SigAlgorithm
		}
	}
	outerAlgo = strings.ToLower(outerAlgo)
	outerSigGenMs := h.cfg.OuterSigGenMs
	outerSigVerifyMs := h.cfg.OuterSigVerifyMs
	outerSigAggMs := h.cfg.OuterSigAggMs
	if outerSigGenMs <= 0 || outerSigVerifyMs <= 0 || outerSigAggMs <= 0 {
		gen, verify, agg := defaultSigProfile(outerAlgo)
		if outerSigGenMs <= 0 {
			outerSigGenMs = gen
		}
		if outerSigVerifyMs <= 0 {
			outerSigVerifyMs = verify
		}
		if outerSigAggMs <= 0 {
			outerSigAggMs = agg
		}
	}

	innerSigGenOps := s
	innerSigVerifyOps := s
	innerAggOps := 1.0
	outerSigGenOps := 0.0
	outerSigVerifyOps := 0.0
	outerAggOps := 0.0
	switch outerMode {
	case "ed25519":
		outerSigGenOps = g
		outerSigVerifyOps = g
	case "none":
		outerSigVerifyOps = g
	default:
		outerSigGenOps = g
		outerSigVerifyOps = g
		outerAggOps = 1
	}

	sigGenCount := innerSigGenOps + outerSigGenOps
	sigVerifyCount := innerSigVerifyOps + outerSigVerifyOps
	sigGenParallelism := math.Max(1, h.cfg.SigGenParallelism)
	sigVerifyParallelism := math.Max(1, h.cfg.SigVerifyParallelism)
	sigAggParallelism := math.Max(1, h.cfg.SigAggParallelism)
	verifyGain := h.cfg.BatchVerifyGain
	if verifyGain <= 0 {
		verifyGain = 1
	}
	batchVerify := 0.0
	if h.cfg.BatchVerify && verifyGain > 1 {
		batchVerify = 1
	}

	sigGenTime := (innerSigGenOps*h.cfg.SigGenMs + outerSigGenOps*outerSigGenMs) / sigGenParallelism
	sigVerifyTime := (innerSigVerifyOps*h.cfg.SigVerifyMs + outerSigVerifyOps*outerSigVerifyMs) / sigVerifyParallelism
	if batchVerify > 0 {
		sigVerifyTime = sigVerifyTime / verifyGain
	}
	aggTime := (innerAggOps*h.cfg.SigAggMs + outerAggOps*outerSigAggMs) / sigAggParallelism
	sigOpsPerTx := 0.0
	batchSize := h.cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	sigOpsPerTx = (sigGenCount + sigVerifyCount + innerAggOps + outerAggOps) / float64(batchSize)

	pre := base * innerWeight * s
	prepare := base * innerWeight * s
	commit := base * outerWeight * g

	sigPerNode := 0.0
	if n > 0 {
		sigPerNode = 2 + 2/g
	}

	return tpbftMetrics{
		prePrepareMs:         pre,
		prepareMs:            prepare,
		commitMs:             commit,
		commBytes:            commBytes,
		totalMessages:        totalMessages,
		sigGenCount:          sigGenCount,
		sigVerifyCount:       sigVerifyCount,
		sigGenTimeMs:         sigGenTime,
		sigVerifyTimeMs:      sigVerifyTime,
		aggregationTimeMs:    aggTime,
		verifyTimeMs:         sigVerifyTime,
		sigPerNode:           sigPerNode,
		sigOpsPerTx:          sigOpsPerTx,
		batchSize:            batchSize,
		batchVerify:          batchVerify,
		verifyGain:           verifyGain,
		sigGenParallelism:    sigGenParallelism,
		sigVerifyParallelism: sigVerifyParallelism,
		sigAggParallelism:    sigAggParallelism,
		outerMode:            outerMode,
		sigAlgo:              h.cfg.SigAlgorithm,
		outerAlgo:            outerAlgo,
	}
}

func normalizeConfig(cfg Config) Config {
	if cfg.NodeCount <= 0 {
		cfg.NodeCount = 32
	}
	if cfg.GroupCount <= 0 && cfg.GroupSize > 0 {
		cfg.GroupCount = maxInt(1, cfg.NodeCount/cfg.GroupSize)
	}
	if cfg.GroupSize <= 0 && cfg.GroupCount > 0 {
		cfg.GroupSize = maxInt(1, cfg.NodeCount/cfg.GroupCount)
	}
	if cfg.GroupCount <= 0 {
		cfg.GroupCount = cfg.NodeCount
	}
	if cfg.GroupSize <= 0 {
		cfg.GroupSize = 1
	}
	if cfg.MessageBytes <= 0 {
		cfg.MessageBytes = 256
	}
	if cfg.BaseLatencyMs <= 0 {
		cfg.BaseLatencyMs = 1
	}
	if cfg.PhaseWeightInner <= 0 {
		cfg.PhaseWeightInner = 1
	}
	if cfg.PhaseWeightOuter <= 0 {
		cfg.PhaseWeightOuter = 1
	}
	if cfg.SigAlgorithm == "" {
		cfg.SigAlgorithm = "bls"
	}
	cfg.SigAlgorithm = strings.ToLower(cfg.SigAlgorithm)
	defaultGen, defaultVerify, defaultAgg := defaultSigProfile(cfg.SigAlgorithm)
	if cfg.SigGenMs <= 0 {
		cfg.SigGenMs = defaultGen
	}
	if cfg.SigVerifyMs <= 0 {
		cfg.SigVerifyMs = defaultVerify
	}
	if cfg.SigAggMs <= 0 {
		cfg.SigAggMs = defaultAgg
	}
	if cfg.OuterSigMode == "" {
		cfg.OuterSigMode = "threshold"
	}
	if cfg.OuterSigAlgorithm == "" {
		if strings.ToLower(cfg.OuterSigMode) == "ed25519" {
			cfg.OuterSigAlgorithm = "ed25519"
		} else {
			cfg.OuterSigAlgorithm = cfg.SigAlgorithm
		}
	}
	if cfg.BatchVerifyGain <= 0 {
		cfg.BatchVerifyGain = 1
	}
	if cfg.SigGenParallelism <= 0 {
		cfg.SigGenParallelism = 1
	}
	if cfg.SigVerifyParallelism <= 0 {
		cfg.SigVerifyParallelism = 1
	}
	if cfg.SigAggParallelism <= 0 {
		cfg.SigAggParallelism = 1
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 200
	}
	return cfg
}

func defaultSigProfile(algo string) (float64, float64, float64) {
	switch strings.ToLower(algo) {
	case "ed25519":
		return 0.35, 0.65, 1.6
	case "bls":
		fallthrough
	default:
		return 0.6, 1.2, 0.9
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
