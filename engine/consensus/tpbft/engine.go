package tpbft

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fffeng99999/hcp-consensus/engine/common"
	"github.com/fffeng99999/hcp-consensus/engine/core"
)

// State 表示 tPBFT 单个高度内的共识阶段。
type State int

const (
	StateIdle State = iota
	StateProposed
	StatePrepared
	StateCommitted
)

// LogEntry 保存某一高度的提案、投票和状态。
type LogEntry struct {
	Height    uint64
	View      uint64
	Block     *core.Block
	Prepare   map[string]*core.Message
	Commit    map[string]*core.Message
	State     State
	Committee []string
}

// TPBFT 是独立的信任增强 PBFT 实现。
//
// 节点根据本地信任评分动态形成候选验证者集合；提案、Prepare、Commit 都由
// 当前高度的集合独立完成。
type TPBFT struct {
	mu sync.RWMutex

	cfg      *core.NodeConfig
	network  core.Network
	txPool   core.TxPool
	executor core.Executor
	signer   *core.Signer

	height   uint64
	view     uint64
	state    State
	leaderID string
	running  bool
	stopCh   chan struct{}
	msgCh    chan *core.Message

	log       map[uint64]*LogEntry
	committed map[uint64]*core.Block
	pending   map[string]*core.Tx
	latencies []float64

	trust *common.TrustScorer

	totalTxCommitted    uint64
	firstSubmitUnixNano int64
	lastCommitUnixNano  int64
	startTime           time.Time
}

// NewTPBFT 创建没有外部预设参数的 tPBFT 引擎。
func NewTPBFT() *TPBFT {
	return &TPBFT{
		stopCh:    make(chan struct{}),
		msgCh:     make(chan *core.Message, 4096),
		log:       make(map[uint64]*LogEntry),
		committed: make(map[uint64]*core.Block),
		pending:   make(map[string]*core.Tx),
		latencies: make([]float64, 0),
		trust:     common.NewTrustScorer(common.DefaultTrustWeights()),
	}
}

// Init 初始化节点状态并注册网络消息处理器。
func (t *TPBFT) Init(cfg *core.NodeConfig, network core.Network, txPool core.TxPool, exec core.Executor) error {
	t.cfg = cfg
	t.network = network
	t.txPool = txPool
	t.executor = exec
	t.signer = &core.Signer{PrivKey: cfg.PrivateKey, PubKeys: cfg.PublicKeys}
	t.leaderID = t.selectLeader(1)
	network.RegisterHandler(cfg.NodeID, func(msg *core.Message) {
		select {
		case t.msgCh <- msg:
		default:
		}
	})
	return nil
}

// Start 启动消息循环和提案循环。
func (t *TPBFT) Start() error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return nil
	}
	t.running = true
	t.startTime = time.Now()
	t.mu.Unlock()

	go t.mainLoop()
	go t.proposalLoop()
	return nil
}

// Stop 停止引擎。
func (t *TPBFT) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		return nil
	}
	t.running = false
	close(t.stopCh)
	return nil
}

// SubmitTx 接收客户端交易；非 leader 节点会转发给当前 leader。
func (t *TPBFT) SubmitTx(tx *core.Tx) error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return fmt.Errorf("engine not running")
	}
	t.pending[tx.ID] = tx
	leader := t.leaderID
	selfLeader := leader == t.cfg.NodeID
	t.mu.Unlock()

	if selfLeader {
		return nil
	}
	return t.network.Send(&core.Message{
		Type:      core.MsgClientRequest,
		From:      t.cfg.NodeID,
		To:        leader,
		Tx:        tx,
		Timestamp: time.Now(),
	})
}

// GetStatus 返回当前引擎状态和延迟指标。
func (t *TPBFT) GetStatus() core.EngineStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	elapsed := time.Since(t.startTime).Seconds()
	tps := 0.0
	if elapsed > 0 {
		tps = float64(atomic.LoadUint64(&t.totalTxCommitted)) / elapsed
	}
	p50, p95, p99 := common.ComputeLatencyStats(t.latencies)
	committed := atomic.LoadUint64(&t.totalTxCommitted)
	return core.EngineStatus{
		NodeID:              t.cfg.NodeID,
		Height:              t.height,
		View:                t.view,
		IsLeader:            t.cfg.NodeID == t.leaderID,
		LeaderID:            t.leaderID,
		PendingTxCount:      len(t.pending),
		CommittedBlocks:     t.height,
		CommittedTxs:        committed,
		FirstSubmitUnixNano: atomic.LoadInt64(&t.firstSubmitUnixNano),
		LastCommitUnixNano:  atomic.LoadInt64(&t.lastCommitUnixNano),
		TPS:                 tps,
		P50LatencyMs:        p50,
		P95LatencyMs:        p95,
		P99LatencyMs:        p99,
	}
}

func (t *TPBFT) mainLoop() {
	for {
		select {
		case <-t.stopCh:
			return
		case msg := <-t.msgCh:
			t.handleMessage(msg)
		}
	}
}

func (t *TPBFT) proposalLoop() {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.tryPropose()
		}
	}
}

func (t *TPBFT) handleMessage(msg *core.Message) {
	if t.cfg.IsByzantine || msg == nil {
		return
	}
	switch msg.Type {
	case core.MsgClientRequest:
		t.handleClientRequest(msg)
	case core.MsgPrePrepare:
		t.handleProposal(msg)
	case core.MsgPrepare:
		t.handlePrepare(msg)
	case core.MsgCommit:
		t.handleCommit(msg)
	}
}

func (t *TPBFT) handleClientRequest(msg *core.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if msg.Tx != nil {
		t.pending[msg.Tx.ID] = msg.Tx
	}
}

func (t *TPBFT) tryPropose() {
	t.mu.Lock()
	defer t.mu.Unlock()
	nextHeight := t.height + 1
	t.leaderID = t.selectLeader(nextHeight)
	if t.cfg.NodeID != t.leaderID || t.state != StateIdle {
		return
	}

	txs := t.txPool.GetTxs(200)
	if len(txs) == 0 {
		for _, tx := range t.pending {
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
	if prev, ok := t.committed[t.height]; ok {
		prevHash = prev.Hash
	}
	block := &core.Block{
		Height:    nextHeight,
		PrevHash:  prevHash,
		Txs:       txs,
		Proposer:  t.cfg.NodeID,
		Timestamp: time.Now(),
	}
	block.Hash = block.ComputeHash()

	committee := t.selectCommittee()
	entry := &LogEntry{
		Height:    block.Height,
		View:      t.view,
		Block:     block,
		Prepare:   make(map[string]*core.Message),
		Commit:    make(map[string]*core.Message),
		State:     StateProposed,
		Committee: committee,
	}
	t.log[block.Height] = entry
	t.state = StateProposed

	blockBytes, _ := json.Marshal(block)
	proposal := &core.Message{
		Type:      core.MsgPrePrepare,
		From:      t.cfg.NodeID,
		View:      t.view,
		Height:    block.Height,
		Block:     block,
		BlockHash: block.Hash,
		Sigs:      map[string][]byte{t.cfg.NodeID: t.signer.Sign(blockBytes)},
		Timestamp: time.Now(),
	}
	t.broadcastToCommittee(proposal, committee)
	t.recordPrepareLocked(block.Height, block.Hash, t.cfg.NodeID)
}

func (t *TPBFT) handleProposal(msg *core.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if msg.Block == nil || msg.Height != t.height+1 {
		return
	}
	expectedLeader := t.selectLeader(msg.Height)
	if msg.From != expectedLeader || msg.Block.Proposer != expectedLeader {
		return
	}
	if msg.Block.ComputeHash() != msg.BlockHash {
		return
	}
	blockBytes, _ := json.Marshal(msg.Block)
	if sig, ok := msg.Sigs[msg.From]; ok && !t.signer.Verify(msg.From, blockBytes, sig) {
		return
	}

	committee := t.selectCommittee()
	if !contains(committee, t.cfg.NodeID) {
		return
	}
	if _, ok := t.log[msg.Height]; !ok {
		t.log[msg.Height] = &LogEntry{
			Height:    msg.Height,
			View:      msg.View,
			Block:     msg.Block,
			Prepare:   make(map[string]*core.Message),
			Commit:    make(map[string]*core.Message),
			State:     StateProposed,
			Committee: committee,
		}
	}
	t.state = StateProposed
	t.recordPrepareLocked(msg.Height, msg.BlockHash, t.cfg.NodeID)
	prepare := &core.Message{
		Type:      core.MsgPrepare,
		From:      t.cfg.NodeID,
		View:      msg.View,
		Height:    msg.Height,
		BlockHash: msg.BlockHash,
		Sigs:      map[string][]byte{t.cfg.NodeID: t.signer.Sign([]byte(msg.BlockHash))},
		Timestamp: time.Now(),
	}
	t.broadcastToCommittee(prepare, committee)
	t.maybeSendCommitLocked(msg.Height, msg.BlockHash)
}

func (t *TPBFT) handlePrepare(msg *core.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.log[msg.Height]
	if !ok || !contains(entry.Committee, msg.From) {
		return
	}
	if sig, ok := msg.Sigs[msg.From]; ok && !t.signer.Verify(msg.From, []byte(msg.BlockHash), sig) {
		t.recordTrustLocked(msg.From, false, msg.Timestamp)
		return
	}
	entry.Prepare[msg.From] = msg
	t.recordTrustLocked(msg.From, true, msg.Timestamp)
	t.maybeSendCommitLocked(msg.Height, msg.BlockHash)
}

func (t *TPBFT) maybeSendCommitLocked(height uint64, hash string) {
	entry, ok := t.log[height]
	if !ok || entry.State >= StatePrepared {
		return
	}
	if len(entry.Prepare) < quorum(len(entry.Committee)) {
		return
	}
	entry.State = StatePrepared
	t.state = StatePrepared
	t.recordCommitLocked(height, hash, t.cfg.NodeID)
	commit := &core.Message{
		Type:      core.MsgCommit,
		From:      t.cfg.NodeID,
		View:      entry.View,
		Height:    height,
		BlockHash: hash,
		Sigs:      map[string][]byte{t.cfg.NodeID: t.signer.Sign([]byte(hash))},
		Timestamp: time.Now(),
	}
	t.broadcastToCommittee(commit, entry.Committee)
}

func (t *TPBFT) handleCommit(msg *core.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.log[msg.Height]
	if !ok || !contains(entry.Committee, msg.From) {
		return
	}
	if sig, ok := msg.Sigs[msg.From]; ok && !t.signer.Verify(msg.From, []byte(msg.BlockHash), sig) {
		t.recordTrustLocked(msg.From, false, msg.Timestamp)
		return
	}
	entry.Commit[msg.From] = msg
	t.recordTrustLocked(msg.From, true, msg.Timestamp)
	if len(entry.Commit) >= quorum(len(entry.Committee)) && entry.State < StateCommitted {
		entry.State = StateCommitted
		t.state = StateCommitted
		t.commitBlockLocked(entry.Block)
	}
}

func (t *TPBFT) recordPrepareLocked(height uint64, hash string, nodeID string) {
	entry, ok := t.log[height]
	if !ok {
		return
	}
	entry.Prepare[nodeID] = &core.Message{
		Type:      core.MsgPrepare,
		From:      nodeID,
		View:      entry.View,
		Height:    height,
		BlockHash: hash,
		Timestamp: time.Now(),
	}
}

func (t *TPBFT) recordCommitLocked(height uint64, hash string, nodeID string) {
	entry, ok := t.log[height]
	if !ok {
		return
	}
	entry.Commit[nodeID] = &core.Message{
		Type:      core.MsgCommit,
		From:      nodeID,
		View:      entry.View,
		Height:    height,
		BlockHash: hash,
		Timestamp: time.Now(),
	}
}

func (t *TPBFT) commitBlockLocked(block *core.Block) {
	if block == nil {
		return
	}
	t.committed[block.Height] = block
	t.height = block.Height
	t.view++
	t.leaderID = t.selectLeader(t.height + 1)
	_ = t.executor.ExecuteBlock(block)

	now := time.Now()
	txIDs := make([]string, 0, len(block.Txs))
	for _, tx := range block.Txs {
		txIDs = append(txIDs, tx.ID)
		delete(t.pending, tx.ID)
		if !tx.SubmitTime.IsZero() {
			t.latencies = append(t.latencies, float64(now.Sub(tx.SubmitTime).Microseconds())/1000.0)
		}
	}
	core.UpdateCommitWindow(&t.firstSubmitUnixNano, &t.lastCommitUnixNano, block, now)
	t.txPool.RemoveTxs(txIDs)
	atomic.AddUint64(&t.totalTxCommitted, uint64(len(block.Txs)))
	t.state = StateIdle
}

func (t *TPBFT) broadcastToCommittee(msg *core.Message, committee []string) {
	for _, nodeID := range committee {
		if nodeID == t.cfg.NodeID {
			continue
		}
		copyMsg := *msg
		copyMsg.To = nodeID
		_ = t.network.Send(&copyMsg)
	}
}

func (t *TPBFT) selectCommittee() []string {
	nodes := append([]string(nil), t.cfg.AllNodes...)
	if len(nodes) == 0 {
		return nodes
	}
	type scored struct {
		id    string
		score float64
	}
	items := make([]scored, 0, len(nodes))
	total := 0.0
	for _, node := range nodes {
		score := 0.5
		if s := t.trust.GetScore(node); s != nil {
			score = s.TotalScore
		}
		items = append(items, scored{id: node, score: score})
		total += score
	}
	avg := total / float64(len(items))
	selected := make([]string, 0, len(items))
	for _, item := range items {
		if item.score >= avg {
			selected = append(selected, item.id)
		}
	}
	minSize := minCommitteeSize(len(nodes))
	if len(selected) < minSize {
		sort.Slice(items, func(i, j int) bool {
			if items[i].score == items[j].score {
				return items[i].id < items[j].id
			}
			return items[i].score > items[j].score
		})
		selected = selected[:0]
		for i := 0; i < minSize && i < len(items); i++ {
			selected = append(selected, items[i].id)
		}
	}
	sort.Strings(selected)
	return selected
}

func (t *TPBFT) selectLeader(height uint64) string {
	committee := t.selectCommittee()
	if len(committee) == 0 {
		return ""
	}
	best := committee[0]
	bestScore := -1.0
	for _, node := range committee {
		score := 0.5
		if s := t.trust.GetScore(node); s != nil {
			score = s.TotalScore
		}
		if score > bestScore || (score == bestScore && rotateTie(node, best, committee, height)) {
			best = node
			bestScore = score
		}
	}
	return best
}

func (t *TPBFT) recordTrustLocked(nodeID string, success bool, msgTime time.Time) {
	responseMs := 0.0
	if !msgTime.IsZero() {
		responseMs = float64(time.Since(msgTime).Microseconds()) / 1000.0
	}
	t.trust.RecordRound(nodeID, success, responseMs, 1.0, float64(len(t.cfg.AllNodes)))
}

// GetTrustScore 获取指定节点的信任分数。
func (t *TPBFT) GetTrustScore(nodeID string) *common.TrustScore {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.trust.GetScore(nodeID)
}

// GetSelectedValidators 返回当前高度按信任评分得到的验证者集合。
func (t *TPBFT) GetSelectedValidators() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return append([]string(nil), t.selectCommittee()...)
}

func quorum(n int) int {
	if n <= 0 {
		return 0
	}
	f := (n - 1) / 3
	return 2*f + 1
}

func minCommitteeSize(total int) int {
	if total <= 0 {
		return 0
	}
	f := (total - 1) / 3
	size := 3*f + 1
	if size > total {
		return total
	}
	return size
}

func contains(nodes []string, nodeID string) bool {
	for _, n := range nodes {
		if n == nodeID {
			return true
		}
	}
	return false
}

func rotateTie(candidate string, current string, committee []string, height uint64) bool {
	if candidate == current {
		return false
	}
	if len(committee) == 0 {
		return candidate < current
	}
	idx := int(height % uint64(len(committee)))
	for i := 0; i < len(committee); i++ {
		node := committee[(idx+i)%len(committee)]
		if node == candidate {
			return true
		}
		if node == current {
			return false
		}
	}
	return candidate < current
}
