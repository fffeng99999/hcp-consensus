package hierarchical

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fffeng99999/hcp-consensus/engine/common"
	"github.com/fffeng99999/hcp-consensus/engine/core"
	"github.com/fffeng99999/hcp-consensus/engine/pbft"
	"github.com/fffeng99999/hcp-consensus/engine/raft"
)

// HierarchicalLightweight 分层轻量级共识
// 使用更小的共识委员会和更轻量的消息机制
type HierarchicalLightweight struct {
	innerEngine core.ConsensusEngine
	cfg         *core.NodeConfig
	groupCount  int
	groupSize   int
	groupID     int
	groupPeers  []string
	outerPeers  []string

	mu               sync.RWMutex
	running          bool
	pendingReqs      map[string]*core.Tx
	submitTimes      map[string]time.Time
	latencyLog       []float64
	totalTxCommitted uint64
	startTime        time.Time
}

func NewHierarchicalLightweight() *HierarchicalLightweight {
	return &HierarchicalLightweight{
		groupCount:  2,
		pendingReqs: make(map[string]*core.Tx),
		submitTimes: make(map[string]time.Time),
		latencyLog:  make([]float64, 0),
	}
}

func (h *HierarchicalLightweight) Init(cfg *core.NodeConfig, network core.Network, txPool core.TxPool, exec core.Executor) error {
	h.cfg = cfg

	h.groupCount = 2
	if len(cfg.AllNodes) < 4 {
		h.groupCount = 1
	}
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

	start := h.groupID * h.groupSize
	end := start + h.groupSize
	if end > len(cfg.AllNodes) {
		end = len(cfg.AllNodes)
	}
	h.groupPeers = make([]string, 0)
	for i := start; i < end; i++ {
		h.groupPeers = append(h.groupPeers, cfg.AllNodes[i])
	}

	h.outerPeers = make([]string, 0)
	for g := 0; g < h.groupCount; g++ {
		idx := g * h.groupSize
		if idx < len(cfg.AllNodes) {
			h.outerPeers = append(h.outerPeers, cfg.AllNodes[idx])
		}
	}

	innerCfg := &core.NodeConfig{
		NodeID:     cfg.NodeID,
		Addr:       cfg.Addr,
		AllNodes:   h.groupPeers,
		PrivateKey: cfg.PrivateKey,
		PublicKeys: cfg.PublicKeys,
	}

	innerAllowed := make(map[string]bool)
	for _, n := range h.groupPeers {
		innerAllowed[n] = true
	}
	innerNet := &FilteredNetwork{Network: network, allowedNodes: innerAllowed, nodeID: cfg.NodeID}

	// 使用Raft作为innerEngine（更轻量）
	inner := raft.NewRaft()
	// 使用Raft默认配置，通过FilteredNetwork减少通信量
	h.innerEngine = inner

	innerPool := common.NewMemTxPool(100000)
	innerExec := common.NewSimpleExecutor()
	return h.innerEngine.Init(innerCfg, innerNet, innerPool, innerExec)
}

func (h *HierarchicalLightweight) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return nil
	}
	h.running = true
	h.startTime = time.Now()
	return h.innerEngine.Start()
}

func (h *HierarchicalLightweight) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.running {
		return nil
	}
	h.running = false
	return h.innerEngine.Stop()
}

func (h *HierarchicalLightweight) SubmitTx(tx *core.Tx) error {
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

func (h *HierarchicalLightweight) GetStatus() core.EngineStatus {
	innerStatus := h.innerEngine.GetStatus()

	h.mu.RLock()
	defer h.mu.RUnlock()
	elapsed := time.Since(h.startTime).Seconds()
	tps := 0.0
	if elapsed > 0 {
		tps = float64(atomic.LoadUint64(&h.totalTxCommitted)) / elapsed
	}
	committedTxs := atomic.LoadUint64(&h.totalTxCommitted)

	p50, p95, p99 := innerStatus.P50LatencyMs, innerStatus.P95LatencyMs, innerStatus.P99LatencyMs
	if p99 <= 0 {
		p50, p95, p99 = common.ComputeLatencyStats(h.latencyLog)
	}

	return core.EngineStatus{
		NodeID:              h.cfg.NodeID,
		Height:              innerStatus.Height,
		IsLeader:            innerStatus.IsLeader,
		LeaderID:            innerStatus.LeaderID,
		PendingTxCount:      len(h.pendingReqs),
		CommittedTxs:        committedTxs,
		FirstSubmitUnixNano: innerStatus.FirstSubmitUnixNano,
		LastCommitUnixNano:  innerStatus.LastCommitUnixNano,
		TPS:                 tps,
		P50LatencyMs:        p50,
		P95LatencyMs:        p95,
		P99LatencyMs:        p99,
	}
}

// ComputeLatencyStats 计算延迟统计（辅助函数）
func ComputeLatencyStats(latencies []float64) (p50, p95, p99 float64) {
	return common.ComputeLatencyStats(latencies)
}

// ValidatorSelectorFn 类型别名
type ValidatorSelectorFn = func() []string

// 设置PBFT额外延迟
func SetPBFTExtraLatency(pb *pbft.PBFT, ms float64) {
	pb.ExtraLatencyMs = ms
}

// math.Max for int
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
