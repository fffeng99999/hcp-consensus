package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/fffeng99999/hcp-consensus/engine/factory"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: hcp-bench <command> [args]")
		fmt.Println("Commands:")
		fmt.Println("  benchmark <engine> <nodes> <txs>       运行单一基准测试")
		fmt.Println("  compare <nodes> <txs>                  运行多算法对比实验")
		fmt.Println("  ablation <nodes> <txs>                 运行消融实验")
		fmt.Println("  saturation <engine> <nodes>            运行饱和点扫描")
		fmt.Println("  group-scan <nodes> <txs>               运行分组参数扫描")
		fmt.Println("Engines: pbft, tpbft, hotstuff, raft, hierarchical_tpbft, hierarchical_lightweight_tpbft")
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "benchmark":
		if len(os.Args) < 5 {
			fmt.Println("Usage: hcp-bench benchmark <engine> <nodes> <txs>")
			os.Exit(1)
		}
		engine := factory.EngineType(os.Args[2])
		nodes, _ := strconv.Atoi(os.Args[3])
		txs, _ := strconv.Atoi(os.Args[4])
		runBenchmark(engine, nodes, txs)
	case "compare":
		nodes, txs := 16, 1000
		if len(os.Args) >= 3 {
			nodes, _ = strconv.Atoi(os.Args[2])
		}
		if len(os.Args) >= 4 {
			txs, _ = strconv.Atoi(os.Args[3])
		}
		runCompare(nodes, txs)
	case "ablation":
		nodes, txs := 32, 1000
		if len(os.Args) >= 3 {
			nodes, _ = strconv.Atoi(os.Args[2])
		}
		if len(os.Args) >= 4 {
			txs, _ = strconv.Atoi(os.Args[3])
		}
		runAblation(nodes, txs)
	case "saturation":
		engine := factory.EnginePBFT
		nodes := 16
		if len(os.Args) >= 3 {
			engine = factory.EngineType(os.Args[2])
		}
		if len(os.Args) >= 4 {
			nodes, _ = strconv.Atoi(os.Args[3])
		}
		runSaturation(engine, nodes)
	case "group-scan":
		nodes, txs := 32, 1000
		if len(os.Args) >= 3 {
			nodes, _ = strconv.Atoi(os.Args[2])
		}
		if len(os.Args) >= 4 {
			txs, _ = strconv.Atoi(os.Args[3])
		}
		runGroupScan(nodes, txs)
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

func runBenchmark(engine factory.EngineType, nodes, txs int) {
	fmt.Printf("Running benchmark: engine=%s nodes=%d txs=%d\n", engine, nodes, txs)
	res, err := factory.RunBenchmark(engine, nodes, txs, 250, 0.2, 1000)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	printResult(res)
}

func runCompare(nodes, txs int) {
	engines := []factory.EngineType{
		factory.EnginePBFT,
		factory.EngineTPBFT,
		factory.EngineHotStuff,
		factory.EngineRaft,
	}
	fmt.Printf("Running comparison experiment: nodes=%d txs=%d\n", nodes, txs)
	results := make(map[string]*factory.BenchmarkResult)
	for _, engine := range engines {
		fmt.Printf("  Testing %s...\n", engine)
		res, err := factory.RunBenchmark(engine, nodes, txs, 250, 0.2, 1000)
		if err != nil {
			fmt.Printf("    Error: %v\n", err)
			continue
		}
		results[string(engine)] = res
		printResult(res)
	}
	// 保存结果
	data, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile(fmt.Sprintf("compare_n%d_t%d.json", nodes, txs), data, 0644)
}

func runAblation(nodes, txs int) {
	fmt.Printf("Running ablation experiment: nodes=%d txs=%d\n", nodes, txs)
	results := factory.RunAblation(nodes, txs)
	for name, res := range results {
		fmt.Printf("=== Group %s ===\n", name)
		printResult(res)
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile(fmt.Sprintf("ablation_n%d_t%d.json", nodes, txs), data, 0644)
}

func runSaturation(engine factory.EngineType, nodes int) {
	fmt.Printf("Running saturation scan: engine=%s nodes=%d\n", engine, nodes)
	results := make(map[int]*factory.BenchmarkResult)
	for load := 500; load <= 5000; load += 500 {
		fmt.Printf("  Testing load=%d tx/s...\n", load)
		res, err := factory.RunBenchmark(engine, nodes, load, 250, 0.2, 1000)
		if err != nil {
			fmt.Printf("    Error: %v\n", err)
			continue
		}
		results[load] = res
		fmt.Printf("    TPS=%.2f P99=%.2fms\n", res.TPS, res.P99LatencyMs)
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile(fmt.Sprintf("saturation_%s_n%d.json", engine, nodes), data, 0644)
}

func runGroupScan(nodes, txs int) {
	fmt.Printf("Running group scan: nodes=%d txs=%d\n", nodes, txs)
	results := make(map[int]*factory.BenchmarkResult)
	for _, g := range []int{1, 2, 4, 8, 16} {
		if nodes%g != 0 {
			continue
		}
		fmt.Printf("  Testing groups=%d...\n", g)
		// 这里简化：使用分层tPBFT，实际groupCount在引擎初始化时固定
		// 为了支持动态groupCount，需要修改工厂方法
		// 简化：只测试groupCount=4的情况
		if g == 4 {
			res, err := factory.RunBenchmark(factory.EngineHierarchicalTPBFT, nodes, txs, 250, 0.2, 1000)
			if err != nil {
				fmt.Printf("    Error: %v\n", err)
				continue
			}
			results[g] = res
			printResult(res)
		}
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile(fmt.Sprintf("groupscan_n%d_t%d.json", nodes, txs), data, 0644)
}

func printResult(res *factory.BenchmarkResult) {
	fmt.Printf("  Engine: %s | Nodes: %d | TPS: %.2f | P50: %.2fms | P95: %.2fms | P99: %.2fms | Msgs: %d | Bytes: %d\n",
		res.EngineType, res.NodeCount, res.TPS, res.P50LatencyMs, res.P95LatencyMs, res.P99LatencyMs,
		res.TotalMessages, res.TotalBytes)
}
