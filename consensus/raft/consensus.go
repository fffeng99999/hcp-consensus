package raft

import (
	"context"
	"fmt"
	"sync"
	"time"

	"cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
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

// RaftConsensus 实现了 Raft 共识引擎
type RaftConsensus struct {
	mu      sync.RWMutex
	running bool

	// Raft 共识特有的字段
	Node              *RaftNode
	TrustScorer       *TrustScorer
	ValidatorSelector *ValidatorSelector

	// Config 保存 Raft 共识的配置参数
	heartbeatInterval time.Duration

	stakingKeeper StakingKeeper
}

// NewRaftConsensus 创建一个新的 Raft 共识实例
func NewRaftConsensus() *RaftConsensus {
	// 初始化辅助组件
	scorer := NewTrustScorer()
	selector := NewValidatorSelector([]string{})

	// Node 使用空配置初始化，如果独立运行需要在外部设置具体参数
	node := NewRaftNode("local-node", []string{})

	return &RaftConsensus{
		Node:              node,
		TrustScorer:       scorer,
		ValidatorSelector: selector,
		heartbeatInterval: 50 * time.Millisecond,
	}
}

// SetStakingKeeper 设置质押模块依赖
func (r *RaftConsensus) SetStakingKeeper(k StakingKeeper) {
	r.stakingKeeper = k
}

// Start 启动共识引擎
func (r *RaftConsensus) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return fmt.Errorf("Raft engine already running")
	}

	r.running = true
	r.Node.Start()
	return nil
}

// Stop 停止共识引擎
func (r *RaftConsensus) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return nil
	}

	r.running = false
	r.Node.Stop()
	return nil
}

// BeginBlock 实现 ConsensusEngine 接口，在区块开始时被调用
func (r *RaftConsensus) BeginBlock(ctx sdk.Context) {
	// 在区块开始时执行的逻辑，例如领导人检查等
}

// EndBlock 实现 ConsensusEngine 接口，在区块结束时被调用
func (r *RaftConsensus) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	// 在区块结束时执行的逻辑，当前未做任何更新
	return nil
}
