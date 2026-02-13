package raft

// MessageType represents the type of Raft message
type MessageType int

const (
	MessageTypeRequestVote MessageType = iota
	MessageTypeRequestVoteResponse
	MessageTypeAppendEntries
	MessageTypeAppendEntriesResponse
)

// ConsensusMessage represents a generic Raft message
type ConsensusMessage struct {
	Type           MessageType
	Term           uint64
	SenderID       string
	
	// RequestVote fields
	LastLogIndex   uint64
	LastLogTerm    uint64
	
	// RequestVoteResponse fields
	VoteGranted    bool
	
	// AppendEntries fields
	PrevLogIndex   uint64
	PrevLogTerm    uint64
	Entries        []*LogEntry
	LeaderCommit   uint64
	
	// AppendEntriesResponse fields
	Success        bool
	MatchIndex     uint64
}

// LogEntry represents a log entry
type LogEntry struct {
	Term    uint64
	Index   uint64
	Command []byte
}
