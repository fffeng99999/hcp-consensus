package main

import (
	"fmt"
	"time"
	"github.com/fffeng99999/hcp-consensus/engine/common"
	"github.com/fffeng99999/hcp-consensus/engine/core"
	"github.com/fffeng99999/hcp-consensus/engine/network"
	"github.com/fffeng99999/hcp-consensus/engine/hierarchical"
	"crypto/ed25519"
	"crypto/rand"
)

func main() {
	cluster := network.NewCluster()
	cluster.Network.SetLatency(5)
	cluster.Network.SetBandwidth(1000)

	nodeIDs := []string{"node-0", "node-1", "node-2", "node-3", "node-4", "node-5", "node-6", "node-7"}
	pubKeys := make(map[string]ed25519.PublicKey)
	privKeys := make(map[string]ed25519.PrivateKey)
	for _, id := range nodeIDs {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		privKeys[id] = priv
		pubKeys[id] = priv.Public().(ed25519.PublicKey)
	}

	for _, id := range nodeIDs {
		cfg := &core.NodeConfig{
			NodeID: id, AllNodes: nodeIDs,
			PrivateKey: privKeys[id], PublicKeys: pubKeys,
		}
		e := hierarchical.NewHierarchicalTPBFT(2, "pbft", 0.6)
		pool := common.NewMemTxPool(10000)
		exec := common.NewSimpleExecutor()
		e.Init(cfg, cluster.Network, pool, exec)
		cluster.AddNode(id, e, pool)
	}

	fmt.Println("Starting...")
	cluster.StartAll()
	fmt.Println("Started, waiting...")
	time.Sleep(800 * time.Millisecond)

	for i := 0; i < 5; i++ {
		tx := core.NewTx([]byte(fmt.Sprintf("test%d", i)), "client", uint64(i))
		tx.SubmitTime = time.Now()
		cluster.SubmitTx(tx)
	}

	fmt.Println("Waiting for commit...")
	time.Sleep(3 * time.Second)
	fmt.Println("Done waiting")

	for id, node := range cluster.Nodes {
		s := node.GetStatus()
		fmt.Printf("Node %s: Height=%d Pending=%d P99=%.2f IsLeader=%v\n", id, s.Height, s.PendingTxCount, s.P99LatencyMs, s.IsLeader)
	}
}
