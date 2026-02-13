package tests

import (
	"testing"
	"time"

	"github.com/fffeng99999/hcp-consensus/consensus/raft"
	"github.com/stretchr/testify/assert"
)

func TestRaftNode_Initialization(t *testing.T) {
	node := raft.NewRaftNode("node1", []string{"node2", "node3"})

	assert.Equal(t, "node1", node.ID)
	assert.Equal(t, uint64(0), node.CurrentTerm)
	assert.Equal(t, raft.RoleFollower, node.Role)
}

func TestRaftNode_RequestVote(t *testing.T) {
	node := raft.NewRaftNode("node1", []string{"node2"})

	// Term 1, node2 asks for vote
	msg := &raft.ConsensusMessage{
		Type:         raft.MessageTypeRequestVote,
		Term:         1,
		SenderID:     "node2",
		LastLogIndex: 0,
		LastLogTerm:  0,
	}

	err := node.HandleMessage(msg)
	assert.NoError(t, err)
	assert.Equal(t, uint64(1), node.CurrentTerm)
	assert.Equal(t, "node2", node.VotedFor)
}

func TestRaftNode_AppendEntries(t *testing.T) {
	node := raft.NewRaftNode("node1", []string{"node2"})

	// Term 1, node2 (leader) sends heartbeat
	msg := &raft.ConsensusMessage{
		Type:         raft.MessageTypeAppendEntries,
		Term:         1,
		SenderID:     "node2",
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

func TestRaftConsensus_Lifecycle(t *testing.T) {
	consensus := raft.NewRaftConsensus()

	err := consensus.Start()
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	err = consensus.Stop()
	assert.NoError(t, err)
}
