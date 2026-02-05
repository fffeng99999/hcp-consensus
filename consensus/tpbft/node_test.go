package tpbft

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPBFTNode_ConsensusFlow(t *testing.T) {
	// Setup 4 nodes (f=1, N=4)
	nodeIDs := []string{"node0", "node1", "node2", "node3"}
	nodes := make([]*PBFTNode, 4)

	for i, id := range nodeIDs {
		// Peers are everyone else
		var peers []string
		for _, pid := range nodeIDs {
			if pid != id {
				peers = append(peers, pid)
			}
		}
		nodes[i] = NewPBFTNode(id, peers)
	}

	seq := uint64(1)
	view := uint64(0)

	// 1. PrePrepare Phase
	// Leader (node0) proposes
	prePrepareMsg := &ConsensusMessage{
		Type:           MessageTypePrePrepare,
		View:           view,
		SequenceNumber: seq,
		Digest:         "block-hash-1",
		NodeID:         "node0",
		Data:           []byte("block-data"),
	}

	// All nodes receive PrePrepare
	for _, node := range nodes {
		err := node.HandleMessage(prePrepareMsg)
		assert.NoError(t, err)
	}

	// 2. Prepare Phase
	// Each node broadcasts Prepare (simulated)
	prepareMsgs := make([]*ConsensusMessage, 4)
	for i, id := range nodeIDs {
		prepareMsgs[i] = &ConsensusMessage{
			Type:           MessageTypePrepare,
			View:           view,
			SequenceNumber: seq,
			Digest:         "block-hash-1",
			NodeID:         id,
		}
	}

	// Deliver Prepare messages to all nodes
	// We need 2f+1 = 3 votes to enter PREPARED state.
	// Let's deliver 3 votes (node0, node1, node2) to node3

	// Check before votes
	assert.False(t, nodes[3].Prepared[seq], "Node3 should not be prepared yet")

	// Deliver votes
	nodes[3].HandleMessage(prepareMsgs[0])
	nodes[3].HandleMessage(prepareMsgs[1])
	nodes[3].HandleMessage(prepareMsgs[2])

	// Check after 3 votes (Quorum met)
	assert.True(t, nodes[3].Prepared[seq], "Node3 should be PREPARED")

	// 3. Commit Phase
	// Each node broadcasts Commit (simulated)
	commitMsgs := make([]*ConsensusMessage, 4)
	for i, id := range nodeIDs {
		commitMsgs[i] = &ConsensusMessage{
			Type:           MessageTypeCommit,
			View:           view,
			SequenceNumber: seq,
			Digest:         "block-hash-1",
			NodeID:         id,
		}
	}

	// Check before votes
	assert.False(t, nodes[3].Committed[seq], "Node3 should not be committed yet")

	// Deliver votes
	nodes[3].HandleMessage(commitMsgs[0])
	nodes[3].HandleMessage(commitMsgs[1])
	nodes[3].HandleMessage(commitMsgs[2])

	// Check after 3 votes
	assert.True(t, nodes[3].Committed[seq], "Node3 should be COMMITTED")
}
