package votor

import (
	"fmt"
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
	node    *Node
}

func NewVotor(cfg Config) *Votor {
	cfg = normalizeConfig(cfg)
	scorer := NewTrustScorer(cfg)
	selector := NewValidatorSelector(cfg)
	node := NewNode(cfg, scorer, selector)
	return &Votor{
		cfg:  cfg,
		node: node,
	}
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
	metrics := v.node.ComputeMetrics()
	fmt.Printf(
		"votor_metrics notarize_latency_ms=%.6f finalize_latency_ms=%.6f path_type=%s bls_agg_ms=%.6f p2p_vote_bytes=%.0f gossip_vote_bytes=%.0f certificate_bytes=%.0f node_count=%d faulty_ratio=%.4f fast_threshold=%.4f slow_threshold=%.4f local_timeout_ms=%.6f height=%d\n",
		metrics.NotarizeLatencyMs,
		metrics.FinalizeLatencyMs,
		metrics.PathType,
		metrics.BlsAggMs,
		metrics.P2PVoteBytes,
		metrics.GossipVoteBytes,
		metrics.CertificateBytes,
		v.cfg.NodeCount,
		v.cfg.FaultyRatio,
		v.cfg.FastThreshold,
		v.cfg.SlowThreshold,
		v.cfg.LocalTimeoutMs,
		ctx.BlockHeight(),
	)
	return nil
}
