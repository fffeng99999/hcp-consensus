package common

import (
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ConsensusEngine 定义可插拔共识模块需要实现的接口
type ConsensusEngine interface {
	Start() error
	Stop() error

	// BeginBlock 在每个区块开始时被调用
	BeginBlock(ctx sdk.Context)

	// EndBlock 在每个区块结束时被调用，并返回验证人更新列表
	EndBlock(ctx sdk.Context) []abci.ValidatorUpdate
}
