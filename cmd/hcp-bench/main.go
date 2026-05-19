package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"

	"github.com/fffeng99999/hcp-consensus/engine/factory"
)

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
		fmt.Println("Engines: pbft, tpbft, hotstuff, raft, cometbft, hierarchical_tpbft, hierarchical_lightweight_tpbft")
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
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

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
		{"CometBFT", factory.EngineCometBFT},
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

func printResult(res *factory.BenchmarkResult) {
	fmt.Printf("  Engine: %s | Nodes: %d | TPS: %.2f | P50: %.2fms | P95: %.2fms | P99: %.2fms | Msgs: %d | Bytes: %d\n",
		res.EngineType, res.NodeCount, res.TPS, res.P50LatencyMs, res.P95LatencyMs, res.P99LatencyMs,
		res.TotalMessages, res.TotalBytes)
}

func saveJSON(path string, v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	os.WriteFile(path, data, 0644)
}
