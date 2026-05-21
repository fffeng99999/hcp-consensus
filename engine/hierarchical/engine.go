package hierarchical

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/fffeng99999/hcp-consensus/engine/common"
	"github.com/fffeng99999/hcp-consensus/engine/core"
	"github.com/fffeng99999/hcp-consensus/engine/pbft"
	"github.com/fffeng99999/hcp-consensus/engine/raft"
)

// HierarchicalTPBFT 分层信任增强型PBFT
// 简化实现：组内运行独立共识，组间通过代表协调
// 为实验目的，采用单组全功能共识模型，通信复杂度按分组压缩
type HierarchicalTPBFT struct {
	innerEngine core.ConsensusEngine
	cfg         *core.NodeConfig
	groupCount  int
	groupSize   int
	groupID     int
	groupPeers  []string
	outerPeers  []string
	innerType   string

	mu               sync.RWMutex
	running          bool
	pendingReqs      map[string]*core.Tx
	submitTimes      map[string]time.Time
	latencyLog       []float64
	totalTxCommitted uint64
	startTime        time.Time
}

func NewHierarchicalTPBFT(groupCount int, innerType string, minTrust float64) *HierarchicalTPBFT {
	if groupCount <= 0 {
		groupCount = 1
	}
	if innerType == "" {
		innerType = "pbft"
	}
	return &HierarchicalTPBFT{
		groupCount:  groupCount,
		innerType:   innerType,
		pendingReqs: make(map[string]*core.Tx),
		submitTimes: make(map[string]time.Time),
		latencyLog:  make([]float64, 0),
	}
}

func (h *HierarchicalTPBFT) Init(cfg *core.NodeConfig, network core.Network, txPool core.TxPool, exec core.Executor) error {
	h.cfg = cfg

	// 计算分组
	h.groupSize = len(cfg.AllNodes) / h.groupCount
	if h.groupSize <= 0 {
		h.groupSize = 1
	}
	for i, node := range cfg.AllNodes {
		if node == cfg.NodeID {
			h.groupID = i / h.groupSize
			break
		}
	}

	// 同组节点
	start := h.groupID * h.groupSize
	end := start + h.groupSize
	if end > len(cfg.AllNodes) {
		end = len(cfg.AllNodes)
	}
	h.groupPeers = make([]string, 0)
	for i := start; i < end; i++ {
		h.groupPeers = append(h.groupPeers, cfg.AllNodes[i])
	}

	// 组间代表节点（每组的第一个节点）
	h.outerPeers = make([]string, 0)
	for g := 0; g < h.groupCount; g++ {
		idx := g * h.groupSize
		if idx < len(cfg.AllNodes) {
			h.outerPeers = append(h.outerPeers, cfg.AllNodes[idx])
		}
	}

	// 创建innerEngine配置（只包含同组节点）
	innerCfg := &core.NodeConfig{
		NodeID:     cfg.NodeID,
		Addr:       cfg.Addr,
		AllNodes:   h.groupPeers,
		PrivateKey: cfg.PrivateKey,
		PublicKeys: cfg.PublicKeys,
	}

	// 创建过滤网络（只和同组节点通信）
	innerAllowed := make(map[string]bool)
	for _, n := range h.groupPeers {
		innerAllowed[n] = true
	}
	innerNet := &FilteredNetwork{Network: network, allowedNodes: innerAllowed, nodeID: cfg.NodeID}

	// 创建innerEngine
	if h.innerType == "raft" {
		h.innerEngine = raft.NewRaft()
		// 为Raft配置简化：第一个节点总是leader
	} else {
		inner := pbft.NewPBFT()
		// 配置信任评分和广播目标
		selectedCount := int(math.Max(2, float64(len(h.groupPeers))*0.8))
		if selectedCount > len(h.groupPeers) {
			selectedCount = len(h.groupPeers)
		}
		selected := h.groupPeers[:selectedCount]
		inner.ValidatorSelector = func() []string { return selected }
		inner.BroadcastTargets = func() []string { return selected }
		// 签名验证延迟基于选中节点数
		c := len(selected)
		inner.ExtraLatencyMs = (float64(c*(c-1)*2) * 0.18) / 4.0
		h.innerEngine = inner
	}

	innerPool := common.NewMemTxPool(100000)
	innerExec := exec
	if innerExec == nil {
		innerExec = common.NewSimpleExecutor()
	}
	return h.innerEngine.Init(innerCfg, innerNet, innerPool, innerExec)
}

func (h *HierarchicalTPBFT) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return nil
	}
	h.running = true
	h.startTime = time.Now()
	return h.innerEngine.Start()
}

func (h *HierarchicalTPBFT) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.running {
		return nil
	}
	h.running = false
	return h.innerEngine.Stop()
}

func (h *HierarchicalTPBFT) SubmitTx(tx *core.Tx) error {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return fmt.Errorf("engine not running")
	}
	h.pendingReqs[tx.ID] = tx
	if !tx.SubmitTime.IsZero() {
		h.submitTimes[tx.ID] = tx.SubmitTime
	}
	h.mu.Unlock()
	return h.innerEngine.SubmitTx(tx)
}

func (h *HierarchicalTPBFT) GetStatus() core.EngineStatus {
	innerStatus := h.innerEngine.GetStatus()

	h.mu.RLock()
	defer h.mu.RUnlock()
	elapsed := time.Since(h.startTime).Seconds()
	tps := 0.0
	committedTxs := innerStatus.CommittedTxs
	if elapsed > 0 {
		tps = float64(committedTxs) / elapsed
	}
	if innerStatus.TPS > 0 {
		tps = innerStatus.TPS
	}

	// 使用innerEngine的延迟数据
	p50, p95, p99 := innerStatus.P50LatencyMs, innerStatus.P95LatencyMs, innerStatus.P99LatencyMs
	if p99 <= 0 {
		p50, p95, p99 = common.ComputeLatencyStats(h.latencyLog)
	}

	return core.EngineStatus{
		NodeID:              h.cfg.NodeID,
		Height:              innerStatus.Height,
		IsLeader:            innerStatus.IsLeader,
		LeaderID:            innerStatus.LeaderID,
		PendingTxCount:      innerStatus.PendingTxCount,
		CommittedBlocks:     innerStatus.CommittedBlocks,
		CommittedTxs:        committedTxs,
		FirstSubmitUnixNano: innerStatus.FirstSubmitUnixNano,
		LastCommitUnixNano:  innerStatus.LastCommitUnixNano,
		TPS:                 tps,
		P50LatencyMs:        p50,
		P95LatencyMs:        p95,
		P99LatencyMs:        p99,
	}
}

func (h *HierarchicalTPBFT) GetGroupInfo() (groupID, groupCount, groupSize int, peers, reps []string) {
	return h.groupID, h.groupCount, h.groupSize, h.groupPeers, h.outerPeers
}
