package tpbft

import (
	"context"
	"fmt"
	"sync"
	"time"

	"cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	tmcrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	crypto "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// StakingKeeper 定义了共识模块从质押模块需要的接口能力
type StakingKeeper interface {
	GetValidatorByConsAddr(ctx context.Context, consAddr sdk.ConsAddress) (stakingtypes.Validator, error)
	GetAllValidators(ctx context.Context) ([]stakingtypes.Validator, error)
	TotalBondedTokens(ctx context.Context) (math.Int, error)
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
}

// TPBFT 实现了带有信任增强机制的 PBFT 共识引擎
type TPBFT struct {
	mu                sync.RWMutex
	TrustScorer       *TrustScorer
	ValidatorSelector *ValidatorSelector
	Node              *PBFTNode
	running           bool

	stakingKeeper StakingKeeper
}

// NewTPBFT 创建一个新的 tPBFT 共识实例
func NewTPBFT() *TPBFT {
	scorer := NewTrustScorer()
	// 默认配置：最小信任分数 minTrust=0.6，最大验证人数量 maxValidators=100
	selector := NewValidatorSelector(scorer, 0.6, 100)

	// Node 使用空配置初始化，如果独立运行需要在外部设置具体参数
	node := NewPBFTNode("local-node", []string{})

	return &TPBFT{
		TrustScorer:       scorer,
		ValidatorSelector: selector,
		Node:              node,
	}
}

// SetStakingKeeper 设置质押模块依赖
func (t *TPBFT) SetStakingKeeper(k StakingKeeper) {
	t.stakingKeeper = k
}

// Start 启动共识引擎
func (t *TPBFT) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return fmt.Errorf("tPBFT engine already running")
	}

	t.running = true
	// 启动共识相关的后台任务
	go t.consensusLoop()

	return nil
}

// consensusLoop 处理周期性后台任务
func (t *TPBFT) consensusLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for t.running {
		select {
		case <-ticker.C:
			// 周期性任务，例如需要时进行信任分衰减
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// Stop 停止共识引擎
func (t *TPBFT) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return nil
	}

	t.running = false
	return nil
}

// GetTrustScorer 返回信任评分组件实例
func (t *TPBFT) GetTrustScorer() *TrustScorer {
	return t.TrustScorer
}

// GetValidatorSelector 返回验证人选择器实例
func (t *TPBFT) GetValidatorSelector() *ValidatorSelector {
	return t.ValidatorSelector
}

// HandleMessage 处理收到的共识消息（独立仿真场景使用）
func (t *TPBFT) HandleMessage(msg *ConsensusMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch msg.Type {
	case MessageTypePrePrepare:
		return t.handlePrePrepare(msg)
	case MessageTypePrepare:
		return t.handlePrepare(msg)
	case MessageTypeCommit:
		return t.handleCommit(msg)
	}
	return nil
}

func (t *TPBFT) handlePrePrepare(msg *ConsensusMessage) error {
	// 1. 校验提案者的信任分数
	score := t.TrustScorer.GetScore(msg.NodeID)
	if score.TotalScore < t.ValidatorSelector.minTrustScore {
		return fmt.Errorf("proposer trust score too low: %f", score.TotalScore)
	}
	return nil
}

func (t *TPBFT) handlePrepare(msg *ConsensusMessage) error {
	return nil
}

func (t *TPBFT) handleCommit(msg *ConsensusMessage) error {
	return nil
}

// BeginBlock 实现 ConsensusEngine 接口的 BeginBlock 钩子
func (t *TPBFT) BeginBlock(ctx sdk.Context) {
	if t.stakingKeeper == nil {
		return
	}

	proposerAddr := ctx.BlockHeader().ProposerAddress
	if len(proposerAddr) == 0 {
		return
	}

	// 计算响应时间（当前为占位实现）
	responseTime := 2 * time.Second // 占位值

	val, err := t.stakingKeeper.GetValidatorByConsAddr(ctx, proposerAddr)
	if err != nil || val.OperatorAddress == "" {
		return
	}
	valAddr := val.OperatorAddress

	stake := val.GetTokens().ToLegacyDec().MustFloat64()
	totalStake := t.getTotalStake(ctx)

	t.TrustScorer.UpdateScore(
		valAddr,
		true, // Success (proposed a block)
		responseTime,
		stake,
		totalStake,
	)
}

// EndBlock 实现 ConsensusEngine 接口的 EndBlock 钩子
func (t *TPBFT) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	if t.stakingKeeper == nil {
		return nil
	}

	// 1. 为所有验证人更新信任分数
	t.updateTrustScores(ctx)

	// 2. 选择下一高度的验证人集合
	newValidators := t.selectNextValidators(ctx)

	// 3. 如有变化则返回验证人更新列表
	if t.validatorsChanged(ctx, newValidators) {
		return t.toABCIValidators(newValidators)
	}

	return nil
}

func (t *TPBFT) updateTrustScores(ctx sdk.Context) {
	voteInfos := ctx.VoteInfos()
	if len(voteInfos) == 0 {
		return
	}

	totalStake := t.getTotalStake(ctx)

	for _, vote := range voteInfos {
		val, err := t.stakingKeeper.GetValidatorByConsAddr(ctx, vote.Validator.Address)
		if err != nil || val.OperatorAddress == "" {
			continue
		}

		operatorAddr := val.OperatorAddress
		stake := val.GetTokens().ToLegacyDec().MustFloat64()
		signed := vote.BlockIdFlag == tmproto.BlockIDFlagCommit

		t.TrustScorer.UpdateScore(
			operatorAddr,
			signed,
			0,
			stake,
			totalStake,
		)
	}
}

func (t *TPBFT) selectNextValidators(ctx sdk.Context) []stakingtypes.Validator {
	allValidators, err := t.stakingKeeper.GetAllValidators(ctx)
	if err != nil {
		return nil
	}

	var allAddrs []string
	valMap := make(map[string]stakingtypes.Validator)
	for _, v := range allValidators {
		addr := v.OperatorAddress
		allAddrs = append(allAddrs, addr)
		valMap[addr] = v
	}

	// 使用 ValidatorSelector 的逻辑选择验证人
	// 我们需要一个目标验证人数量，这里直接使用 selector 中配置的 maxValidators
	count := t.ValidatorSelector.maxValidators
	if count > len(allValidators) {
		count = len(allValidators)
	}

	selectedAddrs := t.ValidatorSelector.SelectValidators(allAddrs, count)

	var selected []stakingtypes.Validator
	for _, addr := range selectedAddrs {
		if val, ok := valMap[addr]; ok {
			selected = append(selected, val)
		}
	}
	return selected
}

func (t *TPBFT) validatorsChanged(ctx sdk.Context, newValidators []stakingtypes.Validator) bool {
	// 简单实现：总是认为验证人集合发生了变化
	// 更严谨的做法应当是与当前已绑定的验证人集合进行比较
	// 可以通过比较哈希、长度或采样来优化性能
	// 当前实现假设 Staking 模块维护原始集合，若返回更新则表示由本模块覆盖
	return true // 当前强制返回需要更新
}

func (t *TPBFT) toABCIValidators(validators []stakingtypes.Validator) []abci.ValidatorUpdate {
	var updates []abci.ValidatorUpdate
	for _, v := range validators {
		// 转换为 ABCI 所需的验证人更新结构
		// 理论上应使用正确的编码工具（codec），部分版本中提供 v.ABCIValidatorUpdate 助手函数
		// 这里采用手动构造方式，并提取共识公钥
		pk, err := v.ConsPubKey()
		if err != nil {
			continue
		}

		tmPk, err := cryptocodecToTm(pk)
		if err != nil {
			continue
		}

		updates = append(updates, abci.ValidatorUpdate{
			PubKey: tmPk,
			Power:  v.GetConsensusPower(sdk.DefaultPowerReduction),
		})
	}
	return updates
}

func (t *TPBFT) getTotalStake(ctx sdk.Context) float64 {
	tokens, err := t.stakingKeeper.TotalBondedTokens(ctx)
	if err != nil {
		return 0
	}
	return tokens.ToLegacyDec().MustFloat64()
}

// 将 crypto 公钥（crypto.PubKey）转换为 Tendermint proto 公钥
func cryptocodecToTm(pk crypto.PubKey) (tmcrypto.PublicKey, error) {
	// Note: This is a simplified conversion.
	// In reality, we need to handle different key types (Ed25519, Secp256k1).
	// For now, assuming Ed25519 for simplicity as most cosmos chains use it.

	return tmcrypto.PublicKey{
		Sum: &tmcrypto.PublicKey_Ed25519{
			Ed25519: pk.Bytes(),
		},
	}, nil
}
