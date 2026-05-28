package tpbft

import (
	"sync"

	"github.com/fffeng99999/hcp-consensus/engine/common"
	"github.com/fffeng99999/hcp-consensus/engine/consensus/pbft"
	"github.com/fffeng99999/hcp-consensus/engine/core"
)

// TPBFT 是信任增强型 PBFT，在 PBFT 基础上加入信任评分与验证者筛选。
type TPBFT struct {
	mu sync.RWMutex

	pbftEngine    *pbft.PBFT
	trustScorer   *common.TrustScorer
	minTrust      float64
	maxValidators int
	selectedVals  []string
	totalStake    float64
	cfg           *core.NodeConfig
}

// NewTPBFT 创建 TPBFT 引擎实例。
func NewTPBFT(minTrust float64, maxValidators int) *TPBFT {
	if minTrust <= 0 {
		minTrust = 0.6
	}
	if maxValidators <= 0 {
		maxValidators = 100
	}
	return &TPBFT{
		pbftEngine:    pbft.NewPBFT(),
		trustScorer:   common.NewTrustScorer(common.DefaultTrustWeights()),
		minTrust:      minTrust,
		maxValidators: maxValidators,
	}
}

// Init 初始化 TPBFT 引擎。
func (t *TPBFT) Init(cfg *core.NodeConfig, network core.Network, txPool core.TxPool, exec core.Executor) error {
	t.cfg = cfg
	t.selectedVals = make([]string, len(cfg.AllNodes))
	copy(t.selectedVals, cfg.AllNodes)
	return t.pbftEngine.Init(cfg, network, txPool, exec)
}

// GetPBFT 返回底层 PBFT 引擎，供 factory 配置广播目标和额外延迟。
func (t *TPBFT) GetPBFT() *pbft.PBFT {
	return t.pbftEngine
}

// Start 启动 TPBFT 引擎。
func (t *TPBFT) Start() error {
	t.updateValidatorSet()
	return t.pbftEngine.Start()
}

// Stop 停止 TPBFT 引擎。
func (t *TPBFT) Stop() error {
	return t.pbftEngine.Stop()
}

// SubmitTx 提交交易到底层 PBFT 引擎。
func (t *TPBFT) SubmitTx(tx *core.Tx) error {
	return t.pbftEngine.SubmitTx(tx)
}

// GetStatus 获取底层 PBFT 引擎状态。
func (t *TPBFT) GetStatus() core.EngineStatus {
	return t.pbftEngine.GetStatus()
}

// updateValidatorSet 根据信任分数更新验证者集合。
func (t *TPBFT) updateValidatorSet() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.trustScorer == nil || t.cfg == nil {
		return
	}
	selected := t.trustScorer.SelectValidators(t.minTrust, t.maxValidators, t.cfg.AllNodes)
	if len(selected) == 0 {
		selected = make([]string, len(t.cfg.AllNodes))
		copy(selected, t.cfg.AllNodes)
	}
	t.selectedVals = selected
}

// RecordTrustRound 记录一轮共识的信任数据。
func (t *TPBFT) RecordTrustRound(nodeID string, success bool, responseMs float64, stake float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.trustScorer == nil {
		return
	}
	t.trustScorer.RecordRound(nodeID, success, responseMs, stake, t.totalStake)
}

// GetTrustScore 获取指定节点的信任分数。
func (t *TPBFT) GetTrustScore(nodeID string) *common.TrustScore {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.trustScorer == nil {
		return nil
	}
	return t.trustScorer.GetScore(nodeID)
}

// GetSelectedValidators 获取当前选中的验证者集合。
func (t *TPBFT) GetSelectedValidators() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]string, len(t.selectedVals))
	copy(result, t.selectedVals)
	return result
}
