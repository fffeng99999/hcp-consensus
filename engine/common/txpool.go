package common

import (
	"sync"

	"github.com/fffeng99999/hcp-consensus/engine/core"
)

// MemTxPool 内存交易池
type MemTxPool struct {
	mu      sync.RWMutex
	txs     map[string]*core.Tx
	order   []string
	maxSize int
}

// NewMemTxPool 创建内存交易池
func NewMemTxPool(maxSize int) *MemTxPool {
	if maxSize <= 0 {
		maxSize = 100000
	}
	return &MemTxPool{
		txs:     make(map[string]*core.Tx),
		order:   make([]string, 0),
		maxSize: maxSize,
	}
}

// AddTx 添加交易到池中
func (p *MemTxPool) AddTx(tx *core.Tx) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.txs[tx.ID]; exists {
		return nil
	}
	if len(p.txs) >= p.maxSize {
		// 移除最旧的交易
		oldest := p.order[0]
		delete(p.txs, oldest)
		p.order = p.order[1:]
	}
	p.txs[tx.ID] = tx
	p.order = append(p.order, tx.ID)
	return nil
}

// GetTxs 获取最多 max 笔交易
func (p *MemTxPool) GetTxs(max int) []*core.Tx {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if max <= 0 || max > len(p.order) {
		max = len(p.order)
	}
	result := make([]*core.Tx, 0, max)
	for i := 0; i < max && i < len(p.order); i++ {
		if tx, ok := p.txs[p.order[i]]; ok {
			result = append(result, tx)
		}
	}
	return result
}

// RemoveTxs 从池中移除指定交易
func (p *MemTxPool) RemoveTxs(txIDs []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, id := range txIDs {
		delete(p.txs, id)
	}
	// 重建 order
	newOrder := make([]string, 0, len(p.order))
	for _, id := range p.order {
		if _, exists := p.txs[id]; exists {
			newOrder = append(newOrder, id)
		}
	}
	p.order = newOrder
}

// PendingCount 返回待处理交易数量
func (p *MemTxPool) PendingCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.order)
}
