package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fffeng99999/hcap-consensus/engine/core"
	"github.com/fffeng99999/hcap-consensus/engine/factory"
	"github.com/quic-go/quic-go"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: hcap-bench <command> [args]")
	fmt.Println("Commands:")
	fmt.Println("  benchmark <engine> <nodes> <txs> [outfile]          run single benchmark")
	fmt.Println("  benchmark-group <engine> <nodes> <groups> <txs>     run grouped benchmark")
	fmt.Println("  compare <nodes> <txs> [outdir]                      run multi-engine comparison")
	fmt.Println("  ablation <nodes> <txs> [repeat] [outdir]            run ablation experiment")
	fmt.Println("  saturation <engine> <nodes> [outdir]                run saturation scan")
	fmt.Println("  group-scan <nodes> <txs> [outdir]                   run group parameter scan")
	fmt.Println("  model-fit <data-json> [outfile]                     fit latency model")
	fmt.Println("  serve <engine> <nodes> <listen> [groups]            start local engine cluster HTTP endpoint")
	fmt.Println("  smoke [outfile]                                      run engine smoke tests")
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

func runSmoke() {
	outfile := ""
	if len(os.Args) >= 3 {
		outfile = os.Args[2]
	}
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

func runServe() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: hcap-bench serve <engine> <nodes> <listen> [groups]")
		fmt.Println("Example: hcap-bench serve raft 8 127.0.0.1:8080")
		os.Exit(1)
	}
	engine := factory.EngineType(os.Args[2])
	nodes, _ := strconv.Atoi(os.Args[3])
	listen := os.Args[4]
	groups := 0
	if len(os.Args) >= 6 {
		groups, _ = strconv.Atoi(os.Args[5])
	}

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
	submitPayload := func(body []byte, remoteAddr string) (string, uint64, error) {
		now := time.Now()
		seq := atomic.AddUint64(&received, 1)
		tx := core.NewTx(body, clientID(remoteAddr), seq)
		tx.SubmitTime = now
		if err := cluster.SubmitTx(tx); err != nil {
			return "", 0, err
		}
		acceptedSeq := atomic.AddUint64(&accepted, 1)
		return tx.ID, acceptedSeq, nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true})
	})
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
		txID, acceptedSeq, err := submitPayload(body, r.RemoteAddr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "tx_id": txID, "seq": acceptedSeq})
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		status := cluster.GetAllStatus()
		var sample core.EngineStatus
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
	go serveQUIC(ctx, listen, submitPayload)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Printf("hcap-bench serve: engine=%s nodes=%d groups=%d listen=%s quic=%s\n", engine, nodes, groups, listen, listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Printf("http server failed: %v\n", err)
		os.Exit(1)
	}
}

func serveQUIC(ctx context.Context, listen string, submit func([]byte, string) (string, uint64, error)) {
	listener, err := quic.ListenAddr(listen, generateQUICServerTLSConfig(), &quic.Config{})
	if err != nil {
		fmt.Printf("quic server disabled: %v\n", err)
		return
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	fmt.Printf("hcap-bench quic tx listener: %s\n", listen)
	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			fmt.Printf("quic accept failed: %v\n", err)
			continue
		}
		go handleQUICConnection(ctx, conn, submit)
	}
}

func handleQUICConnection(ctx context.Context, conn quic.Connection, submit func([]byte, string) (string, uint64, error)) {
	for {
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			return
		}
		go handleQUICStream(stream, conn.RemoteAddr().String(), submit)
	}
}

func handleQUICStream(stream quic.Stream, remoteAddr string, submit func([]byte, string) (string, uint64, error)) {
	defer stream.Close()
	body, err := io.ReadAll(io.LimitReader(stream, 4<<20))
	if err != nil {
		_, _ = stream.Write([]byte("ERR read payload\n"))
		return
	}
	txID, seq, err := submit(body, remoteAddr)
	if err != nil {
		_, _ = stream.Write([]byte("ERR " + err.Error() + "\n"))
		return
	}
	_, _ = stream.Write([]byte(fmt.Sprintf("OK %s %d\n", txID, seq)))
}

func generateQUICServerTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName: "hcap-bench",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"hcap-bench", "localhost"},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{derBytes},
			PrivateKey:  key,
		}},
		NextProtos: []string{"hcap-quic"},
	}
}

func clientID(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return "loadgen"
	}
	sum := sha256.Sum256([]byte(remoteAddr))
	return "loadgen-" + hex.EncodeToString(sum[:4])
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func runBenchmark() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: hcap-bench benchmark <engine> <nodes> <txs> [outfile]")
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
		fmt.Println("Usage: hcap-bench benchmark-group <engine> <nodes> <groups> <txs> [outfile]")
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
		fmt.Println("Usage: hcap-bench model-fit <data-json> [outfile]")
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
