package tests

import (
	"testing"
	"github.com/fffeng99999/hcp-consensus/consensus/hotstuff"
	"github.com/stretchr/testify/assert"
)

func TestValidatorSelector_GetLeader(t *testing.T) {
	validators := []string{"val1", "val2", "val3"}
	vs := hotstuff.NewValidatorSelector(validators)

	// Round robin check
	l1 := vs.GetLeader(0)
	l2 := vs.GetLeader(1)
	l3 := vs.GetLeader(2)
	l4 := vs.GetLeader(3)

	assert.Equal(t, "val1", l1)
	assert.Equal(t, "val2", l2)
	assert.Equal(t, "val3", l3)
	assert.Equal(t, "val1", l4)
}

func TestTrustScorer_Score(t *testing.T) {
	ts := hotstuff.NewTrustScorer()
	
	ts.RecordSuccess("val1")
	ts.RecordSuccess("val1")
	ts.RecordFailure("val1")

	score := ts.GetScore("val1")
	assert.Equal(t, 1.0, score) // 1 + 1 - 1 = 1
}
