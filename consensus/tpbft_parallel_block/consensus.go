package tpbft_parallel_block

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"sync"
	"time"

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
}

func NewTPBFTParallelBlock(cfg Config) *TPBFTParallelBlock {
	if cfg.SubBlockK <= 0 {
		cfg.SubBlockK = 1
	}
	return &TPBFTParallelBlock{
		base: tpbft.NewTPBFT(),
		cfg:  cfg,
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
	start := time.Now()
	subStart := time.Now()
	subRoots := p.computeSubRoots(txs)
	subMs := float64(time.Since(subStart).Microseconds()) / 1000.0
	mergeStart := time.Now()
	_ = merkleRootFromHashes(subRoots)
	mergeMs := float64(time.Since(mergeStart).Microseconds()) / 1000.0
	totalMs := float64(time.Since(start).Microseconds()) / 1000.0
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

func (p *TPBFTParallelBlock) computeSubRoots(txs [][]byte) [][]byte {
	if p.cfg.SubBlockK <= 1 {
		return [][]byte{merkleRootFromTxs(txs)}
	}
	blocks := splitTxs(txs, p.cfg.SubBlockK)
	results := make([][]byte, len(blocks))
	var wg sync.WaitGroup
	for idx, block := range blocks {
		wg.Add(1)
		go func(i int, data [][]byte) {
			defer wg.Done()
			results[i] = merkleRootFromTxs(data)
		}(idx, block)
	}
	wg.Wait()
	return results
}

func splitTxs(txs [][]byte, k int) [][][]byte {
	total := len(txs)
	if k <= 1 || total == 0 {
		return [][][]byte{txs}
	}
	base := total / k
	rem := total % k
	blocks := make([][][]byte, 0, k)
	start := 0
	for i := 0; i < k; i++ {
		size := base
		if i < rem {
			size++
		}
		end := start + size
		if end > total {
			end = total
		}
		blocks = append(blocks, txs[start:end])
		start = end
	}
	return blocks
}

func merkleRootFromTxs(txs [][]byte) []byte {
	if len(txs) == 0 {
		return nil
	}
	hashes := make([][]byte, len(txs))
	for i, tx := range txs {
		digest := sha256.Sum256(tx)
		hashes[i] = digest[:]
	}
	return merkleRootFromHashes(hashes)
}

func merkleRootFromHashes(hashes [][]byte) []byte {
	if len(hashes) == 0 {
		return nil
	}
	current := make([][]byte, len(hashes))
	copy(current, hashes)
	for len(current) > 1 {
		if len(current)%2 == 1 {
			current = append(current, current[len(current)-1])
		}
		next := make([][]byte, 0, len(current)/2)
		for i := 0; i < len(current); i += 2 {
			pair := append(current[i], current[i+1]...)
			digest := sha256.Sum256(pair)
			next = append(next, digest[:])
		}
		current = next
	}
	return current[0]
}
