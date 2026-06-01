package cometbft_light

import (
	"github.com/fffeng99999/hcap-consensus/engine/common"
	"github.com/fffeng99999/hcap-consensus/engine/consensus/cometbft"
)

// CometBFTLight 保留实验脚本中的历史名称 cometbft-light。
//
// 真实实现已经收敛到独立的 cometbft.CometBFT
type CometBFTLight = cometbft.CometBFT

// NewCometBFTLight 创建独立 CometBFT 状态机实例。
func NewCometBFTLight() *CometBFTLight {
	return cometbft.NewCometBFT()
}

// ComputeLatencyStats 计算延迟统计。
func ComputeLatencyStats(latencies []float64) (p50, p95, p99 float64) {
	return common.ComputeLatencyStats(latencies)
}
