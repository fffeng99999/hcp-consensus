package cometbft_light

import (
	"github.com/fffeng99999/hcp-consensus/engine/common"
	"github.com/fffeng99999/hcp-consensus/engine/core"
	"github.com/fffeng99999/hcp-consensus/engine/pbft"
)

// CometBFTLight is an HCP engine implementation of a Tendermint/CometBFT-like
// BFT protocol. It intentionally stays inside the same in-process engine
// framework as PBFT, HotStuff, Raft, and tPBFT so experiments can compare the
// algorithmic behavior under one runner and one SDK execution path.
//
// It is not the official CometBFT node. Use the official hcpd/CometBFT runner
// for the external engineering baseline.
type CometBFTLight struct {
	*pbft.PBFT
}

func NewCometBFTLight() *CometBFTLight {
	return &CometBFTLight{
		PBFT: pbft.NewPBFT(),
	}
}

func (c *CometBFTLight) Init(cfg *core.NodeConfig, network core.Network, txPool core.TxPool, exec core.Executor) error {
	if err := c.PBFT.Init(cfg, network, txPool, exec); err != nil {
		return err
	}

	nodeCount := len(cfg.AllNodes)
	c.PBFT.ValidatorSelector = func() []string { return cfg.AllNodes }
	c.PBFT.BroadcastTargets = func() []string { return cfg.AllNodes }

	// CometBFT-light keeps the PBFT-style two-vote safety shape but models
	// Tendermint engineering improvements such as pipelined proposal handling
	// and vote aggregation with a lower per-round verification overhead.
	baseLatencyMs := (float64(nodeCount*(nodeCount-1)*2) * 0.18) / 4.0
	c.PBFT.ExtraLatencyMs = baseLatencyMs * 0.65

	return nil
}

func ComputeLatencyStats(latencies []float64) (p50, p95, p99 float64) {
	return common.ComputeLatencyStats(latencies)
}

func SetExtraLatency(c *CometBFTLight, ms float64) {
	c.PBFT.ExtraLatencyMs = ms
}
