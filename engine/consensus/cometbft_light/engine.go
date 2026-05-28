package cometbft_light

import (
	"github.com/fffeng99999/hcp-consensus/engine/common"
	"github.com/fffeng99999/hcp-consensus/engine/consensus/pbft"
	"github.com/fffeng99999/hcp-consensus/engine/core"
)

// CometBFTLight 是 HCP 轻量共识框架中的 Tendermint/CometBFT-like BFT 实现。
// 它和 PBFT、HotStuff、Raft、tPBFT 运行在同一进程内 engine 框架中，
// 便于在同一 runner 和同一 SDK 执行路径下比较算法行为。
//
// 它不是官方 CometBFT 节点；官方工程基线由 hcpd/CometBFT runner 单独启动。
type CometBFTLight struct {
	*pbft.PBFT
}

// NewCometBFTLight 创建 CometBFTLight 引擎实例
func NewCometBFTLight() *CometBFTLight {
	return &CometBFTLight{
		PBFT: pbft.NewPBFT(),
	}
}

// Init 初始化 CometBFTLight 引擎
func (c *CometBFTLight) Init(cfg *core.NodeConfig, network core.Network, txPool core.TxPool, exec core.Executor) error {
	if err := c.PBFT.Init(cfg, network, txPool, exec); err != nil {
		return err
	}

	nodeCount := len(cfg.AllNodes)
	c.PBFT.ValidatorSelector = func() []string { return cfg.AllNodes }
	c.PBFT.BroadcastTargets = func() []string { return cfg.AllNodes }

	// CometBFT-light 保留 PBFT 风格的两阶段投票安全结构，
	// 同时用较低的每轮验证开销模拟 Tendermint 的流水线提案和投票聚合优化。
	baseLatencyMs := (float64(nodeCount*(nodeCount-1)*2) * 0.18) / 4.0
	c.PBFT.ExtraLatencyMs = baseLatencyMs * 0.65

	return nil
}

// ComputeLatencyStats 计算延迟统计（辅助函数）
func ComputeLatencyStats(latencies []float64) (p50, p95, p99 float64) {
	return common.ComputeLatencyStats(latencies)
}

// SetExtraLatency 设置额外延迟
func SetExtraLatency(c *CometBFTLight, ms float64) {
	c.PBFT.ExtraLatencyMs = ms
}
