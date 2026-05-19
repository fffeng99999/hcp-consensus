package pbft

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fffeng99999/hcp-consensus/engine/common"
	"github.com/fffeng99999/hcp-consensus/engine/core"
)

// State PBFT节点状态
type State int

const (
	StateIdle State = iota
	StatePrePrepared
	StatePrepared
	StateCommitted
)

// PBFT 实用拜占庭容错共识引擎
type PBFT struct {
	mu sync.RWMutex

	cfg      *core.NodeConfig
	network  core.Network
	txPool   core.TxPool
	executor core.Executor

	// 共识状态
	view          uint64
	height        uint64
	state         State
	leaderID      string
	isLeader      bool

	// 日志
	log           map[uint64]*LogEntry
	committed     map[uint64]*core.Block

	// 投票收集
	prepareVotes  map[string]map[string]bool // height:blockHash -> nodeID -> bool
	commitVotes   map[string]map[string]bool
	pendingCommits map[string][]*core.Message // 缓冲提前到达的Commit消息

	// 客户端请求追踪
	pendingReqs   map[string]*core.Tx
	replyCache    map[string]*ClientReply
	latencyLog    []float64 // 交易确认延迟记录

	// 控制
	running       bool
	stopCh        chan struct{}
	msgCh         chan *core.Message
	timer         *time.Timer
	viewTimeout   time.Duration

	// 签名
	signer        *core.Signer

	// 指标
	totalTxCommitted uint64
	startTime        time.Time

	// === 可扩展钩子 ===
	// BroadcastTargets 自定义广播目标列表，nil表示广播给所有节点
	BroadcastTargets func() []string
	// ValidatorSelector 验证者选择器，返回允许参与共识的节点ID
	ValidatorSelector func() []string
	// OnPrePrepare 收到PrePrepare时的钩子
	OnPrePrepare func(msg *core.Message) bool
	// OnPrepare 收到Prepare时的钩子
	OnPrepare func(msg *core.Message) bool
	// OnCommit 提交区块后的钩子
	OnCommit func(block *core.Block)
	// ExtraLatencyMs 额外注入的延迟（模拟签名验证等）
	ExtraLatencyMs float64
}

type LogEntry struct {
	Height     uint64
	View       uint64
	Block      *core.Block
	PrePrepare *core.Message
	Prepares   map[string]*core.Message
	Commits    map[string]*core.Message
	State      State
}

type ClientReply struct {
	TxID      string
	NodeID    string
	Height    uint64
	Result    string
	Timestamp time.Time
}

func NewPBFT() *PBFT {
	return &PBFT{
		log:           make(map[uint64]*LogEntry),
		committed:     make(map[uint64]*core.Block),
		prepareVotes:  make(map[string]map[string]bool),
		commitVotes:   make(map[string]map[string]bool),
		pendingCommits: make(map[string][]*core.Message),
		pendingReqs:   make(map[string]*core.Tx),
		replyCache:    make(map[string]*ClientReply),
		latencyLog:    make([]float64, 0),
		stopCh:        make(chan struct{}),
		msgCh:         make(chan *core.Message, 1024),
		viewTimeout:   5 * time.Second,
		ExtraLatencyMs: 0,
	}
}

func (p *PBFT) Init(cfg *core.NodeConfig, network core.Network, txPool core.TxPool, exec core.Executor) error {
	p.cfg = cfg
	p.network = network
	p.txPool = txPool
	p.executor = exec
	p.signer = &core.Signer{PrivKey: cfg.PrivateKey, PubKeys: cfg.PublicKeys}
	p.leaderID = cfg.AllNodes[0]
	p.isLeader = cfg.NodeID == p.leaderID
	network.RegisterHandler(cfg.NodeID, func(msg *core.Message) {
		select {
		case p.msgCh <- msg:
		default:
		}
	})
	return nil
}

func (p *PBFT) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}
	p.running = true
	p.startTime = time.Now()
	go p.mainLoop()
	if p.isLeader {
		go p.proposalLoop()
	}
	return nil
}

func (p *PBFT) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return nil
	}
	p.running = false
	close(p.stopCh)
	return nil
}

func (p *PBFT) SubmitTx(tx *core.Tx) error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return fmt.Errorf("engine not running")
	}
	p.pendingReqs[tx.ID] = tx
	p.mu.Unlock()

	msg := &core.Message{
		Type:      core.MsgClientRequest,
		From:      p.cfg.NodeID,
		To:        p.leaderID,
		Tx:        tx,
		Timestamp: time.Now(),
	}
	return p.network.Send(msg)
}

func (p *PBFT) GetStatus() core.EngineStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	elapsed := time.Since(p.startTime).Seconds()
	tps := 0.0
	if elapsed > 0 {
		tps = float64(atomic.LoadUint64(&p.totalTxCommitted)) / elapsed
	}
	p50, p95, p99 := common.ComputeLatencyStats(p.latencyLog)
	return core.EngineStatus{
		NodeID:          p.cfg.NodeID,
		Height:          p.height,
		View:            p.view,
		IsLeader:        p.isLeader,
		LeaderID:        p.leaderID,
		PendingTxCount:  len(p.pendingReqs),
		CommittedBlocks: p.height,
		TPS:             tps,
		AvgLatencyMs:    0,
		P50LatencyMs:    p50,
		P95LatencyMs:    p95,
		P99LatencyMs:    p99,
	}
}

func (p *PBFT) quorumSize() int {
	// 如果使用了ValidatorSelector，基于选中的集合计算
	if p.ValidatorSelector != nil {
		sel := p.ValidatorSelector()
		if len(sel) > 0 {
			f := (len(sel) - 1) / 3
			return 2*f + 1
		}
	}
	n := len(p.cfg.AllNodes)
	f := (n - 1) / 3
	return 2*f + 1
}

func (p *PBFT) fSize() int {
	if p.ValidatorSelector != nil {
		sel := p.ValidatorSelector()
		if len(sel) > 0 {
			return (len(sel) - 1) / 3
		}
	}
	return (len(p.cfg.AllNodes) - 1) / 3
}

func (p *PBFT) mainLoop() {
	for {
		select {
		case <-p.stopCh:
			return
		case msg := <-p.msgCh:
			p.handleMessage(msg)
		}
	}
}

func (p *PBFT) handleMessage(msg *core.Message) {
	if p.cfg.IsByzantine {
		return
	}

	switch msg.Type {
	case core.MsgClientRequest:
		p.handleClientRequest(msg)
	case core.MsgPrePrepare:
		p.handlePrePrepare(msg)
	case core.MsgPrepare:
		p.handlePrepare(msg)
	case core.MsgCommit:
		p.handleCommit(msg)
	case core.MsgViewChange:
		p.handleViewChange(msg)
	case core.MsgNewView:
		p.handleNewView(msg)
	}
}

func (p *PBFT) handleClientRequest(msg *core.Message) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.isLeader {
		return
	}
	if msg.Tx != nil {
		p.pendingReqs[msg.Tx.ID] = msg.Tx
	}
}

func (p *PBFT) proposalLoop() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.proposeBlock()
		}
	}
}

func (p *PBFT) proposeBlock() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.isLeader {
		return
	}
	if p.state != StateIdle {
		return
	}

	txs := p.txPool.GetTxs(200)
	if len(txs) == 0 {
		for _, tx := range p.pendingReqs {
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
	if p.height > 0 {
		if b, ok := p.committed[p.height]; ok {
			prevHash = b.Hash
		}
	}
	block := &core.Block{
		Height:    p.height + 1,
		PrevHash:  prevHash,
		Txs:       txs,
		Proposer:  p.cfg.NodeID,
		Timestamp: time.Now(),
	}
	block.Hash = block.ComputeHash()

	blockData, _ := json.Marshal(block)
	sig := p.signer.Sign(blockData)

	msg := &core.Message{
		Type:      core.MsgPrePrepare,
		From:      p.cfg.NodeID,
		View:      p.view,
		Height:    block.Height,
		Block:     block,
		BlockHash: block.Hash,
		Sigs:      map[string][]byte{p.cfg.NodeID: sig},
		Timestamp: time.Now(),
	}

	p.log[block.Height] = &LogEntry{
		Height:     block.Height,
		View:       p.view,
		Block:      block,
		PrePrepare: msg,
		Prepares:   make(map[string]*core.Message),
		Commits:    make(map[string]*core.Message),
		State:      StatePrePrepared,
	}
	p.state = StatePrePrepared

	// 领导者记录自己的Prepare投票
	prepareSig := p.signer.Sign([]byte(block.Hash))
	prepare := &core.Message{
		Type:      core.MsgPrepare,
		From:      p.cfg.NodeID,
		View:      p.view,
		Height:    block.Height,
		BlockHash: block.Hash,
		Sigs:      map[string][]byte{p.cfg.NodeID: prepareSig},
		Timestamp: time.Now(),
	}
	p.log[block.Height].Prepares[p.cfg.NodeID] = prepare
	key := fmt.Sprintf("%d:%s", block.Height, block.Hash)
	if p.prepareVotes[key] == nil {
		p.prepareVotes[key] = make(map[string]bool)
	}
	p.prepareVotes[key][p.cfg.NodeID] = true

	// 广播或定向发送
	if p.BroadcastTargets != nil {
		targets := p.BroadcastTargets()
		for _, nodeID := range targets {
			if nodeID == p.cfg.NodeID {
				continue
			}
			m := *msg
			m.To = nodeID
			p.network.Send(&m)
		}
	} else {
		p.network.Broadcast(msg)
	}
}

func (p *PBFT) handlePrePrepare(msg *core.Message) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if msg.From != p.leaderID {
		return
	}
	if msg.View != p.view {
		return
	}
	if msg.Height != p.height+1 {
		return
	}

	// 验证签名
	blockData, _ := json.Marshal(msg.Block)
	if sig, ok := msg.Sigs[msg.From]; ok {
		if !p.signer.Verify(msg.From, blockData, sig) {
			return
		}
	}

	// 钩子
	if p.OnPrePrepare != nil && !p.OnPrePrepare(msg) {
		return
	}

	p.log[msg.Height] = &LogEntry{
		Height:     msg.Height,
		View:       msg.View,
		Block:      msg.Block,
		PrePrepare: msg,
		Prepares:   make(map[string]*core.Message),
		Commits:    make(map[string]*core.Message),
		State:      StatePrePrepared,
	}
	p.state = StatePrePrepared

	// 模拟额外延迟（签名验证）
	if p.ExtraLatencyMs > 0 {
		time.Sleep(time.Duration(p.ExtraLatencyMs) * time.Millisecond)
	}

	prepareSig := p.signer.Sign([]byte(msg.BlockHash))
	prepare := &core.Message{
		Type:      core.MsgPrepare,
		From:      p.cfg.NodeID,
		View:      p.view,
		Height:    msg.Height,
		BlockHash: msg.BlockHash,
		Sigs:      map[string][]byte{p.cfg.NodeID: prepareSig},
		Timestamp: time.Now(),
	}
	p.log[msg.Height].Prepares[p.cfg.NodeID] = prepare
	// 记录自己的Prepare投票
	key := fmt.Sprintf("%d:%s", msg.Height, msg.BlockHash)
	if p.prepareVotes[key] == nil {
		p.prepareVotes[key] = make(map[string]bool)
	}
	p.prepareVotes[key][p.cfg.NodeID] = true

	// 检查是否已达到Prepared状态（包括自己的投票）
	qs := p.quorumSize()
	if len(p.prepareVotes[key]) >= qs && p.log[msg.Height].State < StatePrepared {
		p.log[msg.Height].State = StatePrepared
		p.state = StatePrepared
		if p.ExtraLatencyMs > 0 {
			time.Sleep(time.Duration(p.ExtraLatencyMs) * time.Millisecond)
		}
		commitSig := p.signer.Sign([]byte(msg.BlockHash))
		commit := &core.Message{
			Type:      core.MsgCommit,
			From:      p.cfg.NodeID,
			View:      p.view,
			Height:    msg.Height,
			BlockHash: msg.BlockHash,
			Sigs:      map[string][]byte{p.cfg.NodeID: commitSig},
			Timestamp: time.Now(),
		}
		p.log[msg.Height].Commits[p.cfg.NodeID] = commit
		commitKey := fmt.Sprintf("%d:%s", msg.Height, msg.BlockHash)
		if p.commitVotes[commitKey] == nil {
			p.commitVotes[commitKey] = make(map[string]bool)
		}
		p.commitVotes[commitKey][p.cfg.NodeID] = true
		if p.BroadcastTargets != nil {
			targets := p.BroadcastTargets()
			for _, nodeID := range targets {
				if nodeID == p.cfg.NodeID {
					continue
				}
				m := *commit
				m.To = nodeID
				p.network.Send(&m)
			}
		} else {
			p.network.Broadcast(commit)
		}
		p.processPendingCommits(msg.BlockHash, msg.Height)
	}

	if p.BroadcastTargets != nil {
		targets := p.BroadcastTargets()
		for _, nodeID := range targets {
			if nodeID == p.cfg.NodeID {
				continue
			}
			m := *prepare
			m.To = nodeID
			p.network.Send(&m)
		}
	} else {
		p.network.Broadcast(prepare)
	}
}

func (p *PBFT) handlePrepare(msg *core.Message) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if msg.View != p.view {
		return
	}
	entry, ok := p.log[msg.Height]
	if !ok {
		return
	}
	if entry.State < StatePrePrepared {
		return
	}

	if sig, ok := msg.Sigs[msg.From]; ok {
		if !p.signer.Verify(msg.From, []byte(msg.BlockHash), sig) {
			return
		}
	}

	if p.OnPrepare != nil && !p.OnPrepare(msg) {
		return
	}

	entry.Prepares[msg.From] = msg
	key := fmt.Sprintf("%d:%s", msg.Height, msg.BlockHash)
	if p.prepareVotes[key] == nil {
		p.prepareVotes[key] = make(map[string]bool)
	}
	p.prepareVotes[key][msg.From] = true

	qs := p.quorumSize()
	if len(p.prepareVotes[key]) >= qs && entry.State < StatePrepared {
		entry.State = StatePrepared
		p.state = StatePrepared

		if p.ExtraLatencyMs > 0 {
			time.Sleep(time.Duration(p.ExtraLatencyMs) * time.Millisecond)
		}

		commitSig := p.signer.Sign([]byte(msg.BlockHash))
		commit := &core.Message{
			Type:      core.MsgCommit,
			From:      p.cfg.NodeID,
			View:      p.view,
			Height:    msg.Height,
			BlockHash: msg.BlockHash,
			Sigs:      map[string][]byte{p.cfg.NodeID: commitSig},
			Timestamp: time.Now(),
		}
		entry.Commits[p.cfg.NodeID] = commit
		// 记录自己的Commit投票
		commitKey := fmt.Sprintf("%d:%s", msg.Height, msg.BlockHash)
		if p.commitVotes[commitKey] == nil {
			p.commitVotes[commitKey] = make(map[string]bool)
		}
		p.commitVotes[commitKey][p.cfg.NodeID] = true

		if p.BroadcastTargets != nil {
			targets := p.BroadcastTargets()
			for _, nodeID := range targets {
				if nodeID == p.cfg.NodeID {
					continue
				}
				m := *commit
				m.To = nodeID
				p.network.Send(&m)
			}
		} else {
			p.network.Broadcast(commit)
		}
		p.processPendingCommits(msg.BlockHash, msg.Height)
	}
}

func (p *PBFT) handleCommit(msg *core.Message) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if msg.View != p.view {
		return
	}
	entry, ok := p.log[msg.Height]
	if !ok {
		return
	}
	if entry.State < StatePrepared {
		// 缓冲Commit消息，等达到Prepared后再处理
		if p.pendingCommits == nil {
			p.pendingCommits = make(map[string][]*core.Message)
		}
		commitKey := fmt.Sprintf("%d:%s", msg.Height, msg.BlockHash)
		p.pendingCommits[commitKey] = append(p.pendingCommits[commitKey], msg)
		return
	}

	if sig, ok := msg.Sigs[msg.From]; ok {
		if !p.signer.Verify(msg.From, []byte(msg.BlockHash), sig) {
			return
		}
	}

	entry.Commits[msg.From] = msg
	key := fmt.Sprintf("%d:%s", msg.Height, msg.BlockHash)
	if p.commitVotes[key] == nil {
		p.commitVotes[key] = make(map[string]bool)
	}
	p.commitVotes[key][msg.From] = true

	qs := p.quorumSize()
	if len(p.commitVotes[key]) >= qs && entry.State < StateCommitted {
		entry.State = StateCommitted
		p.state = StateCommitted
		p.commitBlock(entry.Block)
	}
}

// processPendingCommits 处理缓冲的Commit消息
func (p *PBFT) processPendingCommits(blockHash string, height uint64) {
	key := fmt.Sprintf("%d:%s", height, blockHash)
	commits, ok := p.pendingCommits[key]
	if !ok || len(commits) == 0 {
		return
	}
	delete(p.pendingCommits, key)
	for _, msg := range commits {
		if sig, ok := msg.Sigs[msg.From]; ok {
			if !p.signer.Verify(msg.From, []byte(msg.BlockHash), sig) {
				continue
			}
		}
		entry, ok := p.log[msg.Height]
		if !ok || entry.State < StatePrepared {
			continue
		}
		entry.Commits[msg.From] = msg
		if p.commitVotes[key] == nil {
			p.commitVotes[key] = make(map[string]bool)
		}
		p.commitVotes[key][msg.From] = true
	}
	qs := p.quorumSize()
	entry, ok := p.log[height]
	if ok && len(p.commitVotes[key]) >= qs && entry.State < StateCommitted {
		entry.State = StateCommitted
		p.state = StateCommitted
		p.commitBlock(entry.Block)
	}
}

func (p *PBFT) commitBlock(block *core.Block) {
	p.committed[block.Height] = block
	p.height = block.Height
	p.executor.ExecuteBlock(block)

	txIDs := make([]string, 0, len(block.Txs))
	now := time.Now()
	for _, tx := range block.Txs {
		txIDs = append(txIDs, tx.ID)
		delete(p.pendingReqs, tx.ID)
		// 使用SubmitTime计算端到端延迟（微秒精度）
		if !tx.SubmitTime.IsZero() {
			latencyUs := now.Sub(tx.SubmitTime).Microseconds()
			if latencyUs > 0 {
				p.latencyLog = append(p.latencyLog, float64(latencyUs)/1000.0)
			}
		}
	}
	p.txPool.RemoveTxs(txIDs)
	atomic.AddUint64(&p.totalTxCommitted, uint64(len(block.Txs)))

	for _, tx := range block.Txs {
		reply := &core.Message{
			Type:   core.MsgClientReply,
			From:   p.cfg.NodeID,
			To:     tx.From,
			Tx:     tx,
			Height: block.Height,
		}
		p.network.Send(reply)
	}

	p.state = StateIdle

	if p.OnCommit != nil {
		p.OnCommit(block)
	}
}

func (p *PBFT) handleViewChange(msg *core.Message) {}
func (p *PBFT) handleNewView(msg *core.Message)    {}

// === 公开访问方法（供子类/外部使用）===
func (p *PBFT) GetNodeID() string        { return p.cfg.NodeID }
func (p *PBFT) GetNetwork() core.Network { return p.network }
func (p *PBFT) GetTxPool() core.TxPool   { return p.txPool }
func (p *PBFT) GetHeight() uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.height
}
func (p *PBFT) GetView() uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.view
}
func (p *PBFT) GetState() State {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}
func (p *PBFT) IsLeader() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.isLeader
}
func (p *PBFT) SetState(s State) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state = s
}
func (p *PBFT) GetAllNodes() []string {
	if p.cfg == nil {
		return nil
	}
	return p.cfg.AllNodes
}
func (p *PBFT) GetCommittedHash(height uint64) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if b, ok := p.committed[height]; ok {
		return b.Hash
	}
	return ""
}
func (p *PBFT) SignData(data []byte) []byte {
	return p.signer.Sign(data)
}
func (p *PBFT) RecordLog(height uint64, entry *LogEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.log[height] = entry
}
func (p *PBFT) Lock()    { p.mu.Lock() }
func (p *PBFT) Unlock()  { p.mu.Unlock() }
func (p *PBFT) RLock()   { p.mu.RLock() }
func (p *PBFT) RUnlock() { p.mu.RUnlock() }
