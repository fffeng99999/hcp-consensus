package pow

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Config struct {
	NodeCount      int
	Difficulty     int
	TargetBlockMs  float64
	TxPerBlock     int
	OrphanBaseRate float64
}

type PoW struct {
	mu      sync.RWMutex
	running bool
	cfg     Config
	rng     *rand.Rand
}

func NewPoW(cfg Config) *PoW {
	cfg = normalizeConfig(cfg)
	return &PoW{
		cfg: cfg,
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func normalizeConfig(cfg Config) Config {
	if cfg.NodeCount <= 0 {
		cfg.NodeCount = 4
	}
	if cfg.Difficulty <= 0 {
		cfg.Difficulty = 12
	}
	if cfg.TargetBlockMs <= 0 {
		cfg.TargetBlockMs = 12000
	}
	if cfg.TxPerBlock <= 0 {
		cfg.TxPerBlock = 1000
	}
	if cfg.OrphanBaseRate < 0 {
		cfg.OrphanBaseRate = 0
	}
	if cfg.OrphanBaseRate > 1 {
		cfg.OrphanBaseRate = 1
	}
	return cfg
}

func (p *PoW) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}
	p.running = true
	return nil
}

func (p *PoW) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = false
	return nil
}

func (p *PoW) BeginBlock(ctx sdk.Context) {
}

func (p *PoW) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	p.mu.RLock()
	cfg := p.cfg
	rng := p.rng
	active := p.running
	p.mu.RUnlock()
	if !active {
		return nil
	}

	scale := 4.0 / float64(cfg.NodeCount)
	jitter := 0.85 + rng.Float64()*0.30
	blockIntervalMs := cfg.TargetBlockMs * scale * jitter
	minInterval := cfg.TargetBlockMs * 0.08
	if blockIntervalMs < minInterval {
		blockIntervalMs = minInterval
	}
	txLatencyMs := blockIntervalMs*(1.0+rng.Float64()*0.35) + float64(cfg.TxPerBlock)*0.002
	hashAttempts := float64(cfg.Difficulty) * 100000.0 * float64(cfg.NodeCount) * jitter
	orphanRate := cfg.OrphanBaseRate + 0.005*float64(cfg.NodeCount-4) + 0.0015*math.Log1p(float64(cfg.Difficulty))
	if orphanRate < 0 {
		orphanRate = 0
	}
	if orphanRate > 0.95 {
		orphanRate = 0.95
	}
	orphanFlag := 0
	if rng.Float64() < orphanRate {
		orphanFlag = 1
	}

	fmt.Printf(
		"pow_metrics block_interval_ms=%.6f tx_latency_ms=%.6f orphan_rate=%.6f orphan_flag=%d difficulty=%d tx_per_block=%d hash_attempts=%.0f node_count=%d height=%d\n",
		blockIntervalMs,
		txLatencyMs,
		orphanRate,
		orphanFlag,
		cfg.Difficulty,
		cfg.TxPerBlock,
		hashAttempts,
		cfg.NodeCount,
		ctx.BlockHeight(),
	)
	return nil
}
