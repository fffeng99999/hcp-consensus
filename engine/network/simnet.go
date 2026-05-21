package network

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/fffeng99999/hcp-consensus/engine/core"
)

// SimNet is an in-memory network with latency, bandwidth, and message metrics.
type SimNet struct {
	mu               sync.RWMutex
	handlers         map[string]func(*core.Message)
	latencyMs        float64
	bandwidth        float64
	msgCounter       uint64
	byteCounter      uint64
	broadcastCounter uint64
	onSend           func(msg *core.Message)
}

func NewSimNet() *SimNet {
	return &SimNet{
		handlers:  make(map[string]func(*core.Message)),
		latencyMs: 0.2,
		bandwidth: 1000,
	}
}

func (n *SimNet) RegisterHandler(nodeID string, handler func(*core.Message)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.handlers[nodeID] = handler
}

func (n *SimNet) SetLatency(latencyMs float64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.latencyMs = latencyMs
}

func (n *SimNet) SetBandwidth(mbps float64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.bandwidth = mbps
}

func (n *SimNet) SetSendHook(hook func(msg *core.Message)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onSend = hook
}

func (n *SimNet) Send(msg *core.Message) error {
	if msg == nil {
		return nil
	}
	data, _ := json.Marshal(msg)
	size := uint64(len(data))

	n.mu.Lock()
	latency := n.latencyMs
	bw := n.bandwidth
	n.msgCounter++
	n.byteCounter += size
	if n.onSend != nil {
		n.onSend(msg)
	}
	handler, ok := n.handlers[msg.To]
	n.mu.Unlock()

	if !ok {
		return fmt.Errorf("node %s not found", msg.To)
	}

	txDelayMs := 0.0
	if bw > 0 {
		txDelayMs = (float64(size) * 8.0 / 1000000.0) / bw * 1000.0
	}
	totalDelay := time.Duration((latency + txDelayMs) * float64(time.Millisecond))

	go func() {
		time.Sleep(totalDelay)
		handler(msg)
	}()
	return nil
}

func (n *SimNet) Broadcast(msg *core.Message) error {
	if msg == nil {
		return nil
	}
	data, _ := json.Marshal(msg)
	size := uint64(len(data))

	n.mu.Lock()
	latency := n.latencyMs
	bw := n.bandwidth
	if n.onSend != nil {
		n.onSend(msg)
	}
	handlers := make(map[string]func(*core.Message))
	for k, v := range n.handlers {
		handlers[k] = v
	}
	n.mu.Unlock()

	txDelayMs := 0.0
	if bw > 0 {
		txDelayMs = (float64(size) * 8.0 / 1000000.0) / bw * 1000.0
	}
	totalDelay := time.Duration((latency + txDelayMs) * float64(time.Millisecond))

	for nodeID, handler := range handlers {
		if nodeID == msg.From {
			continue
		}
		n.mu.Lock()
		n.msgCounter++
		n.byteCounter += size
		n.broadcastCounter++
		n.mu.Unlock()

		go func(h func(*core.Message), m *core.Message) {
			time.Sleep(totalDelay)
			h(m)
		}(handler, msg)
	}
	return nil
}

func (n *SimNet) GetMetrics() core.NetworkMetrics {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return core.NetworkMetrics{
		TotalMessages:  n.msgCounter,
		TotalBytes:     n.byteCounter,
		BroadcastCount: n.broadcastCounter,
		AvgLatencyMs:   n.latencyMs,
	}
}

// Cluster manages all in-memory consensus nodes.
type Cluster struct {
	mu         sync.Mutex
	Network    *SimNet
	Nodes      map[string]core.ConsensusEngine
	TxPools    map[string]core.TxPool
	Started    bool
	nextSubmit uint64
}

func NewCluster() *Cluster {
	return &Cluster{
		Network: NewSimNet(),
		Nodes:   make(map[string]core.ConsensusEngine),
		TxPools: make(map[string]core.TxPool),
	}
}

func (c *Cluster) AddNode(nodeID string, engine core.ConsensusEngine, pool core.TxPool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Nodes[nodeID] = engine
	c.TxPools[nodeID] = pool
}

func (c *Cluster) StartAll() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, node := range c.Nodes {
		if err := node.Start(); err != nil {
			return err
		}
	}
	c.Started = true
	return nil
}

func (c *Cluster) StopAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, node := range c.Nodes {
		node.Stop()
	}
	c.Started = false
}

func (c *Cluster) SubmitTx(tx *core.Tx) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.Nodes) == 0 {
		return nil
	}
	nodeIDs := make([]string, 0, len(c.Nodes))
	for nodeID := range c.Nodes {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)
	nodeID := nodeIDs[int(c.nextSubmit%uint64(len(nodeIDs)))]
	c.nextSubmit++
	return c.Nodes[nodeID].SubmitTx(tx)
}

func (c *Cluster) GetAllStatus() map[string]core.EngineStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	status := make(map[string]core.EngineStatus)
	for id, node := range c.Nodes {
		status[id] = node.GetStatus()
	}
	return status
}

func (c *Cluster) WaitForHeight(target uint64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		allReached := true
		for _, node := range c.Nodes {
			if node.GetStatus().Height < target {
				allReached = false
				break
			}
		}
		if allReached {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
