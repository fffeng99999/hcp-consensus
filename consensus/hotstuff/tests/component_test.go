package tests

import (
	"testing"

	"github.com/fffeng99999/hcp-consensus/consensus/hotstuff"
	"github.com/stretchr/testify/assert"
)

func defaultHotStuffConfig() hotstuff.Config {
	return hotstuff.Config{
		NodeCount:     4,
		FaultyRatio:   0,
		ViewTimeoutMs: 5000,
		BaseLatencyMs: 1.0,
		JitterMs:      0.5,
		MessageBytes:  256,
		PipelineDepth: 3,
	}
}

func TestValidatorSelector_GetLeader(t *testing.T) {
	cfg := defaultHotStuffConfig()
	cfg.NodeCount = 3
	vs := hotstuff.NewValidatorSelector(cfg)

	l1 := vs.GetLeader(0)
	l2 := vs.GetLeader(1)
	l3 := vs.GetLeader(2)
	l4 := vs.GetLeader(3)

	assert.Equal(t, "node0", l1)
	assert.Equal(t, "node1", l2)
	assert.Equal(t, "node2", l3)
	assert.Equal(t, "node0", l4)
}

func TestValidatorSelector_WeightedLeader(t *testing.T) {
	validators := []hotstuff.ValidatorEntry{
		{Address: "val1", Power: 3},
		{Address: "val2", Power: 2},
		{Address: "val3", Power: 1},
	}
	vs := hotstuff.NewWeightedSelector(validators)

	l0 := vs.GetLeader(0)
	l1 := vs.GetLeader(1)
	l2 := vs.GetLeader(2)
	l3 := vs.GetLeader(3)
	l4 := vs.GetLeader(4)
	l5 := vs.GetLeader(5)

	assert.Equal(t, "val1", l0)
	assert.Equal(t, "val1", l1)
	assert.Equal(t, "val1", l2)
	assert.Equal(t, "val2", l3)
	assert.Equal(t, "val2", l4)
	assert.Equal(t, "val3", l5)
}

func TestValidatorSelector_RoundRobin(t *testing.T) {
	cfg := defaultHotStuffConfig()
	cfg.NodeCount = 3
	vs := hotstuff.NewValidatorSelector(cfg)

	l0 := vs.GetLeaderByRoundRobin(0)
	l1 := vs.GetLeaderByRoundRobin(1)
	l2 := vs.GetLeaderByRoundRobin(2)
	l3 := vs.GetLeaderByRoundRobin(3)

	assert.Equal(t, "node0", l0)
	assert.Equal(t, "node1", l1)
	assert.Equal(t, "node2", l2)
	assert.Equal(t, "node0", l3)
}

func TestValidatorSelector_UpdateValidators(t *testing.T) {
	cfg := defaultHotStuffConfig()
	vs := hotstuff.NewValidatorSelector(cfg)
	vs.UpdateValidators([]string{"new1", "new2", "new3"})

	assert.Equal(t, 3, vs.ValidatorCount())
	l0 := vs.GetLeader(0)
	assert.Contains(t, []string{"new1", "new2", "new3"}, l0)
}

func TestValidatorSelector_TotalPower(t *testing.T) {
	validators := []hotstuff.ValidatorEntry{
		{Address: "val1", Power: 100},
		{Address: "val2", Power: 200},
		{Address: "val3", Power: 300},
	}
	vs := hotstuff.NewWeightedSelector(validators)

	assert.Equal(t, int64(600), vs.TotalPower())
}

func TestTrustScorer_Score(t *testing.T) {
	cfg := defaultHotStuffConfig()
	ts := hotstuff.NewTrustScorer(cfg)

	ts.RecordSuccess("val1")
	ts.RecordSuccess("val1")
	ts.RecordFailure("val1")

	score := ts.GetScore("val1")
	assert.Equal(t, 1.0, score)
}

func TestTrustScorer_Timeout(t *testing.T) {
	cfg := defaultHotStuffConfig()
	ts := hotstuff.NewTrustScorer(cfg)

	ts.RecordSuccess("val1")
	ts.RecordTimeout("val1")

	score := ts.GetScore("val1")
	assert.Equal(t, -1.0, score)
}

func TestTrustScorer_Latency(t *testing.T) {
	cfg := defaultHotStuffConfig()
	ts := hotstuff.NewTrustScorer(cfg)

	ts.RecordLatency("val1", 100.0)
	ts.RecordLatency("val1", 200.0)

	stats := ts.GetStats("val1")
	assert.NotNil(t, stats)
	assert.InDelta(t, 110.0, stats.AvgLatencyMs, 0.01)
}

func TestTrustScorer_TopValidators(t *testing.T) {
	cfg := defaultHotStuffConfig()
	ts := hotstuff.NewTrustScorer(cfg)

	ts.RecordSuccess("val1")
	ts.RecordSuccess("val1")
	ts.RecordSuccess("val2")
	ts.RecordFailure("val3")

	top := ts.GetTopValidators(2)
	assert.Len(t, top, 2)
	assert.Equal(t, "val1", top[0].ValidatorAddress)
	assert.Equal(t, 2.0, top[0].Score)
}

func TestQC_IsQuorum(t *testing.T) {
	qc := hotstuff.NewQC(1, "block-hash")

	qc.AddSignature("node1", []byte("sig1"))
	qc.AddSignature("node2", []byte("sig2"))

	assert.False(t, qc.IsQuorum(4, 1))

	qc.AddSignature("node3", []byte("sig3"))
	assert.True(t, qc.IsQuorum(4, 1))
}
