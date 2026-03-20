package parallelmerkle

import (
	"fmt"
	"runtime"
	"sync"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Config struct {
	TxCount   int
	TxSize    int
	SubBlockK int
	Repeat    int
}

type ParallelMerkleConsensus struct {
	cfg       Config
	running   bool
	mu        sync.Mutex
	node      *Node
	scorer    *TrustScorer
	validator *ValidatorSelector
}

func NewParallelMerkleConsensus(cfg Config) *ParallelMerkleConsensus {
	if cfg.TxCount <= 0 {
		cfg.TxCount = 1000
	}
	if cfg.TxSize <= 0 {
		cfg.TxSize = 512
	}
	if cfg.SubBlockK <= 0 {
		cfg.SubBlockK = 1
	}
	if cfg.Repeat <= 0 {
		cfg.Repeat = 1
	}
	return &ParallelMerkleConsensus{
		cfg:       cfg,
		node:      NewNode(cfg),
		scorer:    NewTrustScorer(cfg),
		validator: NewValidatorSelector(cfg),
	}
}

func (p *ParallelMerkleConsensus) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}
	p.running = true
	runtime.GOMAXPROCS(p.cfg.SubBlockK)
	return nil
}

func (p *ParallelMerkleConsensus) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = false
	return nil
}

func (p *ParallelMerkleConsensus) BeginBlock(ctx sdk.Context) {
	p.mu.Lock()
	running := p.running
	p.mu.Unlock()
	if !running {
		return
	}
	txs := p.ensureTxs()
	var totalSum float64
	var subSum float64
	var mergeSum float64
	for i := 0; i < p.cfg.Repeat; i++ {
		totalMs, subMs, mergeMs := p.node.computeOnce(txs)
		totalSum += totalMs
		subSum += subMs
		mergeSum += mergeMs
	}
	repeat := float64(p.cfg.Repeat)
	avgTotal := totalSum / repeat
	avgSub := subSum / repeat
	avgMerge := mergeSum / repeat
	fmt.Printf(
		"block_time: %.4f ms subblock_time: %.4f ms merge_time: %.4f ms k=%d tx=%d size=%d\n",
		avgTotal,
		avgSub,
		avgMerge,
		p.cfg.SubBlockK,
		p.cfg.TxCount,
		p.cfg.TxSize,
	)
}

func (p *ParallelMerkleConsensus) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	return nil
}

// ensureTxs 获取或构造用于并行 Merkle 计算的交易集合。
func (p *ParallelMerkleConsensus) ensureTxs() [][]byte {
	return p.node.ensureTxs()
}
