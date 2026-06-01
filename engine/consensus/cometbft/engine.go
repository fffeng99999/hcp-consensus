package cometbft

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fffeng99999/hcap-consensus/engine/common"
	"github.com/fffeng99999/hcap-consensus/engine/core"
)

// Step 表示 CometBFT 单个高度和轮次内的共识阶段。
type Step int

const (
	StepPropose Step = iota
	StepPrevote
	StepPrecommit
	StepCommit
)

// RoundState 保存某个高度的提案、Prevote 和 Precommit 投票。
type RoundState struct {
	Height     uint64
	Round      uint64
	Block      *core.Block
	BlockHash  string
	Proposer   string
	Prevotes   map[string]*core.Message
	Precommits map[string]*core.Message
	Step       Step
}

// CometBFT 是独立的 Tendermint/CometBFT 风格 BFT 共识实现。
//
// 这里实现的是协议核心流程：轮换 proposer、Proposal、Prevote、
// Precommit、2/3 多数提交。它不继承 PBFT，也不通过预设延迟系数制造结果。
type CometBFT struct {
	mu sync.RWMutex

	cfg      *core.NodeConfig
	network  core.Network
	txPool   core.TxPool
	executor core.Executor
	signer   *core.Signer

	height uint64
	round  uint64
	step   Step

	running bool
	stopCh  chan struct{}
	msgCh   chan *core.Message

	rounds    map[string]*RoundState
	committed map[uint64]*core.Block
	pending   map[string]*core.Tx
	latencies []float64

	totalTxCommitted    uint64
	firstSubmitUnixNano int64
	lastCommitUnixNano  int64
	startTime           time.Time
}

// NewCometBFT 创建 CometBFT 风格共识引擎。
func NewCometBFT() *CometBFT {
	return &CometBFT{
		stopCh:    make(chan struct{}),
		msgCh:     make(chan *core.Message, 4096),
		rounds:    make(map[string]*RoundState),
		committed: make(map[uint64]*core.Block),
		pending:   make(map[string]*core.Tx),
		latencies: make([]float64, 0),
	}
}

// Init 初始化节点并注册网络处理器。
func (c *CometBFT) Init(cfg *core.NodeConfig, network core.Network, txPool core.TxPool, exec core.Executor) error {
	c.cfg = cfg
	c.network = network
	c.txPool = txPool
	c.executor = exec
	c.signer = &core.Signer{PrivKey: cfg.PrivateKey, PubKeys: cfg.PublicKeys}
	network.RegisterHandler(cfg.NodeID, func(msg *core.Message) {
		select {
		case c.msgCh <- msg:
		default:
		}
	})
	return nil
}

// Start 启动消息循环和提案循环。
func (c *CometBFT) Start() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = true
	c.startTime = time.Now()
	c.mu.Unlock()

	go c.mainLoop()
	go c.proposalLoop()
	return nil
}

// Stop 停止共识引擎。
func (c *CometBFT) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return nil
	}
	c.running = false
	close(c.stopCh)
	return nil
}

// SubmitTx 接收客户端交易；非 proposer 节点会转发给当前 proposer。
func (c *CometBFT) SubmitTx(tx *core.Tx) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return fmt.Errorf("engine not running")
	}
	c.pending[tx.ID] = tx
	proposer := c.proposer(c.height+1, c.round)
	isProposer := proposer == c.cfg.NodeID
	c.mu.Unlock()

	if isProposer {
		return nil
	}
	return c.network.Send(&core.Message{
		Type:      core.MsgClientRequest,
		From:      c.cfg.NodeID,
		To:        proposer,
		Tx:        tx,
		Timestamp: time.Now(),
	})
}

// GetStatus 返回当前状态和指标。
func (c *CometBFT) GetStatus() core.EngineStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	elapsed := time.Since(c.startTime).Seconds()
	tps := 0.0
	if elapsed > 0 {
		tps = float64(atomic.LoadUint64(&c.totalTxCommitted)) / elapsed
	}
	p50, p95, p99 := common.ComputeLatencyStats(c.latencies)
	leader := c.proposer(c.height+1, c.round)
	return core.EngineStatus{
		NodeID:              c.cfg.NodeID,
		Height:              c.height,
		View:                c.round,
		IsLeader:            c.cfg.NodeID == leader,
		LeaderID:            leader,
		PendingTxCount:      len(c.pending),
		CommittedBlocks:     c.height,
		CommittedTxs:        atomic.LoadUint64(&c.totalTxCommitted),
		FirstSubmitUnixNano: atomic.LoadInt64(&c.firstSubmitUnixNano),
		LastCommitUnixNano:  atomic.LoadInt64(&c.lastCommitUnixNano),
		TPS:                 tps,
		P50LatencyMs:        p50,
		P95LatencyMs:        p95,
		P99LatencyMs:        p99,
	}
}

func (c *CometBFT) mainLoop() {
	for {
		select {
		case <-c.stopCh:
			return
		case msg := <-c.msgCh:
			c.handleMessage(msg)
		}
	}
}

func (c *CometBFT) proposalLoop() {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.tryPropose()
		}
	}
}

func (c *CometBFT) handleMessage(msg *core.Message) {
	if c.cfg.IsByzantine || msg == nil {
		return
	}
	switch msg.Type {
	case core.MsgClientRequest:
		c.handleClientRequest(msg)
	case core.MsgPrePrepare:
		c.handleProposal(msg)
	case core.MsgPrepare:
		c.handlePrevote(msg)
	case core.MsgCommit:
		c.handlePrecommit(msg)
	}
}

func (c *CometBFT) handleClientRequest(msg *core.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if msg.Tx != nil {
		c.pending[msg.Tx.ID] = msg.Tx
	}
}

func (c *CometBFT) tryPropose() {
	c.mu.Lock()
	defer c.mu.Unlock()

	height := c.height + 1
	if c.cfg.NodeID != c.proposer(height, c.round) || c.step != StepPropose {
		return
	}
	state := c.getRoundStateLocked(height, c.round)
	if state.Block != nil {
		return
	}

	txs := c.txPool.GetTxs(200)
	if len(txs) == 0 {
		for _, tx := range c.pending {
			txs = append(txs, tx)
			if len(txs) >= 200 {
				break
			}
		}
	}
	if len(txs) == 0 {
		return
	}

	prevHash := ""
	if prev, ok := c.committed[c.height]; ok {
		prevHash = prev.Hash
	}
	block := &core.Block{
		Height:    height,
		PrevHash:  prevHash,
		Txs:       txs,
		Proposer:  c.cfg.NodeID,
		Timestamp: time.Now(),
	}
	block.Hash = block.ComputeHash()
	state.Block = block
	state.BlockHash = block.Hash
	state.Proposer = c.cfg.NodeID
	state.Step = StepPrevote
	c.step = StepPrevote

	blockBytes, _ := json.Marshal(block)
	proposal := &core.Message{
		Type:      core.MsgPrePrepare,
		From:      c.cfg.NodeID,
		View:      c.round,
		Height:    height,
		Block:     block,
		BlockHash: block.Hash,
		Sigs:      map[string][]byte{c.cfg.NodeID: c.signer.Sign(blockBytes)},
		Timestamp: time.Now(),
	}
	c.broadcast(proposal)
	c.recordPrevoteLocked(state, c.cfg.NodeID, block.Hash)
	c.broadcastVote(core.MsgPrepare, height, c.round, block.Hash)
}

func (c *CometBFT) handleProposal(msg *core.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if msg.Block == nil || msg.Height != c.height+1 {
		return
	}
	if msg.From != c.proposer(msg.Height, msg.View) || msg.Block.Proposer != msg.From {
		return
	}
	if msg.Block.ComputeHash() != msg.BlockHash {
		return
	}
	blockBytes, _ := json.Marshal(msg.Block)
	if sig, ok := msg.Sigs[msg.From]; ok && !c.signer.Verify(msg.From, blockBytes, sig) {
		return
	}

	state := c.getRoundStateLocked(msg.Height, msg.View)
	if state.Block == nil {
		state.Block = msg.Block
		state.BlockHash = msg.BlockHash
		state.Proposer = msg.From
	}
	state.Step = StepPrevote
	c.step = StepPrevote
	c.recordPrevoteLocked(state, c.cfg.NodeID, msg.BlockHash)
	c.broadcastVote(core.MsgPrepare, msg.Height, msg.View, msg.BlockHash)
	c.maybePrecommitLocked(state)
}

func (c *CometBFT) handlePrevote(msg *core.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.getRoundStateLocked(msg.Height, msg.View)
	if state.BlockHash != "" && msg.BlockHash != state.BlockHash {
		return
	}
	if sig, ok := msg.Sigs[msg.From]; ok && !c.signer.Verify(msg.From, []byte(msg.BlockHash), sig) {
		return
	}
	state.Prevotes[msg.From] = msg
	c.maybePrecommitLocked(state)
}

func (c *CometBFT) maybePrecommitLocked(state *RoundState) {
	if state == nil || state.Block == nil || state.Step >= StepPrecommit {
		return
	}
	if len(state.Prevotes) < quorum(len(c.cfg.AllNodes)) {
		return
	}
	state.Step = StepPrecommit
	c.step = StepPrecommit
	c.recordPrecommitLocked(state, c.cfg.NodeID, state.BlockHash)
	c.broadcastVote(core.MsgCommit, state.Height, state.Round, state.BlockHash)
}

func (c *CometBFT) handlePrecommit(msg *core.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.getRoundStateLocked(msg.Height, msg.View)
	if state.BlockHash != "" && msg.BlockHash != state.BlockHash {
		return
	}
	if sig, ok := msg.Sigs[msg.From]; ok && !c.signer.Verify(msg.From, []byte(msg.BlockHash), sig) {
		return
	}
	state.Precommits[msg.From] = msg
	if len(state.Precommits) >= quorum(len(c.cfg.AllNodes)) && state.Step < StepCommit {
		state.Step = StepCommit
		c.step = StepCommit
		c.commitBlockLocked(state.Block)
	}
}

func (c *CometBFT) commitBlockLocked(block *core.Block) {
	if block == nil {
		return
	}
	c.committed[block.Height] = block
	c.height = block.Height
	c.round = 0
	c.step = StepPropose
	_ = c.executor.ExecuteBlock(block)

	now := time.Now()
	txIDs := make([]string, 0, len(block.Txs))
	for _, tx := range block.Txs {
		txIDs = append(txIDs, tx.ID)
		delete(c.pending, tx.ID)
		if !tx.SubmitTime.IsZero() {
			c.latencies = append(c.latencies, float64(now.Sub(tx.SubmitTime).Microseconds())/1000.0)
		}
	}
	core.UpdateCommitWindow(&c.firstSubmitUnixNano, &c.lastCommitUnixNano, block, now)
	c.txPool.RemoveTxs(txIDs)
	atomic.AddUint64(&c.totalTxCommitted, uint64(len(block.Txs)))
}

func (c *CometBFT) getRoundStateLocked(height uint64, round uint64) *RoundState {
	key := roundKey(height, round)
	if state, ok := c.rounds[key]; ok {
		return state
	}
	state := &RoundState{
		Height:     height,
		Round:      round,
		Proposer:   c.proposer(height, round),
		Prevotes:   make(map[string]*core.Message),
		Precommits: make(map[string]*core.Message),
		Step:       StepPropose,
	}
	c.rounds[key] = state
	return state
}

func (c *CometBFT) recordPrevoteLocked(state *RoundState, nodeID string, hash string) {
	state.Prevotes[nodeID] = &core.Message{
		Type:      core.MsgPrepare,
		From:      nodeID,
		View:      state.Round,
		Height:    state.Height,
		BlockHash: hash,
		Timestamp: time.Now(),
	}
}

func (c *CometBFT) recordPrecommitLocked(state *RoundState, nodeID string, hash string) {
	state.Precommits[nodeID] = &core.Message{
		Type:      core.MsgCommit,
		From:      nodeID,
		View:      state.Round,
		Height:    state.Height,
		BlockHash: hash,
		Timestamp: time.Now(),
	}
}

func (c *CometBFT) broadcastVote(msgType core.MessageType, height uint64, round uint64, hash string) {
	vote := &core.Message{
		Type:      msgType,
		From:      c.cfg.NodeID,
		View:      round,
		Height:    height,
		BlockHash: hash,
		Sigs:      map[string][]byte{c.cfg.NodeID: c.signer.Sign([]byte(hash))},
		Timestamp: time.Now(),
	}
	c.broadcast(vote)
}

func (c *CometBFT) broadcast(msg *core.Message) {
	for _, nodeID := range c.cfg.AllNodes {
		if nodeID == c.cfg.NodeID {
			continue
		}
		copyMsg := *msg
		copyMsg.To = nodeID
		_ = c.network.Send(&copyMsg)
	}
}

func (c *CometBFT) proposer(height uint64, round uint64) string {
	if len(c.cfg.AllNodes) == 0 {
		return ""
	}
	idx := int((height + round - 1) % uint64(len(c.cfg.AllNodes)))
	return c.cfg.AllNodes[idx]
}

func quorum(n int) int {
	if n <= 0 {
		return 0
	}
	f := (n - 1) / 3
	return 2*f + 1
}

func roundKey(height uint64, round uint64) string {
	return fmt.Sprintf("%d:%d", height, round)
}

// ComputeLatencyStats 计算延迟统计。
func ComputeLatencyStats(latencies []float64) (p50, p95, p99 float64) {
	return common.ComputeLatencyStats(latencies)
}
