package hierarchical_tpbft_parallel_block

import (
	"crypto/sha256"
	"math"
	"strings"
	"sync"
	"time"
)

// Node 封装分层 TPBFT 并行块单轮指标计算流程。
type Node struct {
	cfg      Config
	scorer   *TrustScorer
	selector *ValidatorSelector
}

// NewNode 创建分层 TPBFT 并行块节点模型。
func NewNode(cfg Config, scorer *TrustScorer, selector *ValidatorSelector) *Node {
	return &Node{
		cfg:      cfg,
		scorer:   scorer,
		selector: selector,
	}
}

// ComputeMetrics 计算当前配置下的分层 TPBFT + 并行 Merkle 综合指标。
func (n *Node) ComputeMetrics(txs [][]byte) Metrics {
	cfg := n.scorer.Config()
	nodeCount := float64(cfg.NodeCount)
	groupCount, groupSize := n.selector.ResolveGroupShape()
	g := float64(groupCount)
	s := float64(groupSize)
	base := cfg.BaseLatencyMs
	innerWeight := cfg.PhaseWeightInner
	outerWeight := cfg.PhaseWeightOuter
	if g <= 0 {
		g = 1
	}
	if s <= 0 {
		s = math.Max(1, math.Floor(nodeCount/g))
	}

	totalMessages := nodeCount + g
	commBytes := totalMessages * float64(cfg.MessageBytes)
	outerMode := strings.ToLower(cfg.OuterSigMode)
	if outerMode == "" {
		outerMode = "threshold"
	}
	outerAlgo := strings.ToLower(cfg.OuterSigAlgorithm)
	if outerAlgo == "" {
		outerAlgo = cfg.SigAlgorithm
	}
	outerSigGenMs, outerSigVerifyMs, outerSigAggMs := n.resolveOuterSigCosts(cfg, outerMode, outerAlgo)

	innerSigGenOps := s
	innerSigVerifyOps := s
	innerAggOps := 1.0
	outerSigGenOps, outerSigVerifyOps, outerAggOps := resolveOuterOps(g, outerMode)

	sigGenCount := innerSigGenOps + outerSigGenOps
	sigVerifyCount := innerSigVerifyOps + outerSigVerifyOps
	sigGenParallelism := math.Max(1, cfg.SigGenParallelism)
	sigVerifyParallelism := math.Max(1, cfg.SigVerifyParallelism)
	sigAggParallelism := math.Max(1, cfg.SigAggParallelism)
	verifyGain := math.Max(1, cfg.BatchVerifyGain)
	batchVerify := 0.0
	if cfg.BatchVerify && verifyGain > 1 {
		batchVerify = 1
	}

	sigGenTime := (innerSigGenOps*cfg.SigGenMs + outerSigGenOps*outerSigGenMs) / sigGenParallelism
	sigVerifyTime := (innerSigVerifyOps*cfg.SigVerifyMs + outerSigVerifyOps*outerSigVerifyMs) / sigVerifyParallelism
	if batchVerify > 0 {
		sigVerifyTime /= verifyGain
	}
	aggTime := (innerAggOps*cfg.SigAggMs + outerAggOps*outerSigAggMs) / sigAggParallelism

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	sigOpsPerTx := (sigGenCount + sigVerifyCount + innerAggOps + outerAggOps) / float64(batchSize)

	pre := base * innerWeight * s
	prepare := base * innerWeight * s
	commit := base * outerWeight * g

	sigPerNode := 0.0
	if nodeCount > 0 {
		sigPerNode = 2 + 2/g
	}

	// 并行 Merkle 计算
	blockTimeMs, subBlockTimeMs, mergeTimeMs := 0.0, 0.0, 0.0
	txCount := len(txs)
	txBytes := 0
	for _, tx := range txs {
		txBytes += len(tx)
	}
	if txCount > 0 && cfg.SubBlockK > 1 {
		blockTimeMs, subBlockTimeMs, mergeTimeMs = n.computeParallelMerkle(txs)
	} else if txCount > 0 {
		start := time.Now()
		_ = merkleRootFromTxs(txs)
		blockTimeMs = float64(time.Since(start).Microseconds()) / 1000.0
	}

	return Metrics{
		PrePrepareMs:         pre,
		PrepareMs:            prepare,
		CommitMs:             commit,
		CommBytes:            commBytes,
		TotalMessages:        totalMessages,
		SigGenCount:          sigGenCount,
		SigVerifyCount:       sigVerifyCount,
		SigGenTimeMs:         sigGenTime,
		SigVerifyTimeMs:      sigVerifyTime,
		AggregationTimeMs:    aggTime,
		VerifyTimeMs:         sigVerifyTime,
		SigPerNode:           sigPerNode,
		SigOpsPerTx:          sigOpsPerTx,
		BatchSize:            batchSize,
		BatchVerify:          batchVerify,
		VerifyGain:           verifyGain,
		SigGenParallelism:    sigGenParallelism,
		SigVerifyParallelism: sigVerifyParallelism,
		SigAggParallelism:    sigAggParallelism,
		OuterMode:            outerMode,
		SigAlgo:              cfg.SigAlgorithm,
		OuterAlgo:            outerAlgo,
		BlockTimeMs:          blockTimeMs,
		SubBlockTimeMs:       subBlockTimeMs,
		MergeTimeMs:          mergeTimeMs,
		SubBlockK:            cfg.SubBlockK,
		TxCount:              txCount,
		TxBytes:              txBytes,
	}
}

// computeParallelMerkle 对交易列表执行并行 Merkle 计算。
func (n *Node) computeParallelMerkle(txs [][]byte) (float64, float64, float64) {
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

// computeSubRoots 并行计算子块根。
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

// resolveOuterSigCosts 解析外层签名阶段的耗时参数。
func (n *Node) resolveOuterSigCosts(cfg Config, outerMode string, outerAlgo string) (float64, float64, float64) {
	outerSigGenMs := cfg.OuterSigGenMs
	outerSigVerifyMs := cfg.OuterSigVerifyMs
	outerSigAggMs := cfg.OuterSigAggMs
	if outerSigGenMs > 0 && outerSigVerifyMs > 0 && outerSigAggMs > 0 {
		return outerSigGenMs, outerSigVerifyMs, outerSigAggMs
	}
	defaultGen, defaultVerify, defaultAgg := defaultSigProfile(outerAlgo)
	if outerSigGenMs <= 0 {
		outerSigGenMs = defaultGen
	}
	if outerSigVerifyMs <= 0 {
		outerSigVerifyMs = defaultVerify
	}
	if outerSigAggMs <= 0 {
		outerSigAggMs = defaultAgg
	}
	if outerMode == "none" {
		outerSigGenMs = 0
		outerSigAggMs = 0
	}
	return outerSigGenMs, outerSigVerifyMs, outerSigAggMs
}

// resolveOuterOps 计算外层签名各类操作次数。
func resolveOuterOps(groupCount float64, outerMode string) (float64, float64, float64) {
	outerSigGenOps := 0.0
	outerSigVerifyOps := 0.0
	outerAggOps := 0.0
	switch outerMode {
	case "ed25519":
		outerSigGenOps = groupCount
		outerSigVerifyOps = groupCount
	case "none":
		outerSigVerifyOps = groupCount
	default:
		outerSigGenOps = groupCount
		outerSigVerifyOps = groupCount
		outerAggOps = 1
	}
	return outerSigGenOps, outerSigVerifyOps, outerAggOps
}
