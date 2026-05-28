package factory

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fffeng99999/hcp-consensus/engine/common"
	"github.com/fffeng99999/hcp-consensus/engine/consensus/cometbft"
	"github.com/fffeng99999/hcp-consensus/engine/consensus/hierarchical"
	"github.com/fffeng99999/hcp-consensus/engine/consensus/hotstuff"
	"github.com/fffeng99999/hcp-consensus/engine/consensus/pbft"
	"github.com/fffeng99999/hcp-consensus/engine/consensus/raft"
	"github.com/fffeng99999/hcp-consensus/engine/consensus/tpbft"
	"github.com/fffeng99999/hcp-consensus/engine/core"
	"github.com/fffeng99999/hcp-consensus/engine/network"
	"github.com/fffeng99999/hcp-consensus/engine/sdkexec"
)

// EngineType 是实验层传入的共识算法名称。
type EngineType string

const (
	EnginePBFT                    EngineType = "pbft"
	EngineTPBFT                   EngineType = "tpbft"
	EngineHotStuff                EngineType = "hotstuff"
	EngineRaft                    EngineType = "raft"
	EngineCometBFT                EngineType = "cometbft"
	EngineCometBFTLight           EngineType = "cometbft-light"
	EngineHierarchicalTPBFT       EngineType = "hierarchical_tpbft"
	EngineHierarchicalLightweight EngineType = "hierarchical_lightweight_tpbft"
	EngineTPBFTParallel           EngineType = "tpbft_parallel"
)

// CreateEngine 只负责按名称创建算法实例，不给算法注入固定收益参数。
func CreateEngine(et EngineType) (core.ConsensusEngine, error) {
	switch et {
	case EnginePBFT:
		return pbft.NewPBFT(), nil
	case EngineTPBFT:
		return tpbft.NewTPBFT(), nil
	case EngineHotStuff:
		return hotstuff.NewHotStuff(), nil
	case EngineRaft:
		return raft.NewRaft(), nil
	case EngineCometBFT, EngineCometBFTLight, EngineType("cometBFT-light"), EngineType("cometbft_light"):
		return cometbft.NewCometBFT(), nil
	case EngineHierarchicalTPBFT:
		return hierarchical.NewHierarchicalTPBFT(4, "pbft", 0.6), nil
	case EngineHierarchicalLightweight:
		return hierarchical.NewHierarchicalTPBFT(4, "raft", 0.6), nil
	default:
		return nil, fmt.Errorf("unknown engine type: %s", et)
	}
}

// CreateEngineWithGroup 创建需要分组参数的分层共识引擎。
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

func createExecutor(nodeID string) core.Executor {
	if !sdkExecutorEnabled() {
		return common.NewSimpleExecutor()
	}
	baseDir := os.Getenv("HCP_ENGINE_NODE_DATA_DIR")
	if strings.TrimSpace(baseDir) == "" {
		return common.NewSimpleExecutor()
	}
	nodeName := strings.ReplaceAll(nodeID, "-", "")
	homeDir := filepath.Join(baseDir, nodeName)
	exec, err := sdkexec.New(nodeID, homeDir, os.Getenv("HCP_ENGINE_SDK_CHAIN_ID"))
	if err != nil {
		fmt.Printf("sdk executor disabled for %s: %v\n", nodeID, err)
		return common.NewSimpleExecutor()
	}
	return exec
}

func sdkExecutorEnabled() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("HCP_ENGINE_SDK_EXEC")))
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

// BuildClusterWithGroup 构建带分组参数的模拟集群。
func BuildClusterWithGroup(et EngineType, nodeCount int, groupCount int, innerType string, latencyMs, bandwidth float64) (*network.Cluster, error) {
	return buildCluster(et, nodeCount, groupCount, innerType, latencyMs, bandwidth)
}

// BuildCluster 构建普通模拟集群。
func BuildCluster(et EngineType, nodeCount int, latencyMs, bandwidth float64) (*network.Cluster, error) {
	return buildCluster(et, nodeCount, 0, "", latencyMs, bandwidth)
}

func buildCluster(et EngineType, nodeCount int, groupCount int, innerType string, latencyMs, bandwidth float64) (*network.Cluster, error) {
	cluster := network.NewCluster()
	simNet := cluster.Network
	simNet.SetLatency(latencyMs)
	simNet.SetBandwidth(bandwidth)

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

		engine, err := createEngineForCluster(et, groupCount, innerType)
		if err != nil {
			return nil, err
		}
		pool := common.NewMemTxPool(100000)
		exec := createExecutor(nodeID)
		if err := engine.Init(cfg, simNet, pool, exec); err != nil {
			return nil, err
		}
		cluster.AddNode(nodeID, engine, pool)
	}
	return cluster, nil
}

func createEngineForCluster(et EngineType, groupCount int, innerType string) (core.ConsensusEngine, error) {
	if et == EngineHierarchicalTPBFT || et == EngineHierarchicalLightweight {
		return CreateEngineWithGroup(et, groupCount, innerType)
	}
	return CreateEngine(et)
}

// RunBenchmarkWithGroup 运行带分组参数的基准测试。
func RunBenchmarkWithGroup(et EngineType, nodeCount int, groupCount int, innerType string, txCount int, txSize int, latencyMs, bandwidth float64) (*BenchmarkResult, error) {
	cluster, err := BuildClusterWithGroup(et, nodeCount, groupCount, innerType, effectiveLatency(latencyMs), bandwidth)
	if err != nil {
		return nil, err
	}
	return runClusterBenchmark(cluster, et, nodeCount, txCount, txSize)
}

// RunBenchmark 运行普通基准测试。
func RunBenchmark(et EngineType, nodeCount int, txCount int, txSize int, latencyMs, bandwidth float64) (*BenchmarkResult, error) {
	cluster, err := BuildCluster(et, nodeCount, effectiveLatency(latencyMs), bandwidth)
	if err != nil {
		return nil, err
	}
	return runClusterBenchmark(cluster, et, nodeCount, txCount, txSize)
}

func effectiveLatency(latencyMs float64) float64 {
	if latencyMs > 0 {
		return latencyMs
	}
	return 5.0
}

func runClusterBenchmark(cluster *network.Cluster, et EngineType, nodeCount int, txCount int, txSize int) (*BenchmarkResult, error) {
	if err := cluster.StartAll(); err != nil {
		return nil, err
	}
	defer cluster.StopAll()

	time.Sleep(200 * time.Millisecond)
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
		_, _ = rand.Read(payload)
		tx := core.NewTx(payload, fmt.Sprintf("client-%d", i%100), uint64(i))
		tx.From = "client"
		tx.SubmitTime = time.Now()
		_ = cluster.SubmitTx(tx)
		sent++
		if intervalMs > 0 {
			time.Sleep(time.Duration(intervalMs * float64(time.Millisecond)))
		}
	}

	waitForDrain(cluster, nodeCount)
	elapsed := time.Since(start).Seconds()
	sampleStatus := pickSampleStatus(cluster.GetAllStatus())
	netMetrics := cluster.Network.GetMetrics()
	actualTPS := float64(sent) / elapsed
	if actualTPS > float64(sent) {
		actualTPS = float64(sent)
	}

	return &BenchmarkResult{
		EngineType:    string(et),
		NodeCount:     nodeCount,
		TxCount:       sent,
		DurationSec:   elapsed,
		TPS:           actualTPS,
		P50LatencyMs:  sampleStatus.P50LatencyMs,
		P95LatencyMs:  sampleStatus.P95LatencyMs,
		P99LatencyMs:  sampleStatus.P99LatencyMs,
		TotalMessages: netMetrics.TotalMessages,
		TotalBytes:    netMetrics.TotalBytes,
	}, nil
}

func waitForDrain(cluster *network.Cluster, nodeCount int) {
	time.Sleep(2 * time.Second)
	timeout := 30 * time.Second
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
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func pickSampleStatus(statusMap map[string]core.EngineStatus) core.EngineStatus {
	for _, s := range statusMap {
		if s.IsLeader {
			return s
		}
	}
	for _, s := range statusMap {
		return s
	}
	return core.EngineStatus{}
}

// BenchmarkResult 是一次 benchmark 的结果摘要。
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

// RunAblation 运行消融实验。
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
