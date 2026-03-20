package parallelmerkle

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// Node 封装并行 Merkle 计算流程。
type Node struct {
	cfg Config
	mu  sync.Mutex
	txs [][]byte
}

// NewNode 创建并行 Merkle 节点模型。
func NewNode(cfg Config) *Node {
	return &Node{cfg: cfg}
}

// ensureTxs 获取或生成交易样本。
func (n *Node) ensureTxs() [][]byte {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.txs) == n.cfg.TxCount {
		return n.txs
	}
	txs := make([][]byte, 0, n.cfg.TxCount)
	for i := 0; i < n.cfg.TxCount; i++ {
		seed := sha256.Sum256([]byte(fmt.Sprintf("%d", i)))
		buf := make([]byte, n.cfg.TxSize)
		for offset := 0; offset < n.cfg.TxSize; offset += len(seed) {
			copy(buf[offset:], seed[:])
		}
		txs = append(txs, buf)
	}
	n.txs = txs
	return txs
}

// computeOnce 执行一次并行 Merkle 计算。
func (n *Node) computeOnce(txs [][]byte) (float64, float64, float64) {
	start := time.Now()
	subStart := time.Now()
	subRoots := n.computeSubRoots(txs)
	subMs := float64(time.Since(subStart).Microseconds()) / 1000.0
	mergeStart := time.Now()
	_ = merkleRootFromHashes(subRoots)
	mergeMs := float64(time.Since(mergeStart).Microseconds()) / 1000.0
	totalMs := float64(time.Since(start).Microseconds()) / 1000.0
	return totalMs, subMs, mergeMs
}

// computeSubRoots 计算子块根。
func (n *Node) computeSubRoots(txs [][]byte) [][]byte {
	if n.cfg.SubBlockK <= 1 {
		return [][]byte{merkleRootFromTxs(txs)}
	}
	blocks := splitTxs(txs, n.cfg.SubBlockK)
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

// splitTxs 将交易列表按 k 等分。
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

// merkleRootFromTxs 从交易计算 Merkle 根。
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

// merkleRootFromHashes 从哈希列表计算 Merkle 根。
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
