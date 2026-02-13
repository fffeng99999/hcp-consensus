package hotstuff

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

// StakingKeeper defines the interface needed from the staking module
type StakingKeeper interface {
	GetValidatorByConsAddr(ctx context.Context, consAddr sdk.ConsAddress) (stakingtypes.Validator, error)
	GetAllValidators(ctx context.Context) ([]stakingtypes.Validator, error)
	TotalBondedTokens(ctx context.Context) (math.Int, error)
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
}

// HotStuffConsensus implements the HotStuff consensus engine
type HotStuffConsensus struct {
	mu      sync.RWMutex
	running bool

	// HotStuff specific fields
	Node              *HotStuffNode
	TrustScorer       *TrustScorer
	ValidatorSelector *ValidatorSelector

	// Config
	viewTimeout time.Duration

	stakingKeeper StakingKeeper
}

// NewHotStuffConsensus creates a new HotStuff consensus instance
func NewHotStuffConsensus() *HotStuffConsensus {
	// Initialize trust scorer and validator selector
	scorer := NewTrustScorer()
	// Default validators list is empty, will be updated later
	selector := NewValidatorSelector([]string{"local-node"})

	// Node initialized with empty config, to be configured if running standalone
	node := NewHotStuffNode("local-node", []string{})
	node.ValidatorSelector = selector

	return &HotStuffConsensus{
		Node:              node,
		TrustScorer:       scorer,
		ValidatorSelector: selector,
		viewTimeout:       1000 * time.Millisecond,
	}
}

// SetStakingKeeper sets the staking keeper dependency
func (h *HotStuffConsensus) SetStakingKeeper(k StakingKeeper) {
	h.stakingKeeper = k
}

// Start starts the consensus engine
func (h *HotStuffConsensus) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.running {
		return fmt.Errorf("HotStuff engine already running")
	}

	h.running = true
	go h.runLoop()
	return nil
}

// Stop stops the consensus engine
func (h *HotStuffConsensus) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running {
		return nil
	}

	h.running = false
	return nil
}

func (h *HotStuffConsensus) runLoop() {
	ticker := time.NewTicker(h.viewTimeout)
	defer ticker.Stop()

	for h.running {
		select {
		case <-ticker.C:
			// Handle view timeout
			h.newView()
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (h *HotStuffConsensus) newView() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Increment view on the node
	h.Node.View++
	currentView := h.Node.View

	// Get leader for the new view
	leader := h.ValidatorSelector.GetLeader(currentView)

	fmt.Printf("Starting View %d, Leader: %s\n", currentView, leader)

	// Create NewView message
	// In HotStuff, replicas send NEW-VIEW to the next leader
	msg := &ConsensusMessage{
		Type:          MessageTypeNewView,
		View:          currentView,
		NodeID:        h.Node.ID,
		Justification: h.Node.PrepareQC, // Send highest QC
	}

	// In a real network, we would send this to the leader.
	// Here we simulate by handling it if we are the leader, or logging it.
	if h.Node.ID == leader {
		// I am the leader, handle the NewView message (from myself)
		// In reality, I would wait for N-f NewView messages.
		h.Node.HandleMessage(msg)
	} else {
		// Send to leader (mock)
		// network.Send(leader, msg)
		fmt.Printf("Sending NewView to leader %s\n", leader)
	}
}

// BeginBlock implements ConsensusEngine
func (h *HotStuffConsensus) BeginBlock(ctx sdk.Context) {
	// Logic to execute at the beginning of a block
	// e.g. checking for evidence of misbehavior
}

// EndBlock implements ConsensusEngine
func (h *HotStuffConsensus) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	// Logic to execute at the end of a block
	// e.g. updating validator set
	return nil
}
