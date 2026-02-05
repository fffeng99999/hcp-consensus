package tpbft

import (
	"sync"
)

// PBFTNode represents a node in the tPBFT consensus network
type PBFTNode struct {
	ID       string
	Peers    []string
	View     uint64
	Sequence uint64
	
	// Message logs: Sequence -> Type -> NodeID -> Message
	MsgLog   map[uint64]map[MessageType]map[string]*ConsensusMessage
	
	// State
	mu sync.RWMutex
}

// NewPBFTNode creates a new PBFT node
func NewPBFTNode(id string, peers []string) *PBFTNode {
	return &PBFTNode{
		ID:       id,
		Peers:    peers,
		View:     0,
		Sequence: 0,
		MsgLog:   make(map[uint64]map[MessageType]map[string]*ConsensusMessage),
	}
}
