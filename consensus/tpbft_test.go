package consensus

import (
	"testing"
	"time"
)

func TestNewTrustManager(t *testing.T) {
	tm := NewTrustManager()
	if tm == nil {
		t.Fatal("NewTrustManager returned nil")
	}
	if tm.scores == nil {
		t.Fatal("TrustManager scores map not initialized")
	}
}

func TestInitializeNode(t *testing.T) {
	tm := NewTrustManager()
	nodeID := "node1"
	initialEquity := int64(1000000)

	tm.InitializeNode(nodeID, initialEquity)

	score, exists := tm.GetTrustScore(nodeID)
	if !exists {
		t.Fatal("Node not initialized")
	}

	if score.TrustValue != 1.0 {
		t.Errorf("Expected initial trust value 1.0, got %f", score.TrustValue)
	}

	if score.EquityScore != initialEquity {
		t.Errorf("Expected equity %d, got %d", initialEquity, score.EquityScore)
	}
}

func TestRecordTransaction(t *testing.T) {
	tm := NewTrustManager()
	nodeID := "node1"
	tm.InitializeNode(nodeID, 1000000)

	// Record successful transaction
	tm.RecordTransaction(nodeID, true, 100)

	score, _ := tm.GetTrustScore(nodeID)
	if score.SuccessfulTxs != 1 {
		t.Errorf("Expected 1 successful tx, got %d", score.SuccessfulTxs)
	}

	// Record failed transaction
	tm.RecordTransaction(nodeID, false, 200)

	score, _ = tm.GetTrustScore(nodeID)
	if score.FailedTxs != 1 {
		t.Errorf("Expected 1 failed tx, got %d", score.FailedTxs)
	}
}

func TestUpdateTrustScore(t *testing.T) {
	tm := NewTrustManager()
	nodeID := "node1"
	tm.InitializeNode(nodeID, 1000000)

	// Record transactions
	for i := 0; i < 10; i++ {
		tm.RecordTransaction(nodeID, true, 100)
	}
	tm.RecordTransaction(nodeID, false, 500)

	score, _ := tm.GetTrustScore(nodeID)

	// Trust should be high (10 success, 1 fail)
	if score.TrustValue < 0.7 {
		t.Errorf("Expected trust value > 0.7, got %f", score.TrustValue)
	}
}

func TestSelectValidators(t *testing.T) {
	tm := NewTrustManager()

	// Initialize 5 nodes with different trust scores
	for i := 1; i <= 5; i++ {
		nodeID := string(rune('0' + i))
		tm.InitializeNode(nodeID, int64(i*100000))

		// Give different success rates
		for j := 0; j < i*2; j++ {
			tm.RecordTransaction(nodeID, true, 100)
		}
	}

	// Select top 3 validators
	selected := tm.SelectValidators(3)

	if len(selected) != 3 {
		t.Errorf("Expected 3 validators, got %d", len(selected))
	}
}

func TestConsensusConfig(t *testing.T) {
	// Test tPBFT config
	tpbft := DefaultTPBFTConfig()
	if tpbft.TimeoutCommit != 500*time.Millisecond {
		t.Errorf("Expected 500ms commit timeout, got %v", tpbft.TimeoutCommit)
	}

	// Test Raft config
	raft := RaftConfig()
	if raft.TimeoutCommit <= tpbft.TimeoutCommit {
		t.Error("Raft should have longer timeout than tPBFT")
	}

	// Test HotStuff config
	hotstuff := HotStuffConfig()
	if hotstuff.TimeoutCommit <= tpbft.TimeoutCommit {
		t.Error("HotStuff should have different timeout configuration")
	}
}

func BenchmarkRecordTransaction(b *testing.B) {
	tm := NewTrustManager()
	tm.InitializeNode("node1", 1000000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm.RecordTransaction("node1", true, 100)
	}
}

func BenchmarkSelectValidators(b *testing.B) {
	tm := NewTrustManager()

	// Initialize 100 nodes
	for i := 0; i < 100; i++ {
		nodeID := string(rune(i))
		tm.InitializeNode(nodeID, int64(i*10000))
		for j := 0; j < i; j++ {
			tm.RecordTransaction(nodeID, true, 100)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm.SelectValidators(7)
	}
}
