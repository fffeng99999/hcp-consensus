package raft

type MessageType int

const (
	MessageTypeRequestVote MessageType = iota
	MessageTypeRequestVoteResponse
	MessageTypeAppendEntries
	MessageTypeAppendEntriesResponse
	MessageTypeSnapshot
	MessageTypeInstallSnapshot
	MessageTypeTimeoutNow
)

type ConsensusMessage struct {
	Type     MessageType
	Term     uint64
	SenderID string

	LastLogIndex uint64
	LastLogTerm  uint64

	VoteGranted bool

	PrevLogIndex uint64
	PrevLogTerm  uint64
	Entries      []*LogEntry
	LeaderCommit uint64

	Success    bool
	MatchIndex uint64

	Snapshot      []byte
	SnapshotIndex uint64
	SnapshotTerm  uint64

	Configuration    *Configuration
	ConfigurationAck *Configuration
}

type LogEntry struct {
	Term    uint64
	Index   uint64
	Command []byte
}

type Configuration struct {
	Nodes    []string
	Joint    bool
	OldNodes []string
	NewNodes []string
}

type VoteTracker struct {
	Votes map[string]bool
}

func NewVoteTracker() *VoteTracker {
	return &VoteTracker{
		Votes: make(map[string]bool),
	}
}

func (vt *VoteTracker) RecordVote(nodeID string, granted bool) {
	vt.Votes[nodeID] = granted
}

func (vt *VoteTracker) HasMajority(totalNodes int) bool {
	yes := 0
	for _, v := range vt.Votes {
		if v {
			yes++
		}
	}
	return yes > totalNodes/2
}

func (vt *VoteTracker) CountVotes() (yes, no int) {
	for _, v := range vt.Votes {
		if v {
			yes++
		} else {
			no++
		}
	}
	return
}
