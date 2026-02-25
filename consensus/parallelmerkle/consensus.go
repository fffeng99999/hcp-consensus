package parallelmerkle

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"sync"
	"time"

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
	cfg     Config
	running bool
	mu      sync.Mutex
	txs     [][]byte
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
	return &ParallelMerkleConsensus{cfg: cfg}
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
		totalMs, subMs, mergeMs := p.computeOnce(txs)
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

func (p *ParallelMerkleConsensus) ensureTxs() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.txs) == p.cfg.TxCount {
		return p.txs
	}
	txs := make([][]byte, 0, p.cfg.TxCount)
	for i := 0; i < p.cfg.TxCount; i++ {
		seed := sha256.Sum256([]byte(fmt.Sprintf("%d", i)))
		buf := make([]byte, p.cfg.TxSize)
		for offset := 0; offset < p.cfg.TxSize; offset += len(seed) {
			copy(buf[offset:], seed[:])
		}
		txs = append(txs, buf)
	}
	p.txs = txs
	return txs
}

func (p *ParallelMerkleConsensus) computeOnce(txs [][]byte) (float64, float64, float64) {
	start := time.Now()
	subStart := time.Now()
	subRoots := p.computeSubRoots(txs)
	subMs := float64(time.Since(subStart).Microseconds()) / 1000.0
	mergeStart := time.Now()
	_ = merkleRootFromHashes(subRoots)
	mergeMs := float64(time.Since(mergeStart).Microseconds()) / 1000.0
	totalMs := float64(time.Since(start).Microseconds()) / 1000.0
	return totalMs, subMs, mergeMs
}

func (p *ParallelMerkleConsensus) computeSubRoots(txs [][]byte) [][]byte {
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
