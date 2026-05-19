package common

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/fffeng99999/hcp-consensus/engine/core"
)

// SimpleExecutor 简单执行器，只维护状态哈希和账户余额
type SimpleExecutor struct {
	mu         sync.RWMutex
	stateHash  string
	balances   map[string]uint64
	nonce      map[string]uint64
	blockCount uint64
}

func NewSimpleExecutor() *SimpleExecutor {
	h := sha256.Sum256([]byte("genesis"))
	return &SimpleExecutor{
		stateHash: hex.EncodeToString(h[:]),
		balances:  make(map[string]uint64),
		nonce:     make(map[string]uint64),
	}
}

func (e *SimpleExecutor) ExecuteBlock(block *core.Block) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	h := sha256.New()
	h.Write([]byte(e.stateHash))
	for _, tx := range block.Txs {
		h.Write([]byte(tx.ID))
		// 简单模拟：更新nonce
		e.nonce[tx.From] = tx.Nonce
	}
	e.stateHash = hex.EncodeToString(h.Sum(nil))
	e.blockCount++
	return nil
}

func (e *SimpleExecutor) GetStateHash() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.stateHash
}

func (e *SimpleExecutor) GetBlockCount() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.blockCount
}

// BatchVerifier 批量签名验证器
type BatchVerifier struct {
	mu        sync.Mutex
	verifier  func(data, sig []byte, pubKey string) bool
	batchSize int
}

func NewBatchVerifier(batchSize int, verifier func(data, sig []byte, pubKey string) bool) *BatchVerifier {
	if batchSize <= 0 {
		batchSize = 64
	}
	return &BatchVerifier{
		verifier:  verifier,
		batchSize: batchSize,
	}
}

func (bv *BatchVerifier) VerifyBatch(items []VerifyItem) []bool {
	results := make([]bool, len(items))
	for i, item := range items {
		results[i] = bv.verifier(item.Data, item.Sig, item.PubKey)
	}
	return results
}

type VerifyItem struct {
	Data   []byte
	Sig    []byte
	PubKey string
}

// ParallelSigVerifier 并行签名验证器
type ParallelSigVerifier struct {
	workers   int
	verifier  func(data, sig []byte, pubKey string) bool
}

func NewParallelSigVerifier(workers int, verifier func(data, sig []byte, pubKey string) bool) *ParallelSigVerifier {
	if workers <= 0 {
		workers = 4
	}
	return &ParallelSigVerifier{
		workers:  workers,
		verifier: verifier,
	}
}

func (pv *ParallelSigVerifier) VerifyAll(items []VerifyItem) []bool {
	if len(items) == 0 {
		return nil
	}
	// 简化：如果数量少，串行处理
	if len(items) < pv.workers*2 {
		results := make([]bool, len(items))
		for i, item := range items {
			results[i] = pv.verifier(item.Data, item.Sig, item.PubKey)
		}
		return results
	}

	results := make([]bool, len(items))
	chunkSize := (len(items) + pv.workers - 1) / pv.workers
	done := make(chan struct{}, pv.workers)

	for w := 0; w < pv.workers; w++ {
		start := w * chunkSize
		end := start + chunkSize
		if start >= len(items) {
			break
		}
		if end > len(items) {
			end = len(items)
		}
		go func(s, e int) {
			for i := s; i < e; i++ {
				results[i] = pv.verifier(items[i].Data, items[i].Sig, items[i].PubKey)
			}
			done <- struct{}{}
		}(start, end)
	}

	expected := 0
	for w := 0; w < pv.workers; w++ {
		start := w * chunkSize
		if start >= len(items) {
			break
		}
		expected++
	}
	for i := 0; i < expected; i++ {
		<-done
	}
	return results
}

// TrustScorer 信任评分模型
type TrustScorer struct {
	mu       sync.Mutex
	scores   map[string]*TrustScore
	history  map[string][]bool // 节点 -> 最近100轮成功率
	weights  TrustWeights
}

type TrustScore struct {
	NodeID       string
	SuccessRate  float64
	StakeWeight  float64
	ResponseTime float64
	TotalScore   float64
}

type TrustWeights struct {
	W1 float64 // 成功率权重
	W2 float64 // 质押权重
	W3 float64 // 响应速度权重
}

func DefaultTrustWeights() TrustWeights {
	return TrustWeights{W1: 0.4, W2: 0.3, W3: 0.3}
}

func NewTrustScorer(weights TrustWeights) *TrustScorer {
	return &TrustScorer{
		scores:  make(map[string]*TrustScore),
		history: make(map[string][]bool),
		weights: weights,
	}
}

func (ts *TrustScorer) RecordRound(nodeID string, success bool, responseMs float64, stake float64, totalStake float64) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	hist := ts.history[nodeID]
	hist = append(hist, success)
	if len(hist) > 100 {
		hist = hist[1:]
	}
	ts.history[nodeID] = hist

	// 计算成功率
	successCount := 0
	for _, s := range hist {
		if s {
			successCount++
		}
	}
	sr := float64(successCount) / float64(len(hist))

	// 质押权重
	sw := 0.0
	if totalStake > 0 {
		sw = stake / totalStake
	}

	// 响应速度评分（越快越高，假设100ms为基准）
	rs := 1.0
	if responseMs > 0 {
		rs = 100.0 / (responseMs + 100.0)
	}

	score := &TrustScore{
		NodeID:       nodeID,
		SuccessRate:  sr,
		StakeWeight:  sw,
		ResponseTime: responseMs,
		TotalScore:   ts.weights.W1*sr + ts.weights.W2*sw + ts.weights.W3*rs,
	}
	ts.scores[nodeID] = score
}

func (ts *TrustScorer) GetScore(nodeID string) *TrustScore {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if s, ok := ts.scores[nodeID]; ok {
		return s
	}
	return &TrustScore{NodeID: nodeID, TotalScore: 0.5}
}

func (ts *TrustScorer) SelectValidators(minTrust float64, maxCount int, allNodes []string) []string {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	type pair struct {
		id    string
		score float64
	}
	pairs := make([]pair, 0, len(allNodes))
	for _, node := range allNodes {
		s := 0.5
		if sc, ok := ts.scores[node]; ok {
			s = sc.TotalScore
		}
		if s >= minTrust {
			pairs = append(pairs, pair{id: node, score: s})
		}
	}

	// 按分数排序（降序）
	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[i].score < pairs[j].score {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}

	count := maxCount
	if count > len(pairs) {
		count = len(pairs)
	}
	result := make([]string, count)
	for i := 0; i < count; i++ {
		result[i] = pairs[i].id
	}
	return result
}
