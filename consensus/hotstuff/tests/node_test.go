package tests

import (
	"testing"

	"github.com/fffeng99999/hcp-consensus/consensus/hotstuff"
	"github.com/stretchr/testify/assert"
)

func TestHotStuffNode_MessageHandling(t *testing.T) {
	node := hotstuff.NewHotStuffNode("node1", []string{"node2", "node3"})

	msg := &hotstuff.ConsensusMessage{
		Type:           hotstuff.MessageTypeNewView,
		View:           1,
		SequenceNumber: 1,
		Digest:         "block-hash",
		NodeID:         "node2",
	}

	err := node.HandleMessage(msg)
	assert.NoError(t, err)

	// Verify message storage (indirectly via no error and coverage)
	// In a real test we might inspect internal state if exported or via helpers
}

func TestHotStuffNode_OldView(t *testing.T) {
	node := hotstuff.NewHotStuffNode("node1", []string{})
	node.View = 5

	msg := &hotstuff.ConsensusMessage{
		Type:   hotstuff.MessageTypePrepare,
		View:   2, // Old view
		NodeID: "node2",
	}

	err := node.HandleMessage(msg)
	assert.NoError(t, err)
	// Should be ignored
}
