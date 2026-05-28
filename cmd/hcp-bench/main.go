package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fffeng99999/hcp-consensus/engine/core"
	"github.com/fffeng99999/hcp-consensus/engine/factory"
)

// main 是 hcp-bench 命令行工具的入口，支持多种基准测试子命令。
func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: hcp-bench <command> [args]")
		fmt.Println("Commands:")
		fmt.Println("  benchmark <engine> <nodes> <txs> [outfile]          运行单一基准测试")
		fmt.Println("  benchmark-group <engine> <nodes> <groups> <txs>     带分组的benchmark")
		fmt.Println("  compare <nodes> <txs> [outdir]                      运行多算法对比实验")
		fmt.Println("  ablation <nodes> <txs> [repeat] [outdir]            运行消融实验")
		fmt.Println("  saturation <engine> <nodes> [outdir]                运行饱和点扫描")
		fmt.Println("  group-scan <nodes> <txs> [outdir]                   运行分组参数扫描")
		fmt.Println("  model-fit <data-json> [outfile]                     拟合尾延迟模型")
		fmt.Println("  serve <engine> <nodes> <listen> [groups]            启动本地 engine 集群 HTTP 负载入口")
		fmt.Println("  smoke [outfile]                                      验证实验所需 engine 的最小可运行性")
		fmt.Println("Engines: pbft, tpbft, hotstuff, raft, cometbft-light, cometbft, hierarchical_tpbft, hierarchical_lightweight_tpbft")
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "benchmark":
		runBenchmark()
	case "benchmark-group":
		runBenchmarkGroup()
	case "compare":
		runCompare()
	case "ablation":
		runAblation()
	case "saturation":
		runSaturation()
	case "group-scan":
		runGroupScan()
	case "model-fit":
		runModelFit()
	case "serve":
		runServe()
	case "smoke":
		runSmoke()
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

// smokeResult 记录单次 smoke 测试的结果，用于汇总输出。
type smokeResult struct {
	Name   string                   `json:"name"`
	Engine string                   `json:"engine"`
	Nodes  int                      `json:"nodes"`
	Txs    int                      `json:"txs"`
	Groups int                      `json:"groups,omitempty"`
	Passed bool                     `json:"passed"`
	Error  string                   `json:"error,omitempty"`
	Result *factory.BenchmarkResult `json:"result,omitempty"`
}

// runSmoke 对所有支持的共识引擎执行最小可运行性验证（smoke test）。
// 若任一引擎未通过，则进程以非零状态退出。
func runSmoke() {
	outfile := ""
	if len(os.Args) >= 3 {
		outfile = os.Args[2]
	}
	// 定义各引擎的 smoke 测试用例，覆盖普通与分组场景。
	cases := []struct {
		name   string
		engine factory.EngineType
		nodes  int
		txs    int
		groups int
	}{
		{"PBFT baseline", factory.EnginePBFT, 4, 40, 0},
		{"tPBFT trust-filtered PBFT", factory.EngineTPBFT, 4, 40, 0},
		{"HotStuff chained BFT", factory.EngineHotStuff, 4, 40, 0},
		{"Raft crash-fault tolerant", factory.EngineRaft, 4, 40, 0},
		{"CometBFT-light BFT", factory.EngineCometBFTLight, 4, 40, 0},
		{"Legacy CometBFT-like BFT", factory.EngineCometBFT, 4, 40, 0},
		{"Hierarchical tPBFT", factory.EngineHierarchicalTPBFT, 8, 40, 2},
		{"Hierarchical lightweight tPBFT", factory.EngineHierarchicalLightweight, 8, 40, 2},
	}

	results := make([]smokeResult, 0, len(cases))
	failed := false
	for _, tc := range cases {
		fmt.Printf("Smoke: %s engine=%s nodes=%d txs=%d\n", tc.name, tc.engine, tc.nodes, tc.txs)
		var res *factory.BenchmarkResult
		var err error
		if tc.groups > 0 {
			// 分组场景需指定子引擎类型，轻量级层次化默认使用 raft。
			innerType := ""
			if tc.engine == factory.EngineHierarchicalLightweight {
				innerType = "raft"
			}
			res, err = factory.RunBenchmarkWithGroup(tc.engine, tc.nodes, tc.groups, innerType, tc.txs, 250, 5.0, 1000)
		} else {
			res, err = factory.RunBenchmark(tc.engine, tc.nodes, tc.txs, 250, 5.0, 1000)
		}
		item := smokeResult{
			Name:   tc.name,
			Engine: string(tc.engine),
			Nodes:  tc.nodes,
			Txs:    tc.txs,
			Groups: tc.groups,
			Result: res,
		}
		// 校验结果：无错误、交易数匹配、有网络消息、时长与 TPS 为正。
		if err != nil {
			item.Error = err.Error()
		} else if res == nil {
			item.Error = "empty benchmark result"
		} else if res.TxCount != tc.txs {
			item.Error = fmt.Sprintf("tx count mismatch: got %d want %d", res.TxCount, tc.txs)
		} else if res.TotalMessages == 0 {
			item.Error = "no consensus network messages recorded"
		} else if res.DurationSec <= 0 || res.TPS <= 0 {
			item.Error = "non-positive duration or TPS"
		} else {
			item.Passed = true
		}
		if !item.Passed {
			failed = true
			fmt.Printf("  FAIL: %s\n", item.Error)
		} else {
			fmt.Printf("  PASS: TPS=%.2f P99=%.2fms messages=%d\n", res.TPS, res.P99LatencyMs, res.TotalMessages)
		}
		results = append(results, item)
	}

	summary := map[string]any{
		"passed":  !failed,
		"results": results,
	}
	if outfile != "" {
		saveJSON(outfile, summary)
	}
	if failed {
		os.Exit(1)
	}
}

// runServe 启动一个 HTTP 服务，将外部交易请求转发到本地共识引擎集群。
// 提供 /health、/tx、/status 三个端点，支持优雅关闭。
func runServe() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: hcp-bench serve <engine> <nodes> <listen> [groups]")
		fmt.Println("Example: hcp-bench serve raft 8 127.0.0.1:8080")
		os.Exit(1)
	}
	engine := factory.EngineType(os.Args[2])
	nodes, _ := strconv.Atoi(os.Args[3])
	listen := os.Args[4]
	groups := 0
	if len(os.Args) >= 6 {
		groups, _ = strconv.Atoi(os.Args[5])
	}

	// cluster 抽象统一了普通集群与分组集群的公共操作。
	var cluster interface {
		StartAll() error
		StopAll()
		SubmitTx(*core.Tx) error
		GetAllStatus() map[string]core.EngineStatus
	}
	var netMetrics func() core.NetworkMetrics
	var err error
	if groups > 0 {
		c, buildErr := factory.BuildClusterWithGroup(engine, nodes, groups, "", 5.0, 1000)
		err = buildErr
		if c != nil {
			cluster = c
			netMetrics = c.Network.GetMetrics
		}
	} else {
		c, buildErr := factory.BuildCluster(engine, nodes, 5.0, 1000)
		err = buildErr
		if c != nil {
			cluster = c
			netMetrics = c.Network.GetMetrics
		}
	}
	if err != nil {
		fmt.Printf("build cluster failed: %v\n", err)
		os.Exit(1)
	}
	if err := cluster.StartAll(); err != nil {
		fmt.Printf("start cluster failed: %v\n", err)
		os.Exit(1)
	}
	defer cluster.StopAll()

	var received uint64
	var accepted uint64
	startedAt := time.Now()
	mux := http.NewServeMux()
	// /health 健康检查端点。
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true})
	})
	// /tx 接收 POST 请求体作为交易内容，构造 Tx 并提交到集群。
	mux.HandleFunc("/tx", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		now := time.Now()
		seq := atomic.AddUint64(&received, 1)
		tx := core.NewTx(body, clientID(r.RemoteAddr), seq)
		tx.SubmitTime = now
		if err := cluster.SubmitTx(tx); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		acceptedSeq := atomic.AddUint64(&accepted, 1)
		writeJSON(w, map[string]any{"ok": true, "tx_id": tx.ID, "seq": acceptedSeq})
	})
	// /status 返回集群状态、网络指标及基于 Leader 统计的 TPS 估算。
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		status := cluster.GetAllStatus()
		var sample core.EngineStatus
		// 优先选取 Leader 节点状态作为样本。
		for _, s := range status {
			if s.IsLeader {
				sample = s
				break
			}
		}
		if sample.NodeID == "" {
			for _, s := range status {
				sample = s
				break
			}
		}
		// 分组模式下汇总所有 Leader 的指标，取最大值或累加值。
		if groups > 0 {
			var committed uint64
			var pending int
			var firstNano int64
			var lastNano int64
			var maxHeight uint64
			p50, p95, p99 := 0.0, 0.0, 0.0
			for _, s := range status {
				if !s.IsLeader {
					continue
				}
				committed += s.CommittedTxs
				pending += s.PendingTxCount
				if s.FirstSubmitUnixNano > 0 && (firstNano == 0 || s.FirstSubmitUnixNano < firstNano) {
					firstNano = s.FirstSubmitUnixNano
				}
				if s.LastCommitUnixNano > lastNano {
					lastNano = s.LastCommitUnixNano
				}
				if s.Height > maxHeight {
					maxHeight = s.Height
				}
				if s.P50LatencyMs > p50 {
					p50 = s.P50LatencyMs
				}
				if s.P95LatencyMs > p95 {
					p95 = s.P95LatencyMs
				}
				if s.P99LatencyMs > p99 {
					p99 = s.P99LatencyMs
				}
			}
			if committed > 0 {
				sample.CommittedTxs = committed
				sample.PendingTxCount = pending
				sample.FirstSubmitUnixNano = firstNano
				sample.LastCommitUnixNano = lastNano
				sample.Height = maxHeight
				sample.P50LatencyMs = p50
				sample.P95LatencyMs = p95
				sample.P99LatencyMs = p99
			}
		}
		acceptedTxs := atomic.LoadUint64(&accepted)
		committedTxs := sample.CommittedTxs
		firstNano := sample.FirstSubmitUnixNano
		completedNano := sample.LastCommitUnixNano
		completionDuration := 0.0
		benchmarkTPS := 0.0
		// 基于首次提交与最后提交时间差计算实际 TPS。
		if committedTxs > 0 && firstNano > 0 && completedNano > firstNano {
			completionDuration = float64(completedNano-firstNano) / float64(time.Second)
			benchmarkTPS = float64(committedTxs) / completionDuration
		}
		writeJSON(w, map[string]any{
			"engine":                string(engine),
			"nodes":                 nodes,
			"groups":                groups,
			"received_txs":          atomic.LoadUint64(&received),
			"accepted_txs":          acceptedTxs,
			"committed_txs":         committedTxs,
			"completion_duration_s": completionDuration,
			"benchmark_tps":         benchmarkTPS,
			"uptime_s":              time.Since(startedAt).Seconds(),
			"sample_status":         sample,
			"node_status":           status,
			"network":               netMetrics(),
		})
	})

	server := &http.Server{Addr: listen, Handler: mux}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Printf("hcp-bench serve: engine=%s nodes=%d groups=%d listen=%s\n", engine, nodes, groups, listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Printf("http server failed: %v\n", err)
		os.Exit(1)
	}
}

// clientID 根据远程地址生成一个稳定的负载生成器标识符。
func clientID(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return "loadgen"
	}
	sum := sha256.Sum256([]byte(remoteAddr))
	return "loadgen-" + hex.EncodeToString(sum[:4])
}

// writeJSON 将 v 编码为 JSON 并写入响应，同时设置 Content-Type 头。
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// runBenchmark 执行单一引擎的基准测试，参数从命令行读取，结果可选输出到文件。
func runBenchmark() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: hcp-bench benchmark <engine> <nodes> <txs> [outfile]")
		os.Exit(1)
	}
	engine := factory.EngineType(os.Args[2])
	nodes, _ := strconv.Atoi(os.Args[3])
	txs, _ := strconv.Atoi(os.Args[4])
	outfile := ""
	if len(os.Args) >= 6 {
		outfile = os.Args[5]
	}
	res, err := factory.RunBenchmark(engine, nodes, txs, 250, 0.2, 1000)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	printResult(res)
	if outfile != "" {
		saveJSON(outfile, res)
	}
}

// runBenchmarkGroup 执行带分组参数的基准测试，适用于层次化共识引擎。
func runBenchmarkGroup() {
	if len(os.Args) < 6 {
		fmt.Println("Usage: hcp-bench benchmark-group <engine> <nodes> <groups> <txs> [outfile]")
		os.Exit(1)
	}
	engine := factory.EngineType(os.Args[2])
	nodes, _ := strconv.Atoi(os.Args[3])
	groups, _ := strconv.Atoi(os.Args[4])
	txs, _ := strconv.Atoi(os.Args[5])
	outfile := ""
	if len(os.Args) >= 7 {
		outfile = os.Args[6]
	}
	// 轻量级层次化引擎默认使用 raft 作为子共识。
	innerType := "pbft"
	if engine == factory.EngineHierarchicalLightweight {
		innerType = "raft"
	}
	res, err := factory.RunBenchmarkWithGroup(engine, nodes, groups, innerType, txs, 250, 0.2, 1000)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	printResult(res)
	if outfile != "" {
		saveJSON(outfile, res)
	}
}

// runCompare 运行多算法对比实验，默认 16 节点 1000 交易，结果保存为 JSON。
func runCompare() {
	nodes, txs := 16, 1000
	outdir := ""
	if len(os.Args) >= 3 {
		nodes, _ = strconv.Atoi(os.Args[2])
	}
	if len(os.Args) >= 4 {
		txs, _ = strconv.Atoi(os.Args[3])
	}
	if len(os.Args) >= 5 {
		outdir = os.Args[4]
	}

	engines := []struct {
		name string
		et   factory.EngineType
	}{
		{"PBFT", factory.EnginePBFT},
		{"tPBFT", factory.EngineTPBFT},
		{"HotStuff", factory.EngineHotStuff},
		{"Raft", factory.EngineRaft},
		{"CometBFT-light", factory.EngineCometBFTLight},
		{"Hierarchical_tPBFT", factory.EngineHierarchicalTPBFT},
	}

	fmt.Printf("Running comparison experiment: nodes=%d txs=%d\n", nodes, txs)
	results := make(map[string]*factory.BenchmarkResult)
	for _, e := range engines {
		fmt.Printf("  Testing %s...\n", e.name)
		res, err := factory.RunBenchmark(e.et, nodes, txs, 250, 0.2, 1000)
		if err != nil {
			fmt.Printf("    Error: %v\n", err)
			continue
		}
		results[e.name] = res
		printResult(res)
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	filename := fmt.Sprintf("compare_n%d_t%d.json", nodes, txs)
	if outdir != "" {
		os.MkdirAll(outdir, 0755)
		filename = filepath.Join(outdir, filename)
	}
	os.WriteFile(filename, data, 0644)
	fmt.Printf("Saved to %s\n", filename)
}

// runAblation 运行消融实验，对多种配置重复执行基准测试并输出平均值汇总。
func runAblation() {
	nodes, txs, repeat := 32, 1000, 5
	outdir := ""
	if len(os.Args) >= 3 {
		nodes, _ = strconv.Atoi(os.Args[2])
	}
	if len(os.Args) >= 4 {
		txs, _ = strconv.Atoi(os.Args[3])
	}
	if len(os.Args) >= 5 {
		repeat, _ = strconv.Atoi(os.Args[4])
	}
	if len(os.Args) >= 6 {
		outdir = os.Args[5]
	}

	configs := []struct {
		name string
		et   factory.EngineType
	}{
		{"A_Baseline", factory.EnginePBFT},
		{"B_tPBFT", factory.EngineTPBFT},
		{"C_Hierarchical", factory.EngineHierarchicalTPBFT},
		{"D_Lightweight", factory.EngineHierarchicalLightweight},
		{"E_HotStuff", factory.EngineHotStuff},
		{"F_Raft", factory.EngineRaft},
	}

	allRuns := make(map[string][]*factory.BenchmarkResult)
	for _, cfg := range configs {
		fmt.Printf("=== Group %s ===\n", cfg.name)
		var runs []*factory.BenchmarkResult
		for r := 0; r < repeat; r++ {
			fmt.Printf("  Run %d/%d...\n", r+1, repeat)
			res, err := factory.RunBenchmark(cfg.et, nodes, txs, 250, 0.2, 1000)
			if err != nil {
				fmt.Printf("    Error: %v\n", err)
				continue
			}
			printResult(res)
			runs = append(runs, res)
		}
		allRuns[cfg.name] = runs
	}

	summary := make(map[string]map[string]float64)
	for name, runs := range allRuns {
		if len(runs) == 0 {
			continue
		}
		summary[name] = avgResults(runs)
	}

	out := map[string]interface{}{
		"raw":     allRuns,
		"summary": summary,
		"metadata": map[string]interface{}{
			"nodes":  nodes,
			"txs":    txs,
			"repeat": repeat,
		},
	}
	filename := fmt.Sprintf("ablation_n%d_t%d.json", nodes, txs)
	if outdir != "" {
		os.MkdirAll(outdir, 0755)
		filename = filepath.Join(outdir, filename)
	}
	saveJSON(filename, out)
	fmt.Printf("Saved to %s\n", filename)
}

// avgResults 计算多次基准测试结果的平均值，返回包含 TPS、延迟、消息数等指标的 map。
func avgResults(runs []*factory.BenchmarkResult) map[string]float64 {
	var tps, p50, p95, p99, msgs, dur float64
	n := float64(len(runs))
	for _, r := range runs {
		tps += r.TPS
		p50 += r.P50LatencyMs
		p95 += r.P95LatencyMs
		p99 += r.P99LatencyMs
		msgs += float64(r.TotalMessages)
		dur += r.DurationSec
	}
	return map[string]float64{
		"tps":      tps / n,
		"p50_ms":   p50 / n,
		"p95_ms":   p95 / n,
		"p99_ms":   p99 / n,
		"messages": msgs / n,
		"duration": dur / n,
	}
}

// runSaturation 对指定引擎执行负载递增的饱和点扫描实验。
func runSaturation() {
	engine := factory.EnginePBFT
	nodes := 16
	outdir := ""
	if len(os.Args) >= 3 {
		engine = factory.EngineType(os.Args[2])
	}
	if len(os.Args) >= 4 {
		nodes, _ = strconv.Atoi(os.Args[3])
	}
	if len(os.Args) >= 5 {
		outdir = os.Args[4]
	}

	results := make(map[int]*factory.BenchmarkResult)
	// 负载从 20 tx/s 递增到 120 tx/s，步长 20。
	for load := 20; load <= 120; load += 20 {
		txCount := load * 10
		fmt.Printf("  Testing load=%d tx/s (txCount=%d)...\n", load, txCount)
		res, err := factory.RunBenchmark(engine, nodes, txCount, 250, 0.2, 1000)
		if err != nil {
			fmt.Printf("    Error: %v\n", err)
			continue
		}
		results[load] = res
		fmt.Printf("    TPS=%.2f P99=%.2fms\n", res.TPS, res.P99LatencyMs)
	}
	filename := fmt.Sprintf("saturation_%s_n%d.json", engine, nodes)
	if outdir != "" {
		os.MkdirAll(outdir, 0755)
		filename = filepath.Join(outdir, filename)
	}
	saveJSON(filename, results)
	fmt.Printf("Saved to %s\n", filename)
}

// runGroupScan 扫描不同分组数量对层次化 tPBFT 性能的影响。
func runGroupScan() {
	nodes, txs := 32, 1000
	outdir := ""
	if len(os.Args) >= 3 {
		nodes, _ = strconv.Atoi(os.Args[2])
	}
	if len(os.Args) >= 4 {
		txs, _ = strconv.Atoi(os.Args[3])
	}
	if len(os.Args) >= 5 {
		outdir = os.Args[4]
	}

	results := make(map[int]*factory.BenchmarkResult)
	// 遍历常见分组数，跳过不能整除节点数的分组。
	for _, g := range []int{1, 2, 4, 8, 16} {
		if nodes%g != 0 {
			continue
		}
		fmt.Printf("  Testing groups=%d...\n", g)
		res, err := factory.RunBenchmarkWithGroup(factory.EngineHierarchicalTPBFT, nodes, g, "pbft", txs, 250, 0.2, 1000)
		if err != nil {
			fmt.Printf("    Error: %v\n", err)
			continue
		}
		results[g] = res
		printResult(res)
	}
	filename := fmt.Sprintf("groupscan_n%d_t%d.json", nodes, txs)
	if outdir != "" {
		os.MkdirAll(outdir, 0755)
		filename = filepath.Join(outdir, filename)
	}
	saveJSON(filename, results)
	fmt.Printf("Saved to %s\n", filename)
}

// runModelFit 读取按节点数组织的基准测试结果，对每个引擎的 P99 延迟进行二次多项式拟合。
func runModelFit() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: hcp-bench model-fit <data-json> [outfile]")
		os.Exit(1)
	}
	infile := os.Args[2]
	outfile := "model_fit.json"
	if len(os.Args) >= 4 {
		outfile = os.Args[3]
	}
	data, _ := os.ReadFile(infile)
	var byNode map[int]map[string]*factory.BenchmarkResult
	json.Unmarshal(data, &byNode)

	fit := make(map[string]map[string]float64)
	for engine := range byNode[8] {
		var xs, ys []float64
		// 节点数从 8 到 32，步长 8，收集 P99 延迟数据。
		for n := 8; n <= 32; n += 8 {
			if byNode[n] == nil || byNode[n][engine] == nil {
				continue
			}
			xs = append(xs, float64(n))
			ys = append(ys, byNode[n][engine].P99LatencyMs)
		}
		if len(xs) >= 3 {
			alpha, beta, gamma, r2 := poly2Fit(xs, ys)
			fit[engine] = map[string]float64{
				"alpha": alpha,
				"beta":  beta,
				"gamma": gamma,
				"r2":    r2,
			}
		}
	}
	saveJSON(outfile, fit)
	fmt.Printf("Saved model fit to %s\n", outfile)
}

// poly2Fit 对给定的 (x, y) 数据点进行二次多项式拟合 y = alpha*x^2 + beta*x + gamma，并返回 R^2。
func poly2Fit(x, y []float64) (alpha, beta, gamma, r2 float64) {
	n := float64(len(x))
	var sx, sx2, sx3, sx4, sy, sxy, sx2y float64
	for i := range x {
		sx += x[i]
		sx2 += x[i] * x[i]
		sx3 += x[i] * x[i] * x[i]
		sx4 += x[i] * x[i] * x[i] * x[i]
		sy += y[i]
		sxy += x[i] * y[i]
		sx2y += x[i] * x[i] * y[i]
	}
	// 恰好 3 个点时直接解线性方程组；否则退化为常数均值。
	if len(x) == 3 {
		A := [3][3]float64{{x[0] * x[0], x[0], 1}, {x[1] * x[1], x[1], 1}, {x[2] * x[2], x[2], 1}}
		B := [3]float64{y[0], y[1], y[2]}
		alpha, beta, gamma = solve3x3(A, B)
	} else {
		alpha, beta, gamma = 0, 0, sy/n
	}
	var ssTot, ssRes float64
	yMean := sy / n
	for i := range x {
		yPred := alpha*x[i]*x[i] + beta*x[i] + gamma
		ssRes += (y[i] - yPred) * (y[i] - yPred)
		ssTot += (y[i] - yMean) * (y[i] - yMean)
	}
	if ssTot > 0 {
		r2 = 1 - ssRes/ssTot
	} else {
		r2 = 1
	}
	return
}

// solve3x3 使用克莱姆法则求解 3x3 线性方程组 A*x = B。
func solve3x3(A [3][3]float64, B [3]float64) (x, y, z float64) {
	det := A[0][0]*(A[1][1]*A[2][2]-A[1][2]*A[2][1]) -
		A[0][1]*(A[1][0]*A[2][2]-A[1][2]*A[2][0]) +
		A[0][2]*(A[1][0]*A[2][1]-A[1][1]*A[2][0])
	if math.Abs(det) < 1e-10 {
		return 0, 0, 0
	}
	detX := B[0]*(A[1][1]*A[2][2]-A[1][2]*A[2][1]) -
		A[0][1]*(B[1]*A[2][2]-A[1][2]*B[2]) +
		A[0][2]*(B[1]*A[2][1]-A[1][1]*B[2])
	detY := A[0][0]*(B[1]*A[2][2]-A[1][2]*B[2]) -
		B[0]*(A[1][0]*A[2][2]-A[1][2]*A[2][0]) +
		A[0][2]*(A[1][0]*B[2]-B[1]*A[2][0])
	detZ := A[0][0]*(A[1][1]*B[2]-B[1]*A[2][1]) -
		A[0][1]*(A[1][0]*B[2]-B[1]*A[2][0]) +
		B[0]*(A[1][0]*A[2][1]-A[1][1]*A[2][0])
	return detX / det, detY / det, detZ / det
}

// printResult 在控制台打印 BenchmarkResult 的核心指标。
func printResult(res *factory.BenchmarkResult) {
	fmt.Printf("  Engine: %s | Nodes: %d | TPS: %.2f | P50: %.2fms | P95: %.2fms | P99: %.2fms | Msgs: %d | Bytes: %d\n",
		res.EngineType, res.NodeCount, res.TPS, res.P50LatencyMs, res.P95LatencyMs, res.P99LatencyMs,
		res.TotalMessages, res.TotalBytes)
}

// saveJSON 将 v 序列化为缩进 JSON 并写入指定路径，权限 0644。
func saveJSON(path string, v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	os.WriteFile(path, data, 0644)
}
