package ibft

import (
	"math"
	"math/rand"
	"sort"
	"strconv"
	"time"
)

type Metrics struct {
	BlockTimeMs   float64
	PrePrepareMs  float64
	PrepareMs     float64
	CommitMs      float64
	RoundChanges  int
	TotalMessages int
	CommBytes     float64
	NodeCount     int
	F             int
	Quorum        int
	FaultyRatio   float64
	TimeoutMs     float64
	BaseLatencyMs float64
}

type Node struct {
	cfg      Config
	scorer   *TrustScorer
	selector *ValidatorSelector
	rng      *rand.Rand

	lockedRound uint64
	lockedValue string
	hasLock     bool
}

func NewNode(cfg Config, scorer *TrustScorer, selector *ValidatorSelector) *Node {
	source := rand.NewSource(time.Now().UnixNano())
	return &Node{
		cfg:      cfg,
		scorer:   scorer,
		selector: selector,
		rng:      rand.New(source),
	}
}

func (n *Node) ComputeMetrics(height int64) Metrics {
	nodeCount := n.cfg.NodeCount
	f := (nodeCount - 1) / 3
	quorum := 2*f + 1

	var totalPre, totalPrepare, totalCommit float64
	roundChanges := 0
	totalMessages := 0

	value := n.valueForHeight(height)
	for round := 0; round < n.cfg.MaxRounds; round++ {
		_ = n.selector.GetLeader(uint64(round))
		proposerFaulty := n.rng.Float64() < n.cfg.FaultyRatio
		if proposerFaulty {
			roundChanges++
			totalMessages += n.roundChangeMessages(nodeCount)
			totalPre += n.cfg.TimeoutMs
			continue
		}

		proposedValue := value
		if n.hasLock && n.lockedValue != "" {
			proposedValue = n.lockedValue
		}

		preMs := n.phaseLatencyMs(nodeCount, quorum)
		if preMs > n.cfg.TimeoutMs {
			roundChanges++
			totalMessages += n.roundChangeMessages(nodeCount)
			totalPre += n.cfg.TimeoutMs
			continue
		}
		totalPre += preMs
		totalMessages += n.prePrepareMessages(nodeCount)

		prepareMs := n.phaseLatencyMs(nodeCount, quorum)
		if prepareMs > n.cfg.TimeoutMs {
			roundChanges++
			totalMessages += n.roundChangeMessages(nodeCount)
			totalPrepare += n.cfg.TimeoutMs
			continue
		}
		totalPrepare += prepareMs
		totalMessages += n.prepareMessages(nodeCount, f)

		commitMs := n.phaseLatencyMs(nodeCount, quorum)
		if commitMs > n.cfg.TimeoutMs {
			roundChanges++
			totalMessages += n.roundChangeMessages(nodeCount)
			totalCommit += n.cfg.TimeoutMs
			continue
		}
		totalCommit += commitMs
		totalMessages += n.commitMessages(nodeCount, f)

		n.hasLock = true
		n.lockedRound = uint64(round)
		n.lockedValue = proposedValue
		break
	}

	commBytes := float64(totalMessages * n.cfg.MessageBytes)
	return Metrics{
		BlockTimeMs:   totalPre + totalPrepare + totalCommit,
		PrePrepareMs:  totalPre,
		PrepareMs:     totalPrepare,
		CommitMs:      totalCommit,
		RoundChanges:  roundChanges,
		TotalMessages: totalMessages,
		CommBytes:     commBytes,
		NodeCount:     nodeCount,
		F:             f,
		Quorum:        quorum,
		FaultyRatio:   n.cfg.FaultyRatio,
		TimeoutMs:     n.cfg.TimeoutMs,
		BaseLatencyMs: n.cfg.BaseLatencyMs,
	}
}

func (n *Node) valueForHeight(height int64) string {
	return "block-" + strconv.FormatInt(height, 10)
}

func (n *Node) phaseLatencyMs(nodeCount int, quorum int) float64 {
	if nodeCount <= 1 {
		return 0
	}
	delays := make([]float64, nodeCount-1)
	for i := range delays {
		delays[i] = n.scorer.SampleNetworkDelayMs(n.rng)
	}
	sort.Float64s(delays)
	k := quorum - 1
	if k <= 0 {
		return 0
	}
	if k > len(delays) {
		k = len(delays)
	}
	base := delays[k-1]
	processing := 0.02 * math.Log2(float64(nodeCount)+1)
	return base + processing
}

func (n *Node) prePrepareMessages(nodeCount int) int {
	if nodeCount <= 1 {
		return 0
	}
	return nodeCount - 1
}

func (n *Node) prepareMessages(nodeCount int, f int) int {
	honest := nodeCount - f
	if honest < 0 {
		honest = 0
	}
	if nodeCount <= 1 {
		return 0
	}
	return honest * (nodeCount - 1)
}

func (n *Node) commitMessages(nodeCount int, f int) int {
	return n.prepareMessages(nodeCount, f)
}

func (n *Node) roundChangeMessages(nodeCount int) int {
	if nodeCount <= 1 {
		return 0
	}
	return nodeCount * (nodeCount - 1)
}
