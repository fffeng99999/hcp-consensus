package cometbft

import (
	"github.com/fffeng99999/hcp-consensus/engine/common"
	"github.com/fffeng99999/hcp-consensus/engine/consensus/pbft"
	"github.com/fffeng99999/hcp-consensus/engine/core"
)

// CometBFT 模拟 CometBFT 共识引擎（基于 PBFT + 流水线优化）
// CometBFT 是 Tendermint 的继任实现，核心特征：
// 1. 三阶段投票（类似 PBFT）
// 2. 流水线机制：允许新一轮在上一轮 PreCommit 阶段启动
// 3. 轮循式领导者选举（按区块高度轮询）
// 4. 通过 ExtraLatencyMs 模拟流水线带来的吞吐提升
type CometBFT struct {
	*pbft.PBFT
}

// NewCometBFT 创建 CometBFT 引擎实例
func NewCometBFT() *CometBFT {
	return &CometBFT{
		PBFT: pbft.NewPBFT(),
	}
}

// Init 初始化 CometBFT 引擎
func (c *CometBFT) Init(cfg *core.NodeConfig, network core.Network, txPool core.TxPool, exec core.Executor) error {
	// 先调用 PBFT 的初始化
	if err := c.PBFT.Init(cfg, network, txPool, exec); err != nil {
		return err
	}

	// CometBFT 配置：轮循式领导者 + 流水线
	nodeCount := len(cfg.AllNodes)
	c.PBFT.ValidatorSelector = func() []string { return cfg.AllNodes }
	c.PBFT.BroadcastTargets = func() []string { return cfg.AllNodes }

	// 流水线效果：签名验证可以部分并行化
	// 将额外延迟降低到 PBFT 的约 60%（模拟流水线带来的优化）
	baseLatency := (float64(nodeCount*(nodeCount-1)*2) * 0.18) / 4.0
	c.PBFT.ExtraLatencyMs = baseLatency * 0.65

	// 设置轮循式领导者（按高度选择）
	// 这在 PBFT 中通过 view 机制自然体现
	c.PBFT.OnCommit = func(block *core.Block) {
		// 流水线：提交后立即触发下一轮提议
		// 模拟效果通过延迟降低来体现
	}

	return nil
}

// ComputeLatencyStats 计算延迟统计（辅助函数）
func ComputeLatencyStats(latencies []float64) (p50, p95, p99 float64) {
	return common.ComputeLatencyStats(latencies)
}

// SetExtraLatency 设置额外延迟
func SetExtraLatency(c *CometBFT, ms float64) {
	c.PBFT.ExtraLatencyMs = ms
}

// maxInt 返回两个整数中的较大值
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// minInt 返回两个整数中的较小值
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
