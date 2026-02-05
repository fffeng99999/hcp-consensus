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

// StakingKeeper defines the interface needed from the staking module
type StakingKeeper interface {
	GetValidatorByConsAddr(ctx context.Context, consAddr sdk.ConsAddress) (stakingtypes.Validator, error)
	GetAllValidators(ctx context.Context) ([]stakingtypes.Validator, error)
	TotalBondedTokens(ctx context.Context) (math.Int, error)
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
}

// TPBFT implements the Trust-enhanced PBFT consensus engine
type TPBFT struct {
	mu                sync.RWMutex
	TrustScorer       *TrustScorer
	ValidatorSelector *ValidatorSelector
	Node              *PBFTNode
	running           bool

	stakingKeeper StakingKeeper
}

// NewTPBFT creates a new tPBFT consensus instance
func NewTPBFT() *TPBFT {
	scorer := NewTrustScorer()
	// Default config: minTrust=0.6, maxValidators=100
	selector := NewValidatorSelector(scorer, 0.6, 100)

	// Node initialized with empty config, to be configured if running standalone
	node := NewPBFTNode("local-node", []string{})

	return &TPBFT{
		TrustScorer:       scorer,
		ValidatorSelector: selector,
		Node:              node,
	}
}

// SetStakingKeeper sets the staking keeper dependency
func (t *TPBFT) SetStakingKeeper(k StakingKeeper) {
	t.stakingKeeper = k
}

// Start starts the consensus engine
func (t *TPBFT) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return fmt.Errorf("tPBFT engine already running")
	}

	t.running = true
	// Start background tasks
	go t.consensusLoop()

	return nil
}

// consensusLoop handles background tasks
func (t *TPBFT) consensusLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for t.running {
		select {
		case <-ticker.C:
			// Periodic tasks (e.g. trust decay if needed)
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// Stop stops the consensus engine
func (t *TPBFT) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return nil
	}

	t.running = false
	return nil
}

// GetTrustScorer returns the trust scorer instance
func (t *TPBFT) GetTrustScorer() *TrustScorer {
	return t.TrustScorer
}

// GetValidatorSelector returns the validator selector instance
func (t *TPBFT) GetValidatorSelector() *ValidatorSelector {
	return t.ValidatorSelector
}

// HandleMessage handles incoming consensus messages (for standalone simulation)
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
	// 1. Verify Trust Score of proposer
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

// BeginBlock implements ConsensusEngine
func (t *TPBFT) BeginBlock(ctx sdk.Context) {
	if t.stakingKeeper == nil {
		return
	}

	proposerAddr := ctx.BlockHeader().ProposerAddress
	if len(proposerAddr) == 0 {
		return
	}

	// Calculate response time
	responseTime := 2 * time.Second // Placeholder

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

// EndBlock implements ConsensusEngine
func (t *TPBFT) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	if t.stakingKeeper == nil {
		return nil
	}

	// 1. Update trust scores for all validators
	t.updateTrustScores(ctx)

	// 2. Select next validators
	newValidators := t.selectNextValidators(ctx)

	// 3. Return validator updates if changed
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

	// Use ValidatorSelector logic
	// We need desired validator count. Let's use 100 or MaxValidators from params if available.
	// Here hardcoded to 100 for simplicity or use selector's max.
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
	// Simple check: compare with bonded validators
	// This might be expensive.
	// Optimization: compare hash or length + sample.
	// For now: assumes StakingKeeper manages the set, so if we return updates, we override.
	// But standard Staking EndBlock also updates.
	// We should only return updates if we want to *change* what Staking module did?
	// Actually, Staking module does its own updates.
	// If we want to *override*, we need to know what Staking module would do.
	// But usually, we just let Staking module handle it unless we have custom logic.
	// Our custom logic IS the ValidatorSelector.
	// So we should return the difference between "what we want" and "what is currently bonded".
	return true // Force update for now
}

func (t *TPBFT) toABCIValidators(validators []stakingtypes.Validator) []abci.ValidatorUpdate {
	var updates []abci.ValidatorUpdate
	for _, v := range validators {
		// Convert to ABCI validator update
		// We need to use proper codec
		// This is tricky without the codec.
		// Use helper if available.
		// v.ABCIValidatorUpdate(PowerReduction) is available in some versions.

		// Fallback: manually construct
		// We need pubkey.
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
