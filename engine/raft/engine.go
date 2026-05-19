package raft

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fffeng99999/hcp-consensus/engine/common"
	"github.com/fffeng99999/hcp-consensus/engine/core"
)

// Role Raft节点角色
type Role int

const (
	RoleFollower Role = iota
	RoleCandidate
	RoleLeader
)

// Raft 崩溃容错共识引擎（简化但功能完整）
type Raft struct {
	mu sync.RWMutex

	cfg      *core.NodeConfig
	network  core.Network
	txPool   core.TxPool
	executor core.Executor

	role           Role
	currentTerm    uint64
	log            []*RaftLogEntry
	commitIndex    uint64
	lastApplied    uint64

	// Leader追踪follower匹配状态
	matchIndex     map[string]uint64

	pendingReqs    map[string]*core.Tx
	submitTimes    map[string]time.Time
	latencyLog     []float64

	running        bool
	stopCh         chan struct{}
	msgCh          chan *core.Message

	signer         *core.Signer

	totalTxCommitted uint64
	startTime        time.Time
	ExtraLatencyMs   float64
}

type RaftLogEntry struct {
	Term    uint64
	Index   uint64
	Block   *core.Block
}

func NewRaft() *Raft {
	return &Raft{
		log:            make([]*RaftLogEntry, 0),
		matchIndex:     make(map[string]uint64),
		pendingReqs:    make(map[string]*core.Tx),
		submitTimes:    make(map[string]time.Time),
		latencyLog:     make([]float64, 0),
		stopCh:         make(chan struct{}),
		msgCh:          make(chan *core.Message, 1024),
	}
}

func (r *Raft) Init(cfg *core.NodeConfig, network core.Network, txPool core.TxPool, exec core.Executor) error {
	r.cfg = cfg
	r.network = network
	r.txPool = txPool
	r.executor = exec
	r.signer = &core.Signer{PrivKey: cfg.PrivateKey, PubKeys: cfg.PublicKeys}
	r.role = RoleFollower
	r.log = append(r.log, &RaftLogEntry{Term: 0, Index: 0, Block: nil})
	network.RegisterHandler(cfg.NodeID, func(msg *core.Message) {
		select {
		case r.msgCh <- msg:
		default:
		}
	})
	return nil
}

func (r *Raft) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil
	}
	r.running = true
	r.startTime = time.Now()
	go r.mainLoop()
	// 简化：第一个节点始终作为leader（避免选举复杂性）
	if r.cfg.NodeID == r.cfg.AllNodes[0] {
		r.role = RoleLeader
		for _, node := range r.cfg.AllNodes {
			if node != r.cfg.NodeID {
				r.matchIndex[node] = 0
			}
		}
		go r.leaderLoop()
	}
	return nil
}

func (r *Raft) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return nil
	}
	r.running = false
	close(r.stopCh)
	return nil
}

func (r *Raft) SubmitTx(tx *core.Tx) error {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return fmt.Errorf("engine not running")
	}
	r.pendingReqs[tx.ID] = tx
	if !tx.SubmitTime.IsZero() {
		r.submitTimes[tx.ID] = tx.SubmitTime
	}
	isLeader := r.role == RoleLeader
	r.mu.Unlock()

	if isLeader {
		// 由leaderLoop定期处理
	} else {
		// 转发给leader
		msg := &core.Message{
			Type:      core.MsgClientRequest,
			From:      r.cfg.NodeID,
			To:        r.cfg.AllNodes[0], // 直接发给leader
			Tx:        tx,
			Timestamp: time.Now(),
		}
		r.network.Send(msg)
	}
	return nil
}

func (r *Raft) GetStatus() core.EngineStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	elapsed := time.Since(r.startTime).Seconds()
	tps := 0.0
	if elapsed > 0 {
		tps = float64(atomic.LoadUint64(&r.totalTxCommitted)) / elapsed
	}
	p50, p95, p99 := common.ComputeLatencyStats(r.latencyLog)
	leaderID := ""
	if r.role == RoleLeader {
		leaderID = r.cfg.NodeID
	} else {
		leaderID = r.cfg.AllNodes[0]
	}
	return core.EngineStatus{
		NodeID:         r.cfg.NodeID,
		Height:         r.lastApplied,
		IsLeader:       r.role == RoleLeader,
		LeaderID:       leaderID,
		PendingTxCount: len(r.pendingReqs),
		TPS:            tps,
		P50LatencyMs:   p50,
		P95LatencyMs:   p95,
		P99LatencyMs:   p99,
	}
}

func (r *Raft) quorumSize() int {
	return len(r.cfg.AllNodes)/2 + 1
}

func (r *Raft) mainLoop() {
	for {
		select {
		case <-r.stopCh:
			return
		case msg := <-r.msgCh:
			r.handleMessage(msg)
		}
	}
}

func (r *Raft) handleMessage(msg *core.Message) {
	if r.cfg.IsByzantine {
		return
	}
	switch msg.Type {
	case core.MsgClientRequest:
		r.handleClientRequest(msg)
	case core.MsgAppendEntries:
		r.handleAppendEntries(msg)
	case core.MsgAppendEntriesRsp:
		r.handleAppendEntriesRsp(msg)
	}
}

func (r *Raft) handleClientRequest(msg *core.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if msg.Tx != nil {
		r.pendingReqs[msg.Tx.ID] = msg.Tx
		if !msg.Tx.SubmitTime.IsZero() {
			r.submitTimes[msg.Tx.ID] = msg.Tx.SubmitTime
		}
	}
}

func (r *Raft) leaderLoop() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.mu.Lock()
			if r.role != RoleLeader {
				r.mu.Unlock()
				return
			}
			r.mu.Unlock()
			r.broadcastHeartbeat()
			r.appendBlock()
		}
	}
}

func (r *Raft) appendBlock() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.role != RoleLeader {
		return
	}
	txs := r.txPool.GetTxs(200)
	if len(txs) == 0 {
		for _, tx := range r.pendingReqs {
			txs = append(txs, tx)
			if len(txs) >= 200 {
				break
			}
		}
	}
	if len(txs) == 0 {
		return
	}
	// 从pendingReqs中移除
	for _, tx := range txs {
		delete(r.pendingReqs, tx.ID)
	}

	prevHash := ""
	if len(r.log) > 0 && r.log[len(r.log)-1].Block != nil {
		prevHash = r.log[len(r.log)-1].Block.Hash
	}
	block := &core.Block{
		Height:    r.lastApplied + 1,
		PrevHash:  prevHash,
		Txs:       txs,
		Proposer:  r.cfg.NodeID,
		Timestamp: time.Now(),
	}
	block.Hash = block.ComputeHash()

	entry := &RaftLogEntry{
		Term:  r.currentTerm,
		Index: uint64(len(r.log)),
		Block: block,
	}
	r.log = append(r.log, entry)

	// 广播AppendEntries
	r.broadcastAppendEntries(entry)
}

func (r *Raft) broadcastAppendEntries(entry *RaftLogEntry) {
	entriesData, _ := json.Marshal([]*RaftLogEntry{entry})
	for _, nodeID := range r.cfg.AllNodes {
		if nodeID == r.cfg.NodeID {
			continue
		}
		msg := &core.Message{
			Type:      core.MsgAppendEntries,
			From:      r.cfg.NodeID,
			To:        nodeID,
			Height:    r.commitIndex,
			Data:      entriesData,
			Timestamp: time.Now(),
		}
		r.network.Send(msg)
	}
}

func (r *Raft) handleAppendEntries(msg *core.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var entries []*RaftLogEntry
	json.Unmarshal(msg.Data, &entries)

	if len(entries) > 0 {
		for _, entry := range entries {
			if entry.Index >= uint64(len(r.log)) {
				r.log = append(r.log, entry)
			} else if entry.Index < uint64(len(r.log)) {
				r.log[entry.Index] = entry
			}
		}
	}

	// 跟随leader的commitIndex
	if msg.Height > r.commitIndex && msg.Height < uint64(len(r.log)) {
		r.commitIndex = msg.Height
		r.applyCommitted()
	}

	// 回复leader，告知当前log长度-1
	rsp := &core.Message{
		Type:      core.MsgAppendEntriesRsp,
		From:      r.cfg.NodeID,
		To:        msg.From,
		Height:    uint64(len(r.log) - 1),
		Timestamp: time.Now(),
	}
	r.network.Send(rsp)
}

func (r *Raft) handleAppendEntriesRsp(msg *core.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.role != RoleLeader {
		return
	}

	r.matchIndex[msg.From] = msg.Height

	// 检查是否可以提交（从最新log向下检查）
	for idx := uint64(len(r.log) - 1); idx > r.commitIndex; idx-- {
		matchCount := 1 // leader自己
		for _, mIdx := range r.matchIndex {
			if mIdx >= idx {
				matchCount++
			}
		}
		if matchCount >= r.quorumSize() {
			r.commitIndex = idx
			r.applyCommitted()
			break
		}
	}
}

func (r *Raft) applyCommitted() {
	for r.lastApplied < r.commitIndex {
		r.lastApplied++
		if r.lastApplied < uint64(len(r.log)) {
			entry := r.log[r.lastApplied]
			if entry.Block != nil {
				r.executor.ExecuteBlock(entry.Block)
				txIDs := make([]string, 0)
				now := time.Now()
				for _, tx := range entry.Block.Txs {
					txIDs = append(txIDs, tx.ID)
					if submitTime, ok := r.submitTimes[tx.ID]; ok {
						latencyUs := now.Sub(submitTime).Microseconds()
						if latencyUs > 0 {
							r.latencyLog = append(r.latencyLog, float64(latencyUs)/1000.0)
						}
						delete(r.submitTimes, tx.ID)
					}
				}
				r.txPool.RemoveTxs(txIDs)
				atomic.AddUint64(&r.totalTxCommitted, uint64(len(entry.Block.Txs)))
			}
		}
	}
}

func (r *Raft) broadcastHeartbeat() {
	for _, nodeID := range r.cfg.AllNodes {
		if nodeID == r.cfg.NodeID {
			continue
		}
		msg := &core.Message{
			Type:      core.MsgAppendEntries,
			From:      r.cfg.NodeID,
			To:        nodeID,
			Height:    r.commitIndex,
			Timestamp: time.Now(),
		}
		r.network.Send(msg)
	}
}
