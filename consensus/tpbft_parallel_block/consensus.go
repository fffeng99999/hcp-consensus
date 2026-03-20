package tpbft_parallel_block

import (
	"fmt"
	"runtime"
	"sync"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/fffeng99999/hcp-consensus/consensus/tpbft"
)

type Config struct {
	SubBlockK int
}

type TPBFTParallelBlock struct {
	base       *tpbft.TPBFT
	cfg        Config
	running    bool
	mu         sync.Mutex
	lastHeight int64
	node       *Node
	scorer     *TrustScorer
	validator  *ValidatorSelector
}

func NewTPBFTParallelBlock(cfg Config) *TPBFTParallelBlock {
	if cfg.SubBlockK <= 0 {
		cfg.SubBlockK = 1
	}
	return &TPBFTParallelBlock{
		base:      tpbft.NewTPBFT(),
		cfg:       cfg,
		node:      NewNode(cfg),
		scorer:    NewTrustScorer(cfg),
		validator: NewValidatorSelector(cfg),
	}
}

func (p *TPBFTParallelBlock) SetStakingKeeper(k tpbft.StakingKeeper) {
	p.base.SetStakingKeeper(k)
}

func (p *TPBFTParallelBlock) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}
	p.running = true
	runtime.GOMAXPROCS(p.cfg.SubBlockK)
	return p.base.Start()
}

func (p *TPBFTParallelBlock) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = false
	return p.base.Stop()
}

func (p *TPBFTParallelBlock) BeginBlock(ctx sdk.Context) {
	p.base.BeginBlock(ctx)
}

func (p *TPBFTParallelBlock) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	return p.base.EndBlock(ctx)
}

func (p *TPBFTParallelBlock) ObserveProposal(height int64, txs [][]byte) {
	p.mu.Lock()
	if !p.running || height <= p.lastHeight {
		p.mu.Unlock()
		return
	}
	p.lastHeight = height
	p.mu.Unlock()
	if len(txs) == 0 {
		return
	}
	totalMs, subMs, mergeMs := p.node.computeOnce(txs)
	totalBytes := 0
	for _, tx := range txs {
		totalBytes += len(tx)
	}
	fmt.Printf(
		"block_time: %.4f ms subblock_time: %.4f ms merge_time: %.4f ms k=%d txs=%d bytes=%d\n",
		totalMs,
		subMs,
		mergeMs,
		p.cfg.SubBlockK,
		len(txs),
		totalBytes,
	)
}
