package tests

import (
	"testing"
	"time"

	"github.com/fffeng99999/hcap-consensus/consensus/raft"
	"github.com/stretchr/testify/assert"
)

func raftConfig() raft.Config {
	return raft.Config{
		NodeCount:              4,
		ElectionTimeoutMs:      150,
		HeartbeatIntervalMs:    50,
		ElectionTimeoutRangeMs: 150,
		SnapshotDistance:       10000,
		MaxLogEntriesPerRPC:    500,
		MessageBytes:           256,
		FaultyRatio:            0,
		MaxValidators:          100,
	}
}

func createTestNode(cfg raft.Config) *raft.RaftNode {
	scorer := raft.NewTrustScorer(cfg)
	selector := raft.NewValidatorSelector(cfg)
	return raft.NewRaftNode(cfg, scorer, selector)
}

func TestRaftNode_Initialization(t *testing.T) {
	cfg := raftConfig()
	cfg.NodeCount = 3
	node := createTestNode(cfg)

	assert.Equal(t, "node0", node.ID)
	assert.Equal(t, uint64(0), node.CurrentTerm)
	assert.Equal(t, raft.RoleFollower, node.Role)
	assert.Equal(t, 3, node.Total)
}

func TestRaftNode_RequestVote(t *testing.T) {
	cfg := raftConfig()
	cfg.NodeCount = 2
	node := createTestNode(cfg)

	msg := &raft.ConsensusMessage{
		Type:         raft.MessageTypeRequestVote,
		Term:         1,
		SenderID:     "node1",
		LastLogIndex: 0,
		LastLogTerm:  0,
	}

	err := node.HandleMessage(msg)
	assert.NoError(t, err)
	assert.Equal(t, uint64(1), node.CurrentTerm)
	assert.Equal(t, "node1", node.VotedFor)
}

func TestRaftNode_AppendEntries(t *testing.T) {
	cfg := raftConfig()
	cfg.NodeCount = 2
	node := createTestNode(cfg)

	msg := &raft.ConsensusMessage{
		Type:         raft.MessageTypeAppendEntries,
		Term:         1,
		SenderID:     "node1",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries:      nil,
		LeaderCommit: 0,
	}

	err := node.HandleMessage(msg)
	assert.NoError(t, err)
	assert.Equal(t, uint64(1), node.CurrentTerm)
	assert.Equal(t, raft.RoleFollower, node.Role)
}

func TestRaftNode_LogReplication(t *testing.T) {
	cfg := raftConfig()
	cfg.NodeCount = 2
	node := createTestNode(cfg)

	entries := []*raft.LogEntry{
		{Term: 1, Index: 1, Command: []byte("cmd1")},
		{Term: 1, Index: 2, Command: []byte("cmd2")},
	}

	msg := &raft.ConsensusMessage{
		Type:         raft.MessageTypeAppendEntries,
		Term:         1,
		SenderID:     "node1",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries:      entries,
		LeaderCommit: 2,
	}

	err := node.HandleMessage(msg)
	assert.NoError(t, err)
	assert.Equal(t, uint64(2), node.CommitIndex)
	assert.Equal(t, 3, len(node.Log))
}

func TestRaftNode_TermMismatch(t *testing.T) {
	cfg := raftConfig()
	cfg.NodeCount = 2
	node := createTestNode(cfg)
	node.CurrentTerm = 5

	msg := &raft.ConsensusMessage{
		Type:         raft.MessageTypeRequestVote,
		Term:         3,
		SenderID:     "node1",
		LastLogIndex: 10,
		LastLogTerm:  3,
	}

	err := node.HandleMessage(msg)
	assert.NoError(t, err)
	assert.Equal(t, uint64(5), node.CurrentTerm)
	assert.Equal(t, "", node.VotedFor)
}

func TestRaftNode_LogUpToDate(t *testing.T) {
	cfg := raftConfig()
	cfg.NodeCount = 2
	node := createTestNode(cfg)
	node.Log = append(node.Log, &raft.LogEntry{Term: 2, Index: 5, Command: nil})

	assert.True(t, node.IsLogUpToDate(5, 2))
	assert.True(t, node.IsLogUpToDate(6, 3))
	assert.False(t, node.IsLogUpToDate(4, 2))
	assert.False(t, node.IsLogUpToDate(5, 1))
}

func TestRaftConsensus_Lifecycle(t *testing.T) {
	cfg := raftConfig()
	cfg.ElectionTimeoutMs = 10000
	cfg.HeartbeatIntervalMs = 1000

	consensus := raft.NewRaftConsensus(cfg)

	err := consensus.Start()
	assert.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	status := consensus.GetStatus()
	assert.Equal(t, true, status["running"])

	err = consensus.Stop()
	assert.NoError(t, err)
}

func TestRaftConsensus_SubmitCommand(t *testing.T) {
	cfg := raftConfig()
	cfg.ElectionTimeoutMs = 10000

	consensus := raft.NewRaftConsensus(cfg)

	node := consensus.GetNode()
	node.Role = raft.RoleLeader
	node.CurrentTerm = 1

	ok := consensus.SubmitCommand([]byte("test-command"))
	assert.True(t, ok)
	assert.Equal(t, 2, len(node.Log))
}

func TestVoteTracker_HasMajority(t *testing.T) {
	vt := raft.NewVoteTracker()

	vt.RecordVote("n1", true)
	vt.RecordVote("n2", true)
	vt.RecordVote("n3", false)

	assert.False(t, vt.HasMajority(5))
	assert.True(t, vt.HasMajority(3))
}

func TestValidatorSelector_Update(t *testing.T) {
	cfg := raftConfig()
	cfg.NodeCount = 3
	vs := raft.NewValidatorSelector(cfg)

	vs.UpdateValidators([]string{"n3", "n4", "n5"})
	assert.Equal(t, 3, vs.Count())
	assert.True(t, vs.Contains("n3"))
	assert.False(t, vs.Contains("node0"))
}

func TestTrustScorer_RecordStats(t *testing.T) {
	cfg := raftConfig()
	ts := raft.NewTrustScorer(cfg)

	ts.RecordHeartbeat("node1")
	ts.RecordSuccess("node1")
	ts.RecordSuccess("node1")
	ts.RecordFailure("node1")

	score := ts.GetScore("node1")
	assert.InDelta(t, 0.2, score, 0.01)

	stats := ts.GetStats("node1")
	assert.NotNil(t, stats)
	assert.Equal(t, int64(2), stats.SuccessCount)
	assert.Equal(t, int64(1), stats.FailureCount)
	assert.True(t, ts.IsAvailable("node1"))
}

func TestRaftNode_ComputeMetrics(t *testing.T) {
	cfg := raftConfig()
	cfg.NodeCount = 4

	node := createTestNode(cfg)
	metrics := node.ComputeMetrics(1)

	assert.Equal(t, 4, metrics.NodeCount)
	assert.Equal(t, 3, metrics.Quorum)
	assert.Greater(t, metrics.BlockTimeMs, 0.0)
	assert.GreaterOrEqual(t, metrics.TotalMessages, 0)
}
