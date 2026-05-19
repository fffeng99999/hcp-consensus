package tpbft

import (
	"sync"

	"github.com/fffeng99999/hcp-consensus/engine/common"
	"github.com/fffeng99999/hcp-consensus/engine/core"
	"github.com/fffeng99999/hcp-consensus/engine/pbft"
)

// TPBFT 信任增强型PBFT
type TPBFT struct {
	mu sync.RWMutex

	pbftEngine     *pbft.PBFT // 基础PBFT引擎
	trustScorer    *common.TrustScorer
	minTrust       float64
	maxValidators  int
	selectedVals   []string // 当前选中的验证者集合
	totalStake     float64
	cfg            *core.NodeConfig
}

func NewTPBFT(minTrust float64, maxValidators int) *TPBFT {
	if minTrust <= 0 {
		minTrust = 0.6
	}
	if maxValidators <= 0 {
		maxValidators = 100
	}
	t := &TPBFT{
		pbftEngine:    pbft.NewPBFT(),
		trustScorer:   common.NewTrustScorer(common.DefaultTrustWeights()),
		minTrust:      minTrust,
		maxValidators: maxValidators,
	}
	return t
}

func (t *TPBFT) Init(cfg *core.NodeConfig, network core.Network, txPool core.TxPool, exec core.Executor) error {
	t.cfg = cfg
	err := t.pbftEngine.Init(cfg, network, txPool, exec)
	if err != nil {
		return err
	}
	// 初始选中所有节点
	t.selectedVals = make([]string, len(cfg.AllNodes))
	copy(t.selectedVals, cfg.AllNodes)
	return nil
}

func (t *TPBFT) GetPBFT() *pbft.PBFT {
	return t.pbftEngine
}

func (t *TPBFT) Start() error {
	// 启动前先更新验证者集合
	t.updateValidatorSet()
	return t.pbftEngine.Start()
}

func (t *TPBFT) Stop() error {
	return t.pbftEngine.Stop()
}

func (t *TPBFT) SubmitTx(tx *core.Tx) error {
	return t.pbftEngine.SubmitTx(tx)
}

func (t *TPBFT) GetStatus() core.EngineStatus {
	return t.pbftEngine.GetStatus()
}

func (t *TPBFT) updateValidatorSet() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.trustScorer == nil {
		return
	}
	allNodes := t.cfg.AllNodes
	selected := t.trustScorer.SelectValidators(t.minTrust, t.maxValidators, allNodes)
	if len(selected) == 0 {
		// 如果筛选结果为空，保留所有节点
		selected = make([]string, len(allNodes))
		copy(selected, allNodes)
	}
	t.selectedVals = selected
}

// RecordTrustRound 记录一轮共识的信任数据（供外部调用）
func (t *TPBFT) RecordTrustRound(nodeID string, success bool, responseMs float64, stake float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.trustScorer.RecordRound(nodeID, success, responseMs, stake, t.totalStake)
}

func (t *TPBFT) GetTrustScore(nodeID string) *common.TrustScore {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.trustScorer.GetScore(nodeID)
}

func (t *TPBFT) GetSelectedValidators() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]string, len(t.selectedVals))
	copy(result, t.selectedVals)
	return result
}
