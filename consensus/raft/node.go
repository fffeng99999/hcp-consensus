package raft

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// Role represents the server role
type Role int

const (
	RoleFollower Role = iota
	RoleCandidate
	RoleLeader
)

// RaftNode represents a node in the Raft consensus network
type RaftNode struct {
	ID    string
	Peers []string
	
	// Persistent state on all servers
	CurrentTerm uint64
	VotedFor    string
	Log         []*LogEntry

	// Volatile state on all servers
	CommitIndex uint64
	LastApplied uint64

	// Volatile state on leaders
	NextIndex  map[string]uint64
	MatchIndex map[string]uint64

	// State
	Role            Role
	ElectionTimer   *time.Timer
	HeartbeatTicker *time.Ticker
	
	// Timings
	ElectionTimeout   time.Duration
	HeartbeatInterval time.Duration

	mu sync.RWMutex
}

// NewRaftNode creates a new Raft node
func NewRaftNode(id string, peers []string) *RaftNode {
	node := &RaftNode{
		ID:                id,
		Peers:             peers,
		CurrentTerm:       0,
		VotedFor:          "",
		Log:               make([]*LogEntry, 0),
		CommitIndex:       0,
		LastApplied:       0,
		NextIndex:         make(map[string]uint64),
		MatchIndex:        make(map[string]uint64),
		Role:              RoleFollower,
		ElectionTimeout:   150 * time.Millisecond, // Base timeout
		HeartbeatInterval: 50 * time.Millisecond,
	}
	
	// Initialize with a dummy entry at index 0
	node.Log = append(node.Log, &LogEntry{Term: 0, Index: 0, Command: nil})
	
	return node
}

// Start starts the node
func (n *RaftNode) Start() {
	n.mu.Lock()
	defer n.mu.Unlock()
	
	n.resetElectionTimer()
}

// Stop stops the node
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

// HandleMessage processes an incoming consensus message
func (n *RaftNode) HandleMessage(msg *ConsensusMessage) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// All servers: If RPC request or response contains term T > currentTerm:
	// set currentTerm = T, convert to follower
	if msg.Term > n.CurrentTerm {
		n.CurrentTerm = msg.Term
		n.VotedFor = ""
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
	}
	return nil
}

// --- Message Handlers ---

func (n *RaftNode) handleRequestVote(msg *ConsensusMessage) error {
	reply := &ConsensusMessage{
		Type:        MessageTypeRequestVoteResponse,
		Term:        n.CurrentTerm,
		SenderID:    n.ID,
		VoteGranted: false,
	}

	// 1. Reply false if term < currentTerm
	if msg.Term < n.CurrentTerm {
		n.send(reply, msg.SenderID)
		return nil
	}

	// 2. If votedFor is null or candidateId, and candidate's log is at least as up-to-date as receiver's log, grant vote
	if (n.VotedFor == "" || n.VotedFor == msg.SenderID) && n.isLogUpToDate(msg.LastLogIndex, msg.LastLogTerm) {
		n.VotedFor = msg.SenderID
		reply.VoteGranted = true
		n.resetElectionTimer()
		fmt.Printf("Node %s granted vote to %s for term %d\n", n.ID, msg.SenderID, msg.Term)
	}

	n.send(reply, msg.SenderID)
	return nil
}

func (n *RaftNode) handleRequestVoteResponse(msg *ConsensusMessage) error {
	if n.Role != RoleCandidate {
		return nil
	}

	if msg.VoteGranted {
		// Count votes (simplified: assume majority if we get one for now, or just log)
		// In real impl, we track votes received
		fmt.Printf("Node %s received vote from %s\n", n.ID, msg.SenderID)
		
		// If majority, become leader
		// For simulation, let's just become leader on first vote (simplification)
		// Or assume single node cluster for now if peers empty
		if len(n.Peers) == 0 {
			n.becomeLeader()
		}
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

	// 1. Reply false if term < currentTerm
	if msg.Term < n.CurrentTerm {
		n.send(reply, msg.SenderID)
		return nil
	}

	// Reset election timer on valid heartbeat/appendEntries
	n.resetElectionTimer()
	if n.Role == RoleCandidate {
		n.Role = RoleFollower
	}

	// 2. Reply false if log doesn't contain an entry at prevLogIndex whose term matches prevLogTerm
	if msg.PrevLogIndex >= uint64(len(n.Log)) || n.Log[msg.PrevLogIndex].Term != msg.PrevLogTerm {
		n.send(reply, msg.SenderID)
		return nil
	}

	// 3. If an existing entry conflicts with a new one (same index but different terms), delete the existing entry and all that follow it
	// 4. Append any new entries not already in the log
	// Simplified: append entries
	for _, entry := range msg.Entries {
		index := entry.Index
		if index < uint64(len(n.Log)) {
			if n.Log[index].Term != entry.Term {
				n.Log = n.Log[:index]
				n.Log = append(n.Log, entry)
			}
		} else {
			n.Log = append(n.Log, entry)
		}
	}

	// 5. If leaderCommit > commitIndex, set commitIndex = min(leaderCommit, index of last new entry)
	if msg.LeaderCommit > n.CommitIndex {
		lastEntryIndex := n.Log[len(n.Log)-1].Index
		if msg.LeaderCommit < lastEntryIndex {
			n.CommitIndex = msg.LeaderCommit
		} else {
			n.CommitIndex = lastEntryIndex
		}
		// Apply to state machine...
	}

	reply.Success = true
	reply.MatchIndex = n.Log[len(n.Log)-1].Index
	n.send(reply, msg.SenderID)
	
	return nil
}

func (n *RaftNode) handleAppendEntriesResponse(msg *ConsensusMessage) error {
	if n.Role != RoleLeader {
		return nil
	}

	if msg.Success {
		// Update nextIndex and matchIndex for follower
		n.MatchIndex[msg.SenderID] = msg.MatchIndex
		n.NextIndex[msg.SenderID] = msg.MatchIndex + 1
		
		// Update commitIndex if majority matchIndex[i] > commitIndex
		// Simplified...
	} else {
		// Decrement nextIndex and retry
		if n.NextIndex[msg.SenderID] > 1 {
			n.NextIndex[msg.SenderID]--
		}
	}
	return nil
}

// --- Core Logic ---

func (n *RaftNode) resetElectionTimer() {
	if n.ElectionTimer != nil {
		n.ElectionTimer.Stop()
	}
	// Randomized timeout: 150ms - 300ms
	timeout := n.ElectionTimeout + time.Duration(rand.Intn(150))*time.Millisecond
	n.ElectionTimer = time.AfterFunc(timeout, func() {
		n.mu.Lock()
		defer n.mu.Unlock()
		n.startElection()
	})
}

func (n *RaftNode) startElection() {
	if n.Role == RoleLeader {
		return
	}
	
	n.Role = RoleCandidate
	n.CurrentTerm++
	n.VotedFor = n.ID
	fmt.Printf("Node %s starting election for term %d\n", n.ID, n.CurrentTerm)
	
	n.resetElectionTimer()
	
	// Request votes
	lastLogIndex := n.Log[len(n.Log)-1].Index
	lastLogTerm := n.Log[len(n.Log)-1].Term
	
	msg := &ConsensusMessage{
		Type:         MessageTypeRequestVote,
		Term:         n.CurrentTerm,
		SenderID:     n.ID,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	
	n.broadcast(msg)
	
	// Vote for self
	// In simulation, we handle it immediately or assume 1 vote
}

func (n *RaftNode) becomeLeader() {
	n.Role = RoleLeader
	fmt.Printf("Node %s became Leader for term %d\n", n.ID, n.CurrentTerm)
	
	// Initialize nextIndex/matchIndex
	lastLogIndex := n.Log[len(n.Log)-1].Index
	for _, peer := range n.Peers {
		n.NextIndex[peer] = lastLogIndex + 1
		n.MatchIndex[peer] = 0
	}
	
	// Start heartbeat
	n.sendHeartbeat()
	n.HeartbeatTicker = time.NewTicker(n.HeartbeatInterval)
	go func() {
		for range n.HeartbeatTicker.C {
			n.mu.Lock()
			if n.Role != RoleLeader {
				n.HeartbeatTicker.Stop()
				n.mu.Unlock()
				return
			}
			n.sendHeartbeat()
			n.mu.Unlock()
		}
	}()
}

func (n *RaftNode) sendHeartbeat() {
	for _, peer := range n.Peers {
		// Prepare AppendEntries
		prevLogIndex := n.NextIndex[peer] - 1
		prevLogTerm := uint64(0)
		if prevLogIndex < uint64(len(n.Log)) {
			prevLogTerm = n.Log[prevLogIndex].Term
		}
		
		msg := &ConsensusMessage{
			Type:         MessageTypeAppendEntries,
			Term:         n.CurrentTerm,
			SenderID:     n.ID,
			LeaderCommit: n.CommitIndex,
			PrevLogIndex: prevLogIndex,
			PrevLogTerm:  prevLogTerm,
			Entries:      nil, // Heartbeat
		}
		
		// Send to peer
		n.send(msg, peer)
	}
}

// --- Helpers ---

func (n *RaftNode) isLogUpToDate(lastLogIndex, lastLogTerm uint64) bool {
	myLastIndex := n.Log[len(n.Log)-1].Index
	myLastTerm := n.Log[len(n.Log)-1].Term
	
	if lastLogTerm != myLastTerm {
		return lastLogTerm > myLastTerm
	}
	return lastLogIndex >= myLastIndex
}

func (n *RaftNode) send(msg *ConsensusMessage, target string) {
	// Simulation: print or mock
	// fmt.Printf("SEND %v to %s\n", msg.Type, target)
}

func (n *RaftNode) broadcast(msg *ConsensusMessage) {
	for _, peer := range n.Peers {
		n.send(msg, peer)
	}
}
