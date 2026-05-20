package hotstuff

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fffeng99999/hcp-consensus/engine/common"
	"github.com/fffeng99999/hcp-consensus/engine/core"
)

// HotStuff 共识引擎（简化但功能完整的实现）
type HotStuff struct {
	mu sync.RWMutex

	cfg      *core.NodeConfig
	network  core.Network
	txPool   core.TxPool
	executor core.Executor

	// HotStuff状态
	height           uint64
	view             uint64
	leaderID         string
	isLeader         bool
	proposalInFlight bool
	proposalStarted  time.Time
	lockedQC         *core.QuorumCertificate
	prepareQC        *core.QuorumCertificate
	commitQC         *core.QuorumCertificate

	// 投票收集
	prepareVotes   map[uint64]map[string]*core.Message // height -> nodeID -> vote
	precommitVotes map[uint64]map[string]*core.Message
	commitVotes    map[uint64]map[string]*core.Message

	// 客户端
	pendingReqs map[string]*core.Tx
	latencyLog  []float64

	// 区块存储
	blocks map[uint64]*core.Block // height -> block

	// 控制
	running     bool
	stopCh      chan struct{}
	msgCh       chan *core.Message
	viewTimeout time.Duration
	signer      *core.Signer

	// 指标
	totalTxCommitted    uint64
	firstSubmitUnixNano int64
	lastCommitUnixNano  int64
	startTime           time.Time

	// 配置
	EnableThresholdSig bool
	PipelineDepth      int

	// 额外延迟（模拟签名验证等）
	ExtraLatencyMs float64
}

func NewHotStuff() *HotStuff {
	return &HotStuff{
		prepareVotes:       make(map[uint64]map[string]*core.Message),
		precommitVotes:     make(map[uint64]map[string]*core.Message),
		commitVotes:        make(map[uint64]map[string]*core.Message),
		pendingReqs:        make(map[string]*core.Tx),
		latencyLog:         make([]float64, 0),
		blocks:             make(map[uint64]*core.Block),
		stopCh:             make(chan struct{}),
		msgCh:              make(chan *core.Message, 1024),
		viewTimeout:        5 * time.Second,
		EnableThresholdSig: true,
		PipelineDepth:      3,
	}
}

func (h *HotStuff) Init(cfg *core.NodeConfig, network core.Network, txPool core.TxPool, exec core.Executor) error {
	h.cfg = cfg
	h.network = network
	h.txPool = txPool
	h.executor = exec
	h.signer = &core.Signer{PrivKey: cfg.PrivateKey, PubKeys: cfg.PublicKeys}
	h.leaderID = cfg.AllNodes[0]
	h.isLeader = cfg.NodeID == h.leaderID
	network.RegisterHandler(cfg.NodeID, func(msg *core.Message) {
		select {
		case h.msgCh <- msg:
		default:
		}
	})
	return nil
}

func (h *HotStuff) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return nil
	}
	h.running = true
	h.startTime = time.Now()
	go h.mainLoop()
	if h.isLeader {
		go h.proposalLoop()
	}
	return nil
}

func (h *HotStuff) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.running {
		return nil
	}
	h.running = false
	close(h.stopCh)
	return nil
}

func (h *HotStuff) SubmitTx(tx *core.Tx) error {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return fmt.Errorf("engine not running")
	}
	h.pendingReqs[tx.ID] = tx
	h.mu.Unlock()
	msg := &core.Message{
		Type:      core.MsgClientRequest,
		From:      h.cfg.NodeID,
		To:        h.leaderID,
		Tx:        tx,
		Timestamp: time.Now(),
	}
	return h.network.Send(msg)
}

func (h *HotStuff) GetStatus() core.EngineStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	elapsed := time.Since(h.startTime).Seconds()
	tps := 0.0
	if elapsed > 0 {
		tps = float64(atomic.LoadUint64(&h.totalTxCommitted)) / elapsed
	}
	committedTxs := atomic.LoadUint64(&h.totalTxCommitted)
	firstSubmitUnixNano := atomic.LoadInt64(&h.firstSubmitUnixNano)
	lastCommitUnixNano := atomic.LoadInt64(&h.lastCommitUnixNano)
	p50, p95, p99 := common.ComputeLatencyStats(h.latencyLog)
	return core.EngineStatus{
		NodeID:              h.cfg.NodeID,
		Height:              h.height,
		View:                h.view,
		IsLeader:            h.isLeader,
		LeaderID:            h.leaderID,
		PendingTxCount:      len(h.pendingReqs),
		CommittedTxs:        committedTxs,
		FirstSubmitUnixNano: firstSubmitUnixNano,
		LastCommitUnixNano:  lastCommitUnixNano,
		TPS:                 tps,
		P50LatencyMs:        p50,
		P95LatencyMs:        p95,
		P99LatencyMs:        p99,
	}
}

func (h *HotStuff) quorumSize() int {
	n := len(h.cfg.AllNodes)
	f := (n - 1) / 3
	return 2*f + 1
}

func (h *HotStuff) mainLoop() {
	for {
		select {
		case <-h.stopCh:
			return
		case msg := <-h.msgCh:
			h.handleMessage(msg)
		}
	}
}

func (h *HotStuff) handleMessage(msg *core.Message) {
	if h.cfg.IsByzantine {
		return
	}
	switch msg.Type {
	case core.MsgClientRequest:
		h.handleClientRequest(msg)
	case core.MsgPrepareHS:
		h.handlePrepareVote(msg)
	case core.MsgPreCommitHS:
		h.handlePreCommitVote(msg)
	case core.MsgCommitHS:
		h.handleCommitVote(msg)
	case core.MsgNewViewHS:
		h.handleNewView(msg)
	case core.MsgDecideHS:
		h.handleDecide(msg)
	}
}

func (h *HotStuff) handleClientRequest(msg *core.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.isLeader {
		return
	}
	if msg.Tx != nil {
		h.pendingReqs[msg.Tx.ID] = msg.Tx
	}
}

func (h *HotStuff) proposalLoop() {
	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.proposeBlock()
		}
	}
}

func (h *HotStuff) proposeBlock() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.isLeader {
		return
	}
	if h.proposalInFlight {
		if time.Since(h.proposalStarted) < 100*time.Millisecond {
			return
		}
		h.proposalInFlight = false
	}

	txs := h.txPool.GetTxs(200)
	if len(txs) == 0 {
		for _, tx := range h.pendingReqs {
			txs = append(txs, tx)
			if len(txs) >= 200 {
				break
			}
		}
	}
	if len(txs) == 0 {
		return
	}
	h.proposalInFlight = true
	h.proposalStarted = time.Now()

	prevHash := ""
	if h.height > 0 {
		// 使用prepareQC作为前一区块的哈希引用
		if h.prepareQC != nil {
			prevHash = h.prepareQC.BlockHash
		}
	}
	block := &core.Block{
		Height:    h.height + 1,
		PrevHash:  prevHash,
		Txs:       txs,
		Proposer:  h.cfg.NodeID,
		Timestamp: time.Now(),
	}
	block.Hash = block.ComputeHash()

	blockData, _ := json.Marshal(block)
	sig := h.signer.Sign(blockData)

	// 携带prepareQC（ justify ）
	msg := &core.Message{
		Type:      core.MsgNewViewHS,
		From:      h.cfg.NodeID,
		Height:    block.Height,
		View:      h.view,
		Block:     block,
		BlockHash: block.Hash,
		QC:        h.prepareQC, // 上一轮的prepareQC
		Sigs:      map[string][]byte{h.cfg.NodeID: sig},
		Timestamp: time.Now(),
	}

	h.network.Broadcast(msg)

	// 记录区块
	h.blocks[block.Height] = block

	// Leader记录自己的投票，等待异步收集达到法定人数
	if h.prepareVotes[block.Height] == nil {
		h.prepareVotes[block.Height] = make(map[string]*core.Message)
	}
	// 记录leader自己的Prepare投票
	prepareSig := h.signer.Sign([]byte(block.Hash))
	leaderVote := &core.Message{
		Type:      core.MsgPrepareHS,
		From:      h.cfg.NodeID,
		Height:    block.Height,
		BlockHash: block.Hash,
		Sigs:      map[string][]byte{h.cfg.NodeID: prepareSig},
		Timestamp: time.Now(),
	}
	h.prepareVotes[block.Height][h.cfg.NodeID] = leaderVote
	// 广播NewView（不含自动提交）
}

func (h *HotStuff) handlePrepareVote(msg *core.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if entry, ok := h.logAtHeight(msg.Height); ok && entry.block != nil && msg.BlockHash != entry.block.Hash {
		return
	}
	if h.prepareVotes[msg.Height] == nil {
		h.prepareVotes[msg.Height] = make(map[string]*core.Message)
	}
	h.prepareVotes[msg.Height][msg.From] = msg

	// 达到法定人数（含leader）后生成QC
	if len(h.prepareVotes[msg.Height]) >= h.quorumSize() {
		if entry, ok := h.logAtHeight(msg.Height); ok && entry.block != nil {
			h.sendPreCommit(entry.block)
		}
	}
}

func (h *HotStuff) sendPreCommit(block *core.Block) {
	// 生成prepareQC
	qc := &core.QuorumCertificate{
		BlockHash: block.Hash,
		Height:    block.Height,
		View:      h.view,
		Sigs:      make(map[string][]byte),
	}
	// 收集签名
	for nodeID, vote := range h.prepareVotes[block.Height] {
		if sig, ok := vote.Sigs[nodeID]; ok {
			qc.Sigs[nodeID] = sig
		}
	}
	// 加上leader自己的
	blockData, _ := json.Marshal(block)
	qc.Sigs[h.cfg.NodeID] = h.signer.Sign(blockData)

	h.prepareQC = qc

	// 广播PreCommit
	precommit := &core.Message{
		Type:      core.MsgPreCommitHS,
		From:      h.cfg.NodeID,
		Height:    block.Height,
		BlockHash: block.Hash,
		QC:        qc,
		Timestamp: time.Now(),
	}
	h.network.Broadcast(precommit)

	// Leader自动进入commit阶段
	h.sendCommit(block)
}

func (h *HotStuff) handlePreCommitVote(msg *core.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if entry, ok := h.logAtHeight(msg.Height); ok && entry.block != nil && msg.BlockHash != entry.block.Hash {
		return
	}

	if h.precommitVotes[msg.Height] == nil {
		h.precommitVotes[msg.Height] = make(map[string]*core.Message)
	}
	h.precommitVotes[msg.Height][msg.From] = msg

	if msg.QC != nil {
		h.prepareQC = msg.QC
	}

	// 收到PreCommit后投票Commit
	h.voteCommit(msg.Height, msg.BlockHash)

	// 达到法定人数后发送Commit
	if len(h.precommitVotes[msg.Height]) >= h.quorumSize() {
		if entry, ok := h.logAtHeight(msg.Height); ok && entry.block != nil {
			h.sendCommit(entry.block)
		}
	}
}

func (h *HotStuff) voteCommit(height uint64, blockHash string) {
	sig := h.signer.Sign([]byte(blockHash))
	vote := &core.Message{
		Type:      core.MsgCommitHS,
		From:      h.cfg.NodeID,
		Height:    height,
		BlockHash: blockHash,
		Sigs:      map[string][]byte{h.cfg.NodeID: sig},
		Timestamp: time.Now(),
	}
	// 实际发送vote
	vote.To = h.leaderID
	h.network.Send(vote)
}

func (h *HotStuff) sendCommit(block *core.Block) {
	qc := &core.QuorumCertificate{
		BlockHash: block.Hash,
		Height:    block.Height,
		View:      h.view,
		Sigs:      make(map[string][]byte),
	}
	for nodeID, vote := range h.precommitVotes[block.Height] {
		if sig, ok := vote.Sigs[nodeID]; ok {
			qc.Sigs[nodeID] = sig
		}
	}
	qc.Sigs[h.cfg.NodeID] = h.signer.Sign([]byte(block.Hash))
	h.commitQC = qc

	commit := &core.Message{
		Type:      core.MsgDecideHS,
		From:      h.cfg.NodeID,
		Height:    block.Height,
		Block:     block,
		BlockHash: block.Hash,
		QC:        qc,
		Timestamp: time.Now(),
	}
	h.network.Broadcast(commit)
	h.commitBlock(block)
}

func (h *HotStuff) handleCommitVote(msg *core.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if entry, ok := h.logAtHeight(msg.Height); ok && entry.block != nil && msg.BlockHash != entry.block.Hash {
		return
	}
	if h.commitVotes[msg.Height] == nil {
		h.commitVotes[msg.Height] = make(map[string]*core.Message)
	}
	h.commitVotes[msg.Height][msg.From] = msg
}

func (h *HotStuff) handleDecide(msg *core.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if msg.From != h.leaderID {
		return
	}
	if msg.Height != h.height+1 {
		return
	}
	if msg.Block != nil {
		h.commitBlock(msg.Block)
	}
}

func (h *HotStuff) handleNewView(msg *core.Message) {
	// 收到leader的提案（NewView消息包含区块和justify QC）
	h.mu.Lock()
	defer h.mu.Unlock()

	if msg.From != h.leaderID {
		return
	}
	if msg.Height != h.height+1 {
		return
	}

	block := msg.Block
	if block == nil {
		return
	}

	// 验证justify QC
	if msg.QC != nil {
		// 简化：信任QC的有效性
		h.prepareQC = msg.QC
	}

	// 投票Prepare
	blockData, _ := json.Marshal(block)
	if sig, ok := msg.Sigs[msg.From]; ok {
		if !h.signer.Verify(msg.From, blockData, sig) {
			return
		}
	}

	sig := h.signer.Sign(blockData)
	vote := &core.Message{
		Type:      core.MsgPrepareHS,
		From:      h.cfg.NodeID,
		Height:    block.Height,
		BlockHash: block.Hash,
		Sigs:      map[string][]byte{h.cfg.NodeID: sig},
		Timestamp: time.Now(),
	}
	vote.To = h.leaderID
	h.network.Send(vote)

	// 记录区块
	h.blocks[block.Height] = block
	// 记录投票
	if h.prepareVotes[block.Height] == nil {
		h.prepareVotes[block.Height] = make(map[string]*core.Message)
	}
	h.prepareVotes[block.Height][h.cfg.NodeID] = vote
}

func (h *HotStuff) commitBlock(block *core.Block) {
	if block.Height <= h.height {
		return // 防止重复提交
	}
	h.committedHeight(block.Height)
	if h.ExtraLatencyMs > 0 {
		time.Sleep(time.Duration(h.ExtraLatencyMs * float64(time.Millisecond)))
	}
	h.executor.ExecuteBlock(block)

	txIDs := make([]string, 0, len(block.Txs))
	now := time.Now()
	for _, tx := range block.Txs {
		txIDs = append(txIDs, tx.ID)
		delete(h.pendingReqs, tx.ID)
		if !tx.SubmitTime.IsZero() {
			latencyUs := now.Sub(tx.SubmitTime).Microseconds()
			if latencyUs > 0 {
				h.latencyLog = append(h.latencyLog, float64(latencyUs)/1000.0)
			}
		}
	}
	core.UpdateCommitWindow(&h.firstSubmitUnixNano, &h.lastCommitUnixNano, block, now)
	h.txPool.RemoveTxs(txIDs)
	atomic.AddUint64(&h.totalTxCommitted, uint64(len(block.Txs)))
	for _, tx := range block.Txs {
		reply := &core.Message{
			Type:   core.MsgClientReply,
			From:   h.cfg.NodeID,
			To:     tx.From,
			Tx:     tx,
			Height: block.Height,
		}
		h.network.Send(reply)
	}
	h.height = block.Height
	h.proposalInFlight = false
}

func (h *HotStuff) committedHeight(height uint64) {}

func (h *HotStuff) logAtHeight(height uint64) (*hsEntry, bool) {
	if b, ok := h.blocks[height]; ok {
		return &hsEntry{height: height, block: b}, true
	}
	return nil, false
}

type hsEntry struct {
	height uint64
	block  *core.Block
	qc     *core.QuorumCertificate
}
