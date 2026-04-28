package hotstuff

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"
)

type Metrics struct {
	BlockTimeMs   float64
	PrepareMs     float64
	PreCommitMs   float64
	CommitMs      float64
	ViewChanges   int
	TotalMessages int
	CommBytes     float64
	NodeCount     int
	F             int
	Quorum        int
	FaultyRatio   float64
	ViewTimeoutMs float64
	BaseLatencyMs float64
}

type HotStuffNode struct {
	cfg      Config
	scorer   *TrustScorer
	selector *ValidatorSelector
	rng      *rand.Rand

	ID     string
	Peers  []string
	View   uint64
	Height uint64
	Total  int
	F      int

	LockedQC    *QuorumCertificate
	PrepareQC   *QuorumCertificate
	CommitQC    *QuorumCertificate
	JustifiedQC *QuorumCertificate

	PendingBlocks map[uint64]*Block

	VoteLog map[uint64]map[MessageType]map[string]*VoteMessage

	LastViewChange time.Time
	TimeoutCount   uint64

	ValidatorSelector *ValidatorSelector
	TrustScorer       *TrustScorer

	mu sync.RWMutex
}

func NewHotStuffNode(cfg Config, scorer *TrustScorer, selector *ValidatorSelector) *HotStuffNode {
	source := rand.NewSource(time.Now().UnixNano())
	n := cfg.NodeCount
	f := (n - 1) / 3
	if f < 1 && n > 1 {
		f = 1
	}

	return &HotStuffNode{
		cfg:      cfg,
		scorer:   scorer,
		selector: selector,
		rng:      rand.New(source),
		ID:       "node0",
		Peers:    make([]string, cfg.NodeCount-1),
		View:     0,
		Height:   0,
		Total:    n,
		F:        f,
		PendingBlocks: make(map[uint64]*Block),
		VoteLog:  make(map[uint64]map[MessageType]map[string]*VoteMessage),
		LastViewChange: time.Now(),
		ValidatorSelector: selector,
		TrustScorer:   scorer,
	}
}

func (n *HotStuffNode) ComputeMetrics(height int64) Metrics {
	nodeCount := n.cfg.NodeCount
	f := (nodeCount - 1) / 3
	quorum := 2*f + 1

	messagesPerBlock := 2 * (nodeCount - 1)
	if n.cfg.EnableThresholdSig {
		messagesPerBlock = (nodeCount - 1) + 1
	}

	phaseLatency := n.phaseLatencyMs(nodeCount, quorum)

	viewChanges := 0
	totalTimeout := 0.0
	currentTimeout := n.cfg.ViewTimeoutMs

	if n.rng.Float64() < n.cfg.FaultyRatio {
		viewChanges++
		totalTimeout += currentTimeout
		currentTimeout *= n.cfg.TimeoutExponent
	}

	blockTime := phaseLatency*float64(n.cfg.PipelineDepth) + totalTimeout

	return Metrics{
		BlockTimeMs:   blockTime,
		PrepareMs:     phaseLatency,
		PreCommitMs:   phaseLatency,
		CommitMs:      phaseLatency,
		ViewChanges:   viewChanges,
		TotalMessages: messagesPerBlock,
		CommBytes:     float64(messagesPerBlock * n.cfg.MessageBytes),
		NodeCount:     nodeCount,
		F:             f,
		Quorum:        quorum,
		FaultyRatio:   n.cfg.FaultyRatio,
		ViewTimeoutMs: n.cfg.ViewTimeoutMs,
		BaseLatencyMs: n.cfg.BaseLatencyMs,
	}
}

func (n *HotStuffNode) phaseLatencyMs(nodeCount int, quorum int) float64 {
	if nodeCount <= 1 {
		return 0
	}
	delays := make([]float64, nodeCount-1)
	for i := range delays {
		delays[i] = n.scorer.SampleNetworkDelayMs(n.rng)
	}
	sort.Float64s(delays)
	k := quorum - 1
	if k <= 0 {
		return 0
	}
	if k > len(delays) {
		k = len(delays)
	}
	base := delays[k-1]
	processing := 0.02 * math.Log2(float64(nodeCount)+1)
	return base + processing
}

func (n *HotStuffNode) HandleMessage(msg *ConsensusMessage) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if msg.View < n.View {
		return nil
	}

	switch msg.Type {
	case MessageTypeNewView:
		return n.handleNewView(msg)
	case MessageTypePrepare:
		return n.handlePrepare(msg)
	case MessageTypePreCommit:
		return n.handlePreCommit(msg)
	case MessageTypeCommit:
		return n.handleCommit(msg)
	case MessageTypeDecide:
		return n.handleDecide(msg)
	case MessageTypeTimeout:
		return n.handleTimeout(msg)
	}
	return nil
}

func (n *HotStuffNode) HandleVote(vote *VoteMessage) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if vote.View < n.View {
		return nil
	}

	n.storeVote(vote)
	return n.checkQuorumAndAdvance(vote.View, vote.Type)
}

func (n *HotStuffNode) storeVote(vote *VoteMessage) {
	if _, ok := n.VoteLog[vote.View]; !ok {
		n.VoteLog[vote.View] = make(map[MessageType]map[string]*VoteMessage)
	}
	if _, ok := n.VoteLog[vote.View][vote.Type]; !ok {
		n.VoteLog[vote.View][vote.Type] = make(map[string]*VoteMessage)
	}
	n.VoteLog[vote.View][vote.Type][vote.NodeID] = vote
}

func (n *HotStuffNode) handleNewView(msg *ConsensusMessage) error {
	fmt.Printf("[View %d] Node %s received NewView from %s\n", msg.View, n.ID, msg.NodeID)

	leader := n.getLeader(msg.View)
	if n.ID != leader {
		return nil
	}

	n.Propose(msg.View)
	return nil
}

func (n *HotStuffNode) Propose(view uint64) {
	block := &Block{
		Hash:      fmt.Sprintf("block-h%d-v%d", n.Height, view),
		Height:    n.Height,
		View:      view,
		Payload:   []byte(fmt.Sprintf("tx-data-%d", view)),
		ParentQC:  n.JustifiedQC,
		Proposer:  n.ID,
		Timestamp: time.Now().UnixNano(),
	}

	n.PendingBlocks[view] = block

	msg := &ConsensusMessage{
		Type:          MessageTypePrepare,
		View:          view,
		Block:         block,
		NodeID:        n.ID,
		Justification: n.JustifiedQC,
	}

	fmt.Printf("[View %d] Leader %s PROPOSES %s (parentQC view=%d)\n",
		view, n.ID, block.Hash, safeView(n.JustifiedQC))

	n.broadcast(msg)
	n.votePrepare(msg)
}

func (n *HotStuffNode) handlePrepare(msg *ConsensusMessage) error {
	if msg.Block == nil {
		return fmt.Errorf("prepare message missing block")
	}

	fmt.Printf("[View %d] Node %s received Prepare %s from %s\n",
		msg.View, n.ID, msg.Block.Hash, msg.NodeID)

	if !n.isSafe(msg.Justification, msg.View) {
		fmt.Printf("[View %d] Node %s REJECTS unsafe proposal\n", msg.View, n.ID)
		return nil
	}

	if msg.Justification != nil && higherQC(msg.Justification, n.JustifiedQC) {
		n.JustifiedQC = msg.Justification
	}

	if msg.Justification != nil && higherQC(msg.Justification, n.PrepareQC) {
		n.PrepareQC = msg.Justification
	}

	n.votePrepare(msg)
	return nil
}

func (n *HotStuffNode) votePrepare(msg *ConsensusMessage) {
	vote := &VoteMessage{
		Type:      MessageTypePrepare,
		View:      msg.View,
		BlockHash: msg.Block.Hash,
		NodeID:    n.ID,
		Signature: []byte(fmt.Sprintf("sig-%s-v%d-prepare", n.ID, msg.View)),
		HighQC:    n.JustifiedQC,
	}

	leader := n.getLeader(msg.View)
	n.sendVote(vote, leader)
}

func (n *HotStuffNode) handlePreCommit(msg *ConsensusMessage) error {
	fmt.Printf("[View %d] Node %s received PreCommit (QC view=%d)\n",
		msg.View, n.ID, safeView(msg.Justification))

	if msg.Justification == nil {
		return nil
	}

	if higherQC(msg.Justification, n.PrepareQC) {
		n.PrepareQC = msg.Justification
	}

	if n.LockedQC != nil && n.LockedQC.View > msg.Justification.View {
		fmt.Printf("[View %d] Node %s LOCKED, skip PreCommit vote\n", msg.View, n.ID)
		return nil
	}

	n.votePreCommit(msg)
	return nil
}

func (n *HotStuffNode) votePreCommit(msg *ConsensusMessage) {
	vote := &VoteMessage{
		Type:      MessageTypePreCommit,
		View:      msg.View,
		BlockHash: msg.Justification.BlockHash,
		NodeID:    n.ID,
		Signature: []byte(fmt.Sprintf("sig-%s-v%d-precommit", n.ID, msg.View)),
		HighQC:    n.JustifiedQC,
	}

	leader := n.getLeader(msg.View)
	n.sendVote(vote, leader)
}

func (n *HotStuffNode) handleCommit(msg *ConsensusMessage) error {
	fmt.Printf("[View %d] Node %s received Commit (QC view=%d)\n",
		msg.View, n.ID, safeView(msg.Justification))

	if msg.Justification == nil {
		return nil
	}

	if higherQC(msg.Justification, n.LockedQC) {
		n.LockedQC = msg.Justification
	}

	if higherQC(msg.Justification, n.PrepareQC) {
		n.PrepareQC = msg.Justification
	}

	n.voteCommit(msg)
	return nil
}

func (n *HotStuffNode) voteCommit(msg *ConsensusMessage) {
	vote := &VoteMessage{
		Type:      MessageTypeCommit,
		View:      msg.View,
		BlockHash: msg.Justification.BlockHash,
		NodeID:    n.ID,
		Signature: []byte(fmt.Sprintf("sig-%s-v%d-commit", n.ID, msg.View)),
		HighQC:    n.JustifiedQC,
	}

	leader := n.getLeader(msg.View)
	n.sendVote(vote, leader)
}

func (n *HotStuffNode) handleDecide(msg *ConsensusMessage) error {
	if msg.Justification == nil {
		return nil
	}

	fmt.Printf("[View %d] Node %s DECIDES block %s (QC view=%d)\n",
		msg.View, n.ID, msg.Justification.BlockHash, msg.Justification.View)

	if higherQC(msg.Justification, n.CommitQC) {
		n.CommitQC = msg.Justification
	}

	n.executeBlock(msg.Justification)
	return nil
}

func (n *HotStuffNode) handleTimeout(msg *ConsensusMessage) error {
	fmt.Printf("[View %d] Node %s received Timeout from %s\n", msg.View, n.ID, msg.NodeID)

	leader := n.getLeader(msg.View + 1)
	if n.ID != leader {
		return nil
	}

	n.enterNewView(msg.View + 1)
	return nil
}

func (n *HotStuffNode) checkQuorumAndAdvance(view uint64, phase MessageType) error {
	votes := n.VoteLog[view][phase]
	if len(votes) < n.QuorumSize() {
		return nil
	}

	qc := n.buildQC(view, votes)
	if qc == nil {
		return nil
	}

	fmt.Printf("[View %d] Leader %s forms QC for %v (%d signatures)\n",
		view, n.ID, phase, len(qc.Signers))

	var nextMsg *ConsensusMessage

	switch phase {
	case MessageTypePrepare:
		nextMsg = &ConsensusMessage{
			Type:          MessageTypePreCommit,
			View:          view,
			NodeID:        n.ID,
			Justification: qc,
		}
		if higherQC(qc, n.PrepareQC) {
			n.PrepareQC = qc
		}

	case MessageTypePreCommit:
		nextMsg = &ConsensusMessage{
			Type:          MessageTypeCommit,
			View:          view,
			NodeID:        n.ID,
			Justification: qc,
		}
		if higherQC(qc, n.LockedQC) {
			n.LockedQC = qc
		}

	case MessageTypeCommit:
		nextMsg = &ConsensusMessage{
			Type:          MessageTypeDecide,
			View:          view,
			NodeID:        n.ID,
			Justification: qc,
		}
		if higherQC(qc, n.CommitQC) {
			n.CommitQC = qc
		}
		n.executeBlock(qc)
		n.Height++
		n.Propose(view + 1)
	}

	if nextMsg != nil {
		n.broadcast(nextMsg)
		go n.HandleMessage(nextMsg)
	}

	return nil
}

func (n *HotStuffNode) buildQC(view uint64, votes map[string]*VoteMessage) *QuorumCertificate {
	if len(votes) == 0 {
		return nil
	}

	var blockHash string
	for _, v := range votes {
		blockHash = v.BlockHash
		break
	}

	qc := NewQC(view, blockHash)
	for nodeID, vote := range votes {
		qc.AddSignature(nodeID, vote.Signature)
	}
	return qc
}

func (n *HotStuffNode) IsSafe(proposalQC *QuorumCertificate, proposalView uint64) bool {
	return n.isSafe(proposalQC, proposalView)
}

func (n *HotStuffNode) SetLockedQC(qc *QuorumCertificate) {
	n.LockedQC = qc
}

func (n *HotStuffNode) isSafe(proposalQC *QuorumCertificate, proposalView uint64) bool {
	if n.LockedQC == nil {
		return true
	}
	if proposalQC == nil {
		return proposalView > n.LockedQC.View
	}
	return proposalQC.View >= n.LockedQC.View
}

func (n *HotStuffNode) executeBlock(qc *QuorumCertificate) {
	if qc == nil {
		return
	}

	if block, ok := n.PendingBlocks[qc.View]; ok {
		fmt.Printf("[EXECUTE] Block %s at height %d finalized\n", block.Hash, block.Height)
		delete(n.PendingBlocks, qc.View)
		n.Height++
	} else {
		fmt.Printf("[EXECUTE] Block at view %d finalized (no pending block)\n", qc.View)
	}
}

func (n *HotStuffNode) enterNewView(newView uint64) {
	n.View = newView
	n.LastViewChange = time.Now()
	n.TimeoutCount++

	fmt.Printf("[View %d] Node %s enters new view (timeout #%d)\n",
		newView, n.ID, n.TimeoutCount)

	leader := n.getLeader(newView)
	if n.ID == leader {
		n.Propose(newView)
	} else {
		fmt.Printf("[View %d] Node %s sends NewView to leader %s\n", newView, n.ID, leader)
	}
}

func (n *HotStuffNode) QuorumSize() int {
	return 2*n.F + 1
}

func (n *HotStuffNode) getLeader(view uint64) string {
	if n.ValidatorSelector != nil {
		return n.ValidatorSelector.GetLeader(view)
	}
	return n.ID
}

func (n *HotStuffNode) broadcast(msg *ConsensusMessage) {
	for _, peer := range n.Peers {
		fmt.Printf("  → Send %v View %d to %s\n", msg.Type, msg.View, peer)
	}
}

func (n *HotStuffNode) sendVote(vote *VoteMessage, leader string) {
	if n.ID == leader {
		go n.HandleVote(vote)
	} else {
		fmt.Printf("  → Vote %v View %d to leader %s\n", vote.Type, vote.View, leader)
	}
}

func higherQC(qc1, qc2 *QuorumCertificate) bool {
	if qc1 == nil {
		return false
	}
	if qc2 == nil {
		return true
	}
	return qc1.View > qc2.View
}

func safeView(qc *QuorumCertificate) int64 {
	if qc == nil {
		return -1
	}
	return int64(qc.View)
}
