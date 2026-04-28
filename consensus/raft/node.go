package raft

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"
)

type Metrics struct {
	BlockTimeMs         float64
	AppendEntriesMs     float64
	ReplicationMs       float64
	ElectionMs          float64
	Elections           int
	HeartbeatMessages   int
	TotalMessages       int
	CommBytes           float64
	NodeCount           int
	Quorum              int
	FaultyRatio         float64
	ElectionTimeoutMs   float64
	HeartbeatIntervalMs float64
}

type Role int

const (
	RoleFollower Role = iota
	RoleCandidate
	RoleLeader
)

type RaftNode struct {
	cfg      Config
	scorer   *TrustScorer
	selector *ValidatorSelector
	rng      *rand.Rand

	ID    string
	Peers []string
	Total int

	CurrentTerm uint64
	VotedFor    string
	Log         []*LogEntry

	CommitIndex uint64
	LastApplied uint64

	NextIndex  map[string]uint64
	MatchIndex map[string]uint64

	Role            Role
	ElectionTimer   *time.Timer
	HeartbeatTicker *time.Ticker

	VoteTracker *VoteTracker

	LastHeartbeat time.Time
	mu            sync.RWMutex
}

func NewRaftNode(cfg Config, scorer *TrustScorer, selector *ValidatorSelector) *RaftNode {
	source := rand.NewSource(time.Now().UnixNano())
	n := cfg.NodeCount

	return &RaftNode{
		cfg:           cfg,
		scorer:        scorer,
		selector:      selector,
		rng:           rand.New(source),
		ID:            "node0",
		Peers:         make([]string, cfg.NodeCount-1),
		Total:         n,
		CurrentTerm:   0,
		VotedFor:      "",
		Log:           []*LogEntry{{Term: 0, Index: 0, Command: nil}},
		CommitIndex:   0,
		LastApplied:   0,
		NextIndex:     make(map[string]uint64),
		MatchIndex:    make(map[string]uint64),
		Role:          RoleFollower,
		VoteTracker:   NewVoteTracker(),
		LastHeartbeat: time.Now(),
	}
}

func (n *RaftNode) ComputeMetrics(height int64) Metrics {
	nodeCount := n.cfg.NodeCount
	quorum := nodeCount/2 + 1

	appendEntriesLatency := n.phaseLatencyMs(nodeCount, quorum)

	messagesPerBlock := 2 * (nodeCount - 1)

	blockTimeMs := 100.0
	heartbeatsPerBlock := int(blockTimeMs / n.cfg.HeartbeatIntervalMs)
	if heartbeatsPerBlock < 1 {
		heartbeatsPerBlock = 1
	}
	heartbeatMessages := heartbeatsPerBlock * (nodeCount - 1)

	electionMs := 0.0
	elections := 0
	if n.rng.Float64() < n.cfg.FaultyRatio {
		elections++
		electionTimeout := n.cfg.ElectionTimeoutMs + n.rng.Float64()*n.cfg.ElectionTimeoutRangeMs
		voteLatency := n.phaseLatencyMs(nodeCount, quorum)
		electionMs = electionTimeout + voteLatency
	}

	totalMessages := messagesPerBlock + heartbeatMessages
	if elections > 0 {
		totalMessages += (nodeCount - 1) + (quorum - 1)
	}

	blockTime := appendEntriesLatency + electionMs

	return Metrics{
		BlockTimeMs:         blockTime,
		AppendEntriesMs:     appendEntriesLatency / 2,
		ReplicationMs:       appendEntriesLatency / 2,
		ElectionMs:          electionMs,
		Elections:           elections,
		HeartbeatMessages:   heartbeatMessages,
		TotalMessages:       totalMessages,
		CommBytes:           float64(totalMessages * n.cfg.MessageBytes),
		NodeCount:           nodeCount,
		Quorum:              quorum,
		FaultyRatio:         n.cfg.FaultyRatio,
		ElectionTimeoutMs:   n.cfg.ElectionTimeoutMs,
		HeartbeatIntervalMs: n.cfg.HeartbeatIntervalMs,
	}
}

func (n *RaftNode) phaseLatencyMs(nodeCount int, quorum int) float64 {
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

func (n *RaftNode) Start() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.resetElectionTimer()
}

func (n *RaftNode) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ElectionTimer != nil {
		n.ElectionTimer.Stop()
	}
	if n.HeartbeatTicker != nil {
		n.HeartbeatTicker.Stop()
	}
}

func (n *RaftNode) HandleMessage(msg *ConsensusMessage) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if msg.Term > n.CurrentTerm {
		n.CurrentTerm = msg.Term
		n.VotedFor = ""
		if n.Role == RoleLeader {
			n.stopHeartbeat()
		}
		n.Role = RoleFollower
		n.resetElectionTimer()
	}

	switch msg.Type {
	case MessageTypeRequestVote:
		return n.handleRequestVote(msg)
	case MessageTypeRequestVoteResponse:
		return n.handleRequestVoteResponse(msg)
	case MessageTypeAppendEntries:
		return n.handleAppendEntries(msg)
	case MessageTypeAppendEntriesResponse:
		return n.handleAppendEntriesResponse(msg)
	case MessageTypeTimeoutNow:
		n.startElection()
	}
	return nil
}

func (n *RaftNode) handleRequestVote(msg *ConsensusMessage) error {
	reply := &ConsensusMessage{
		Type:        MessageTypeRequestVoteResponse,
		Term:        n.CurrentTerm,
		SenderID:    n.ID,
		VoteGranted: false,
	}

	if msg.Term < n.CurrentTerm {
		n.send(reply, msg.SenderID)
		return nil
	}

	if (n.VotedFor == "" || n.VotedFor == msg.SenderID) && n.isLogUpToDate(msg.LastLogIndex, msg.LastLogTerm) {
		n.VotedFor = msg.SenderID
		reply.VoteGranted = true
		n.resetElectionTimer()
		fmt.Printf("[RV] Node %s grants vote to %s (term %d)\n", n.ID, msg.SenderID, msg.Term)
	}

	n.send(reply, msg.SenderID)
	return nil
}

func (n *RaftNode) handleRequestVoteResponse(msg *ConsensusMessage) error {
	if n.Role != RoleCandidate {
		return nil
	}

	if msg.Term > n.CurrentTerm {
		n.CurrentTerm = msg.Term
		n.Role = RoleFollower
		n.resetElectionTimer()
		return nil
	}

	if msg.VoteGranted {
		n.VoteTracker.RecordVote(msg.SenderID, true)
		fmt.Printf("[RV] Node %s received vote from %s (term %d)\n", n.ID, msg.SenderID, n.CurrentTerm)

		if n.VoteTracker.HasMajority(n.Total) {
			n.becomeLeader()
		}
	} else {
		n.VoteTracker.RecordVote(msg.SenderID, false)
	}

	return nil
}

func (n *RaftNode) handleAppendEntries(msg *ConsensusMessage) error {
	reply := &ConsensusMessage{
		Type:       MessageTypeAppendEntriesResponse,
		Term:       n.CurrentTerm,
		SenderID:   n.ID,
		Success:    false,
		MatchIndex: 0,
	}

	if msg.Term < n.CurrentTerm {
		n.send(reply, msg.SenderID)
		return nil
	}

	n.LastHeartbeat = time.Now()
	n.resetElectionTimer()

	if n.Role == RoleCandidate {
		n.Role = RoleFollower
	}

	if msg.PrevLogIndex > 0 && (msg.PrevLogIndex >= uint64(len(n.Log)) || n.Log[msg.PrevLogIndex].Term != msg.PrevLogTerm) {
		n.send(reply, msg.SenderID)
		return nil
	}

	for _, entry := range msg.Entries {
		idx := entry.Index
		if idx < uint64(len(n.Log)) {
			if n.Log[idx].Term != entry.Term {
				n.Log = n.Log[:idx]
				n.Log = append(n.Log, entry)
			}
		} else {
			n.Log = append(n.Log, entry)
		}
	}

	if msg.LeaderCommit > n.CommitIndex {
		lastNewEntry := msg.PrevLogIndex + uint64(len(msg.Entries))
		if msg.LeaderCommit < lastNewEntry {
			n.CommitIndex = msg.LeaderCommit
		} else {
			n.CommitIndex = lastNewEntry
		}
		n.maybeApply()
	}

	reply.Success = true
	reply.MatchIndex = n.lastLogIndex()
	n.send(reply, msg.SenderID)
	return nil
}

func (n *RaftNode) handleAppendEntriesResponse(msg *ConsensusMessage) error {
	if n.Role != RoleLeader {
		return nil
	}

	if msg.Term > n.CurrentTerm {
		n.CurrentTerm = msg.Term
		n.Role = RoleFollower
		n.resetElectionTimer()
		return nil
	}

	if msg.Success {
		n.MatchIndex[msg.SenderID] = msg.MatchIndex
		n.NextIndex[msg.SenderID] = msg.MatchIndex + 1
		n.updateCommitIndex()
	} else {
		if n.NextIndex[msg.SenderID] > 1 {
			n.NextIndex[msg.SenderID]--
		}
	}
	return nil
}

func (n *RaftNode) updateCommitIndex() {
	for i := n.CommitIndex + 1; i <= n.lastLogIndex(); i++ {
		if n.Log[i].Term == n.CurrentTerm && n.countMatch(i) > n.Total/2 {
			n.CommitIndex = i
		}
	}
}

func (n *RaftNode) countMatch(index uint64) int {
	count := 1
	for _, peer := range n.Peers {
		if n.MatchIndex[peer] >= index {
			count++
		}
	}
	return count
}

func (n *RaftNode) maybeApply() {
	for n.LastApplied < n.CommitIndex {
		n.LastApplied++
		entry := n.Log[n.LastApplied]
		fmt.Printf("[APPLY] Node %s applies log %d (term %d)\n", n.ID, entry.Index, entry.Term)
	}
}

func (n *RaftNode) startElection() {
	if n.Role == RoleLeader {
		return
	}

	n.Role = RoleCandidate
	n.CurrentTerm++
	n.VotedFor = n.ID
	n.VoteTracker = NewVoteTracker()
	n.VoteTracker.RecordVote(n.ID, true)

	fmt.Printf("[ELECTION] Node %s starts election for term %d\n", n.ID, n.CurrentTerm)
	n.resetElectionTimer()

	lastIndex := n.lastLogIndex()
	lastTerm := n.lastLogTerm()

	msg := &ConsensusMessage{
		Type:         MessageTypeRequestVote,
		Term:         n.CurrentTerm,
		SenderID:     n.ID,
		LastLogIndex: lastIndex,
		LastLogTerm:  lastTerm,
	}

	n.broadcast(msg)
}

func (n *RaftNode) becomeLeader() {
	n.Role = RoleLeader
	fmt.Printf("[LEADER] Node %s became Leader for term %d\n", n.ID, n.CurrentTerm)

	lastIndex := n.lastLogIndex()
	for _, peer := range n.Peers {
		n.NextIndex[peer] = lastIndex + 1
		n.MatchIndex[peer] = 0
	}

	n.startHeartbeat()
}

func (n *RaftNode) startHeartbeat() {
	n.stopHeartbeat()
	interval := time.Duration(n.cfg.HeartbeatIntervalMs) * time.Millisecond
	n.HeartbeatTicker = time.NewTicker(interval)
	go func() {
		for {
			n.mu.Lock()
			if n.Role != RoleLeader {
				n.HeartbeatTicker.Stop()
				n.mu.Unlock()
				return
			}
			n.sendHeartbeat()
			n.mu.Unlock()
			time.Sleep(10 * time.Millisecond)
		}
	}()
}

func (n *RaftNode) stopHeartbeat() {
	if n.HeartbeatTicker != nil {
		n.HeartbeatTicker.Stop()
		n.HeartbeatTicker = nil
	}
}

func (n *RaftNode) sendHeartbeat() {
	lastIdx := n.lastLogIndex()

	for _, peer := range n.Peers {
		prevIdx := n.NextIndex[peer] - 1
		prevTerm := uint64(0)
		if prevIdx > 0 && prevIdx < uint64(len(n.Log)) {
			prevTerm = n.Log[prevIdx].Term
		}

		var entries []*LogEntry
		if n.NextIndex[peer] <= lastIdx {
			entries = n.Log[n.NextIndex[peer]:]
		}

		msg := &ConsensusMessage{
			Type:         MessageTypeAppendEntries,
			Term:         n.CurrentTerm,
			SenderID:     n.ID,
			PrevLogIndex: prevIdx,
			PrevLogTerm:  prevTerm,
			Entries:      entries,
			LeaderCommit: n.CommitIndex,
		}

		n.send(msg, peer)
	}
}

func (n *RaftNode) SubmitCommand(command []byte) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.Role != RoleLeader {
		return false
	}

	entry := &LogEntry{
		Term:    n.CurrentTerm,
		Index:   n.lastLogIndex() + 1,
		Command: command,
	}
	n.Log = append(n.Log, entry)

	fmt.Printf("[SUBMIT] Leader %s accepted command: %s\n", n.ID, string(command))
	return true
}

func (n *RaftNode) resetElectionTimer() {
	if n.ElectionTimer != nil {
		n.ElectionTimer.Stop()
	}
	timeout := time.Duration(n.cfg.ElectionTimeoutMs+n.cfg.ElectionTimeoutRangeMs) * time.Millisecond
	n.ElectionTimer = time.AfterFunc(timeout, func() {
		n.mu.Lock()
		defer n.mu.Unlock()
		n.checkElectionTimeout()
	})
}

func (n *RaftNode) checkElectionTimeout() {
	if n.Role == RoleLeader {
		return
	}
	sinceLast := time.Since(n.LastHeartbeat)
	timeout := time.Duration(n.cfg.ElectionTimeoutMs) * time.Millisecond
	if sinceLast > timeout {
		n.startElection()
	} else {
		n.resetElectionTimer()
	}
}

func (n *RaftNode) isLogUpToDate(lastLogIndex, lastLogTerm uint64) bool {
	myLastIndex := n.lastLogIndex()
	myLastTerm := n.lastLogTerm()

	if lastLogTerm != myLastTerm {
		return lastLogTerm > myLastTerm
	}
	return lastLogIndex >= myLastIndex
}

func (n *RaftNode) IsLogUpToDate(lastLogIndex, lastLogTerm uint64) bool {
	return n.isLogUpToDate(lastLogIndex, lastLogTerm)
}

func (n *RaftNode) lastLogIndex() uint64 {
	if len(n.Log) == 0 {
		return 0
	}
	return n.Log[len(n.Log)-1].Index
}

func (n *RaftNode) lastLogTerm() uint64 {
	if len(n.Log) == 0 {
		return 0
	}
	return n.Log[len(n.Log)-1].Term
}

func (n *RaftNode) send(msg *ConsensusMessage, target string) {
	fmt.Printf("  → %s to %s (term %d)\n", msg.Type, target, msg.Term)
}

func (n *RaftNode) broadcast(msg *ConsensusMessage) {
	for _, peer := range n.Peers {
		n.send(msg, peer)
	}
}

func (n *RaftNode) GetStatus() map[string]interface{} {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return map[string]interface{}{
		"id":           n.ID,
		"role":         n.Role,
		"term":         n.CurrentTerm,
		"voted_for":    n.VotedFor,
		"log_len":      len(n.Log),
		"commit_index": n.CommitIndex,
		"last_applied": n.LastApplied,
	}
}

func (n *RaftNode) QuorumSize() int {
	return n.Total/2 + 1
}

func (m MessageType) String() string {
	switch m {
	case MessageTypeRequestVote:
		return "RequestVote"
	case MessageTypeRequestVoteResponse:
		return "RequestVoteResponse"
	case MessageTypeAppendEntries:
		return "AppendEntries"
	case MessageTypeAppendEntriesResponse:
		return "AppendEntriesResponse"
	case MessageTypeSnapshot:
		return "Snapshot"
	case MessageTypeInstallSnapshot:
		return "InstallSnapshot"
	case MessageTypeTimeoutNow:
		return "TimeoutNow"
	default:
		return "Unknown"
	}
}
