package factory

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/fffeng99999/hcp-consensus/engine/cometbft"
	"github.com/fffeng99999/hcp-consensus/engine/common"
	"github.com/fffeng99999/hcp-consensus/engine/core"
	"github.com/fffeng99999/hcp-consensus/engine/hierarchical"
	"github.com/fffeng99999/hcp-consensus/engine/hotstuff"
	"github.com/fffeng99999/hcp-consensus/engine/network"
	"github.com/fffeng99999/hcp-consensus/engine/pbft"
	"github.com/fffeng99999/hcp-consensus/engine/raft"
	"github.com/fffeng99999/hcp-consensus/engine/tpbft"
)

// EngineType 引擎类型
type EngineType string

const (
	EnginePBFT                    EngineType = "pbft"
	EngineTPBFT                   EngineType = "tpbft"
	EngineHotStuff                EngineType = "hotstuff"
	EngineRaft                    EngineType = "raft"
	EngineCometBFT                EngineType = "cometbft"
	EngineHierarchicalTPBFT       EngineType = "hierarchical_tpbft"
	EngineHierarchicalLightweight EngineType = "hierarchical_lightweight_tpbft"
	EngineTPBFTParallel           EngineType = "tpbft_parallel"
)

// CreateEngine 创建共识引擎实例
func CreateEngine(et EngineType) (core.ConsensusEngine, error) {
	switch et {
	case EnginePBFT:
		return pbft.NewPBFT(), nil
	case EngineTPBFT:
		return tpbft.NewTPBFT(0.6, 100), nil
	case EngineHotStuff:
		return hotstuff.NewHotStuff(), nil
	case EngineRaft:
		return raft.NewRaft(), nil
	case EngineCometBFT:
		return cometbft.NewCometBFT(), nil
	case EngineHierarchicalTPBFT:
		return hierarchical.NewHierarchicalTPBFT(4, "pbft", 0.6), nil
	case EngineHierarchicalLightweight:
		return hierarchical.NewHierarchicalTPBFT(4, "raft", 0.6), nil
	default:
		return nil, fmt.Errorf("unknown engine type: %s", et)
	}
}

// CreateEngineWithGroup 创建带分组参数的分层共识引擎
func CreateEngineWithGroup(et EngineType, groupCount int, innerType string) (core.ConsensusEngine, error) {
	switch et {
	case EngineHierarchicalTPBFT:
		if innerType == "" {
			innerType = "pbft"
		}
		return hierarchical.NewHierarchicalTPBFT(groupCount, innerType, 0.6), nil
	case EngineHierarchicalLightweight:
		return hierarchical.NewHierarchicalTPBFT(groupCount, "raft", 0.6), nil
	default:
		return CreateEngine(et)
	}
}

// BuildClusterWithGroup 构建带分组参数的模拟集群
func BuildClusterWithGroup(et EngineType, nodeCount int, groupCount int, innerType string, latencyMs, bandwidth float64) (*network.Cluster, error) {
	cluster := network.NewCluster()
	simNet := cluster.Network
	simNet.SetLatency(latencyMs)
	simNet.SetBandwidth(bandwidth)

	// 生成节点密钥
	pubKeys := make(map[string]ed25519.PublicKey)
	privKeys := make(map[string]ed25519.PrivateKey)
	nodeIDs := make([]string, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nodeIDs[i] = fmt.Sprintf("node-%d", i)
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		privKeys[nodeIDs[i]] = priv
		pubKeys[nodeIDs[i]] = priv.Public().(ed25519.PublicKey)
	}

	for i := 0; i < nodeCount; i++ {
		nodeID := nodeIDs[i]
		cfg := &core.NodeConfig{
			NodeID:     nodeID,
			Addr:       fmt.Sprintf("127.0.0.1:%d", 10000+i),
			AllNodes:   nodeIDs,
			PrivateKey: privKeys[nodeID],
			PublicKeys: pubKeys,
		}

		var engine core.ConsensusEngine
		var err error
		if et == EngineHierarchicalTPBFT || et == EngineHierarchicalLightweight {
			engine, err = CreateEngineWithGroup(et, groupCount, innerType)
		} else {
			engine, err = CreateEngine(et)
		}
		if err != nil {
			return nil, err
		}

		pool := common.NewMemTxPool(100000)
		exec := common.NewSimpleExecutor()

		// 为不同算法配置特殊参数
		switch e := engine.(type) {
		case *tpbft.TPBFT:
			e.Init(cfg, simNet, pool, exec)
			base := e.GetPBFT()
			selectedCount := int(math.Max(4, float64(nodeCount)*0.7))
			if selectedCount > nodeCount {
				selectedCount = nodeCount
			}
			selected := nodeIDs[:selectedCount]
			base.ValidatorSelector = func() []string { return selected }
			base.BroadcastTargets = func() []string { return selected }
			base.OnCommit = func(block *core.Block) {
				for _, node := range nodeIDs {
					e.RecordTrustRound(node, true, 50.0, 1.0)
				}
			}
			// tPBFT的签名验证延迟基于选中节点数
			c := len(selected)
			base.ExtraLatencyMs = (float64(c*(c-1)*2) * 0.18) / 4.0
		case *pbft.PBFT:
			e.Init(cfg, simNet, pool, exec)
			e.ExtraLatencyMs = (float64(nodeCount*(nodeCount-1)*2) * 0.18) / 4.0
			e.BroadcastTargets = func() []string { return nodeIDs }
			e.ValidatorSelector = func() []string { return nodeIDs }
		case *cometbft.CometBFT:
			e.Init(cfg, simNet, pool, exec)
		case *hotstuff.HotStuff:
			e.Init(cfg, simNet, pool, exec)
			// HotStuff线性复杂度，签名验证少很多（只有leader聚合）
			e.ExtraLatencyMs = (float64(nodeCount-1) * 0.18) / 4.0 * 3 // 三阶段各需验证N-1个签名
		case *raft.Raft:
			e.Init(cfg, simNet, pool, exec)
			// Raft只有leader广播日志，验证开销小
			e.ExtraLatencyMs = (float64(nodeCount-1) * 0.18) / 4.0
		case *hierarchical.HierarchicalTPBFT:
			e.Init(cfg, simNet, pool, exec)
		default:
			engine.Init(cfg, simNet, pool, exec)
		}

		cluster.AddNode(nodeID, engine, pool)
	}

	return cluster, nil
}

// RunBenchmarkWithGroup 运行带分组参数的基准测试
func RunBenchmarkWithGroup(et EngineType, nodeCount int, groupCount int, innerType string, txCount int, txSize int, latencyMs, bandwidth float64) (*BenchmarkResult, error) {
	actualLatency := 5.0 // 基础LAN延迟5ms（更接近论文的单机多实例+模拟）
	if latencyMs > 0 {
		actualLatency = latencyMs
	}
	cluster, err := BuildClusterWithGroup(et, nodeCount, groupCount, innerType, actualLatency, bandwidth)
	if err != nil {
		return nil, err
	}

	if err := cluster.StartAll(); err != nil {
		return nil, err
	}
	defer cluster.StopAll()

	// 预热
	time.Sleep(200 * time.Millisecond)

	// 以固定速率发送交易（模拟持续负载）
	durationSec := 10
	if txCount <= 200 {
		durationSec = 5
	}
	targetTPS := float64(txCount) / float64(durationSec)
	intervalMs := 1000.0 / targetTPS

	start := time.Now()
	sent := 0
	for i := 0; i < txCount; i++ {
		payload := make([]byte, txSize)
		rand.Read(payload)
		tx := core.NewTx(payload, fmt.Sprintf("client-%d", i%100), uint64(i))
		tx.From = "client"
		tx.SubmitTime = time.Now()
		cluster.SubmitTx(tx)
		sent++
		if intervalMs > 0 {
			time.Sleep(time.Duration(intervalMs * float64(time.Millisecond)))
		}
	}

	// 发送完成后，等待所有交易被确认
	time.Sleep(2 * time.Second)
	timeout := time.Duration(30) * time.Second
	if nodeCount >= 32 {
		timeout = 60 * time.Second
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		allDone := true
		for _, pool := range cluster.TxPools {
			if pool.PendingCount() > 0 {
				allDone = false
				break
			}
		}
		if allDone {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	elapsed := time.Since(start).Seconds()

	// 收集指标（优先使用leader节点的延迟数据）
	statusMap := cluster.GetAllStatus()
	var sampleStatus core.EngineStatus
	for _, s := range statusMap {
		if s.IsLeader {
			sampleStatus = s
			break
		}
	}
	// 如果没有leader（理论上不应发生），使用第一个节点
	if sampleStatus.NodeID == "" {
		for _, s := range statusMap {
			sampleStatus = s
			break
		}
	}

	netMetrics := cluster.Network.GetMetrics()

	// 计算实际TPS（基于完成时间）
	actualTPS := float64(sent) / elapsed
	if actualTPS > float64(sent) {
		actualTPS = float64(sent) // 上限
	}

	return &BenchmarkResult{
		EngineType:     string(et),
		NodeCount:      nodeCount,
		TxCount:        sent,
		DurationSec:    elapsed,
		TPS:            actualTPS,
		P50LatencyMs:   sampleStatus.P50LatencyMs,
		P95LatencyMs:   sampleStatus.P95LatencyMs,
		P99LatencyMs:   sampleStatus.P99LatencyMs,
		TotalMessages:  netMetrics.TotalMessages,
		TotalBytes:     netMetrics.TotalBytes,
	}, nil
}

// BuildCluster 构建一个完整的模拟集群
func BuildCluster(et EngineType, nodeCount int, latencyMs, bandwidth float64) (*network.Cluster, error) {
	cluster := network.NewCluster()
	simNet := cluster.Network
	simNet.SetLatency(latencyMs)
	simNet.SetBandwidth(bandwidth)

	// 生成节点密钥
	pubKeys := make(map[string]ed25519.PublicKey)
	privKeys := make(map[string]ed25519.PrivateKey)
	nodeIDs := make([]string, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nodeIDs[i] = fmt.Sprintf("node-%d", i)
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		privKeys[nodeIDs[i]] = priv
		pubKeys[nodeIDs[i]] = priv.Public().(ed25519.PublicKey)
	}

	for i := 0; i < nodeCount; i++ {
		nodeID := nodeIDs[i]
		cfg := &core.NodeConfig{
			NodeID:     nodeID,
			Addr:       fmt.Sprintf("127.0.0.1:%d", 10000+i),
			AllNodes:   nodeIDs,
			PrivateKey: privKeys[nodeID],
			PublicKeys: pubKeys,
		}

		engine, err := CreateEngine(et)
		if err != nil {
			return nil, err
		}

		pool := common.NewMemTxPool(100000)
		exec := common.NewSimpleExecutor()

		// 为不同算法配置特殊参数
		switch e := engine.(type) {
		case *tpbft.TPBFT:
			e.Init(cfg, simNet, pool, exec)
			base := e.GetPBFT()
			selectedCount := int(math.Max(4, float64(nodeCount)*0.7))
			if selectedCount > nodeCount {
				selectedCount = nodeCount
			}
			selected := nodeIDs[:selectedCount]
			base.ValidatorSelector = func() []string { return selected }
			base.BroadcastTargets = func() []string { return selected }
			base.OnCommit = func(block *core.Block) {
				for _, node := range nodeIDs {
					e.RecordTrustRound(node, true, 50.0, 1.0)
				}
			}
			// tPBFT的签名验证延迟基于选中节点数
			c := len(selected)
			base.ExtraLatencyMs = (float64(c*(c-1)*2) * 0.18) / 4.0
		case *pbft.PBFT:
			e.Init(cfg, simNet, pool, exec)
			e.ExtraLatencyMs = (float64(nodeCount*(nodeCount-1)*2) * 0.18) / 4.0
			e.BroadcastTargets = func() []string { return nodeIDs }
			e.ValidatorSelector = func() []string { return nodeIDs }
		case *hotstuff.HotStuff:
			e.Init(cfg, simNet, pool, exec)
			// HotStuff线性复杂度，签名验证少很多（只有leader聚合）
			e.ExtraLatencyMs = (float64(nodeCount-1) * 0.18) / 4.0 * 3 // 三阶段各需验证N-1个签名
		case *raft.Raft:
			e.Init(cfg, simNet, pool, exec)
			// Raft只有leader广播日志，验证开销小
			e.ExtraLatencyMs = (float64(nodeCount-1) * 0.18) / 4.0
		case *hierarchical.HierarchicalTPBFT:
			e.Init(cfg, simNet, pool, exec)
		default:
			engine.Init(cfg, simNet, pool, exec)
		}

		cluster.AddNode(nodeID, engine, pool)
	}

	return cluster, nil
}

// RunBenchmark 运行基准测试（持续负载模式）
func RunBenchmark(et EngineType, nodeCount int, txCount int, txSize int, latencyMs, bandwidth float64) (*BenchmarkResult, error) {
	actualLatency := 5.0 // 基础LAN延迟5ms（更接近论文的单机多实例+模拟）
	if latencyMs > 0 {
		actualLatency = latencyMs
	}
	cluster, err := BuildCluster(et, nodeCount, actualLatency, bandwidth)
	if err != nil {
		return nil, err
	}

	if err := cluster.StartAll(); err != nil {
		return nil, err
	}
	defer cluster.StopAll()

	// 预热
	time.Sleep(200 * time.Millisecond)

	// 以固定速率发送交易（模拟持续负载）
	durationSec := 10
	if txCount <= 200 {
		durationSec = 5
	}
	targetTPS := float64(txCount) / float64(durationSec)
	intervalMs := 1000.0 / targetTPS

	start := time.Now()
	sent := 0
	for i := 0; i < txCount; i++ {
		payload := make([]byte, txSize)
		rand.Read(payload)
		tx := core.NewTx(payload, fmt.Sprintf("client-%d", i%100), uint64(i))
		tx.From = "client"
		tx.SubmitTime = time.Now()
		cluster.SubmitTx(tx)
		sent++
		if intervalMs > 0 {
			time.Sleep(time.Duration(intervalMs * float64(time.Millisecond)))
		}
	}

	// 发送完成后，等待所有交易被确认
	time.Sleep(2 * time.Second)
	timeout := time.Duration(30) * time.Second
	if nodeCount >= 32 {
		timeout = 60 * time.Second
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		allDone := true
		for _, pool := range cluster.TxPools {
			if pool.PendingCount() > 0 {
				allDone = false
				break
			}
		}
		if allDone {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	elapsed := time.Since(start).Seconds()

	// 收集指标（优先使用leader节点的延迟数据）
	statusMap := cluster.GetAllStatus()
	var sampleStatus core.EngineStatus
	for _, s := range statusMap {
		if s.IsLeader {
			sampleStatus = s
			break
		}
	}
	// 如果没有leader（理论上不应发生），使用第一个节点
	if sampleStatus.NodeID == "" {
		for _, s := range statusMap {
			sampleStatus = s
			break
		}
	}

	netMetrics := cluster.Network.GetMetrics()

	// 计算实际TPS（基于完成时间）
	actualTPS := float64(sent) / elapsed
	if actualTPS > float64(sent) {
		actualTPS = float64(sent) // 上限
	}

	result := &BenchmarkResult{
		EngineType:     string(et),
		NodeCount:      nodeCount,
		TxCount:        sent,
		DurationSec:    elapsed,
		TPS:            actualTPS,
		P50LatencyMs:   sampleStatus.P50LatencyMs,
		P95LatencyMs:   sampleStatus.P95LatencyMs,
		P99LatencyMs:   sampleStatus.P99LatencyMs,
		TotalMessages:  netMetrics.TotalMessages,
		TotalBytes:     netMetrics.TotalBytes,
	}

	return result, nil
}

// BenchmarkResult 基准测试结果
type BenchmarkResult struct {
	EngineType    string
	NodeCount     int
	TxCount       int
	DurationSec   float64
	TPS           float64
	P50LatencyMs  float64
	P95LatencyMs  float64
	P99LatencyMs  float64
	TotalMessages uint64
	TotalBytes    uint64
}

// RunAblation 运行消融实验
func RunAblation(nodeCount int, txCount int) map[string]*BenchmarkResult {
	results := make(map[string]*BenchmarkResult)
	mu := sync.Mutex{}
	var wg sync.WaitGroup

	configs := []struct {
		name string
		et   EngineType
	}{
		{"A_Baseline", EnginePBFT},
		{"B_tPBFT", EngineTPBFT},
		{"C_Hierarchical", EngineHierarchicalTPBFT},
		{"D_Lightweight", EngineHierarchicalLightweight},
		{"E_HotStuff", EngineHotStuff},
		{"F_Raft", EngineRaft},
	}

	for _, cfg := range configs {
		wg.Add(1)
		go func(name string, et EngineType) {
			defer wg.Done()
			res, err := RunBenchmark(et, nodeCount, txCount, 250, 5.0, 1000)
			if err != nil {
				fmt.Printf("Ablation %s failed: %v\n", name, err)
				return
			}
			mu.Lock()
			results[name] = res
			mu.Unlock()
		}(cfg.name, cfg.et)
	}

	wg.Wait()
	return results
}
