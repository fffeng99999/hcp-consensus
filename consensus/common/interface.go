package common

import (
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ConsensusEngine defines the interface for pluggable consensus modules
type ConsensusEngine interface {
	Start() error
	Stop() error
	
	// BeginBlock is called at the beginning of each block
	BeginBlock(ctx sdk.Context)
	
	// EndBlock is called at the end of each block and returns validator updates
	EndBlock(ctx sdk.Context) []abci.ValidatorUpdate
}
