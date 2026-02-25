package hierarchical

import (
	"fmt"
	"math"
	"sync"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Config struct {
	NodeCount        int
	GroupCount       int
	GroupSize        int
	MessageBytes     int
	BaseLatencyMs    float64
	PhaseWeightInner float64
	PhaseWeightOuter float64
}

type HierarchicalConsensus struct {
	mu      sync.RWMutex
	running bool
	cfg     Config
}

func NewHierarchicalConsensus(cfg Config) *HierarchicalConsensus {
	cfg = normalizeConfig(cfg)
	return &HierarchicalConsensus{cfg: cfg}
}

func (h *HierarchicalConsensus) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return nil
	}
	h.running = true
	return nil
}

func (h *HierarchicalConsensus) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.running = false
	return nil
}

func (h *HierarchicalConsensus) BeginBlock(ctx sdk.Context) {
}

func (h *HierarchicalConsensus) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	pre, prepare, commit, comm := h.computeMetrics()
	fmt.Printf(
		"hierarchical_metrics pre_prepare_ms=%.6f prepare_ms=%.6f commit_ms=%.6f comm_bytes=%.0f group_count=%d group_size=%d node_count=%d\n",
		pre,
		prepare,
		commit,
		comm,
		h.cfg.GroupCount,
		h.cfg.GroupSize,
		h.cfg.NodeCount,
	)
	return nil
}

func (h *HierarchicalConsensus) computeMetrics() (float64, float64, float64, float64) {
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
	comm := (n*n)/g + g*g
	pre := base * innerWeight * s
	prepare := base * innerWeight * (n * n) / g
	commit := base * outerWeight * (g * g)
	return pre, prepare, commit, comm * float64(h.cfg.MessageBytes)
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
	return cfg
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
