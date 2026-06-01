package tests

import (
	"testing"

	"github.com/fffeng99999/hcap-consensus/consensus/hotstuff"
	"github.com/stretchr/testify/assert"
)

func hotStuffConfig() hotstuff.Config {
	return hotstuff.Config{
		NodeCount:          4,
		FaultyRatio:        0,
		ViewTimeoutMs:      5000,
		TimeoutExponent:    2.0,
		BaseLatencyMs:      1.0,
		JitterMs:           0.5,
		MessageBytes:       256,
		PipelineDepth:      3,
		EnableThresholdSig: false,
	}
}

func createTestNode(cfg hotstuff.Config) *hotstuff.HotStuffNode {
	scorer := hotstuff.NewTrustScorer(cfg)
	selector := hotstuff.NewValidatorSelector(cfg)
	return hotstuff.NewHotStuffNode(cfg, scorer, selector)
}

func TestHotStuffNode_Create(t *testing.T) {
	cfg := hotStuffConfig()
	cfg.NodeCount = 3
	node := createTestNode(cfg)

	assert.Equal(t, "node0", node.ID)
	assert.Equal(t, uint64(0), node.View)
	assert.Equal(t, 3, node.Total)
	assert.Equal(t, 1, node.F) // f = (3-1)/3 = 0, but min 1
}

func TestHotStuffNode_MessageHandling(t *testing.T) {
	cfg := hotStuffConfig()
	cfg.NodeCount = 3
	node := createTestNode(cfg)

	msg := &hotstuff.ConsensusMessage{
		Type:   hotstuff.MessageTypeNewView,
		View:   1,
		NodeID: "node1",
	}

	err := node.HandleMessage(msg)
	assert.NoError(t, err)
}

func TestHotStuffNode_OldViewIgnored(t *testing.T) {
	cfg := hotStuffConfig()
	cfg.NodeCount = 1
	node := createTestNode(cfg)
	node.View = 5

	msg := &hotstuff.ConsensusMessage{
		Type:   hotstuff.MessageTypePrepare,
		View:   2,
		NodeID: "node1",
	}

	err := node.HandleMessage(msg)
	assert.NoError(t, err)
}

func TestHotStuffNode_QuorumSize(t *testing.T) {
	cfg4 := hotStuffConfig()
	cfg4.NodeCount = 4
	node4 := createTestNode(cfg4)
	assert.Equal(t, 3, node4.QuorumSize())

	cfg7 := hotStuffConfig()
	cfg7.NodeCount = 7
	node7 := createTestNode(cfg7)
	assert.Equal(t, 5, node7.QuorumSize())
}

func TestHotStuffNode_SafetyCheck(t *testing.T) {
	cfg := hotStuffConfig()
	cfg.NodeCount = 3
	node := createTestNode(cfg)

	assert.True(t, node.IsSafe(nil, 1))

	qc := hotstuff.NewQC(5, "block-5")
	node.SetLockedQC(qc)

	assert.False(t, node.IsSafe(nil, 3))

	qc6 := hotstuff.NewQC(6, "block-6")
	assert.True(t, node.IsSafe(qc6, 6))
}

func TestHotStuffNode_PipelineFlow(t *testing.T) {
	cfg := hotStuffConfig()
	cfg.NodeCount = 4

	node0 := createTestNode(cfg)
	node1 := createTestNode(cfg)
	node2 := createTestNode(cfg)
	node3 := createTestNode(cfg)

	assert.Equal(t, 3, node0.QuorumSize())
	assert.Equal(t, 3, node1.QuorumSize())
	assert.Equal(t, 3, node2.QuorumSize())
	assert.Equal(t, 3, node3.QuorumSize())

	block := &hotstuff.Block{
		Hash:     "block-0",
		Height:   0,
		View:     0,
		Proposer: "node0",
	}

	prepareMsg := &hotstuff.ConsensusMessage{
		Type:   hotstuff.MessageTypePrepare,
		View:   0,
		Block:  block,
		NodeID: "node0",
	}

	err := node0.HandleMessage(prepareMsg)
	assert.NoError(t, err)
	err = node1.HandleMessage(prepareMsg)
	assert.NoError(t, err)
	err = node2.HandleMessage(prepareMsg)
	assert.NoError(t, err)
	err = node3.HandleMessage(prepareMsg)
	assert.NoError(t, err)
}

func TestHotStuffConsensus_CreateAndStart(t *testing.T) {
	cfg := hotStuffConfig()
	cfg.ViewTimeoutMs = 10000

	consensus := hotstuff.NewHotStuffConsensus(cfg)
	assert.NotNil(t, consensus)

	err := consensus.Start()
	assert.NoError(t, err)

	status := consensus.GetStatus()
	assert.Equal(t, true, status["running"])

	err = consensus.Stop()
	assert.NoError(t, err)
}

func TestHotStuffNode_ComputeMetrics(t *testing.T) {
	cfg := hotStuffConfig()
	cfg.NodeCount = 4
	cfg.BaseLatencyMs = 1.0
	cfg.JitterMs = 0.0

	node := createTestNode(cfg)
	metrics := node.ComputeMetrics(1)

	assert.Equal(t, 4, metrics.NodeCount)
	assert.Equal(t, 1, metrics.F)
	assert.Equal(t, 3, metrics.Quorum)
	assert.Greater(t, metrics.BlockTimeMs, 0.0)
}
