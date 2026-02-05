package tpbft

import (
	"fmt"
	"sync"
)

// PBFTNode represents a node in the tPBFT consensus network
type PBFTNode struct {
	ID       string
	Peers    []string
	View     uint64
	Sequence uint64

	// Message logs: Sequence -> View -> Type -> NodeID -> Message
	MsgLog map[uint64]map[uint64]map[MessageType]map[string]*ConsensusMessage

	// State tracking
	Prepared  map[uint64]bool // Sequence -> bool
	Committed map[uint64]bool // Sequence -> bool

	// State
	mu sync.RWMutex
}

// NewPBFTNode creates a new PBFT node
func NewPBFTNode(id string, peers []string) *PBFTNode {
	return &PBFTNode{
		ID:        id,
		Peers:     peers,
		View:      0,
		Sequence:  0,
		MsgLog:    make(map[uint64]map[uint64]map[MessageType]map[string]*ConsensusMessage),
		Prepared:  make(map[uint64]bool),
		Committed: make(map[uint64]bool),
	}
}

// HandleMessage processes an incoming consensus message
func (n *PBFTNode) HandleMessage(msg *ConsensusMessage) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Basic validation
	if msg.View < n.View {
		return nil // Ignore old view messages
	}

	// Store message
	n.storeMessage(msg)

	switch msg.Type {
	case MessageTypePrePrepare:
		return n.handlePrePrepare(msg)
	case MessageTypePrepare:
		return n.handlePrepare(msg)
	case MessageTypeCommit:
		return n.handleCommit(msg)
	}
	return nil
}

func (n *PBFTNode) storeMessage(msg *ConsensusMessage) {
	if _, ok := n.MsgLog[msg.SequenceNumber]; !ok {
		n.MsgLog[msg.SequenceNumber] = make(map[uint64]map[MessageType]map[string]*ConsensusMessage)
	}
	if _, ok := n.MsgLog[msg.SequenceNumber][msg.View]; !ok {
		n.MsgLog[msg.SequenceNumber][msg.View] = make(map[MessageType]map[string]*ConsensusMessage)
	}
	if _, ok := n.MsgLog[msg.SequenceNumber][msg.View][msg.Type]; !ok {
		n.MsgLog[msg.SequenceNumber][msg.View][msg.Type] = make(map[string]*ConsensusMessage)
	}
	n.MsgLog[msg.SequenceNumber][msg.View][msg.Type][msg.NodeID] = msg
}

func (n *PBFTNode) handlePrePrepare(msg *ConsensusMessage) error {
	// In a real implementation, we would verify the proposal here.
	// For now, we assume it's valid and broadcast a PREPARE message.
	// Note: The actual broadcast would happen via a callback or channel.
	// Here we just update state.

	fmt.Printf("Node %s received PrePrepare for Seq %d View %d\n", n.ID, msg.SequenceNumber, msg.View)
	return nil
}

func (n *PBFTNode) handlePrepare(msg *ConsensusMessage) error {
	votes := n.countVotes(msg.SequenceNumber, msg.View, MessageTypePrepare)
	quorum := n.getQuorum()

	if votes >= quorum {
		if !n.Prepared[msg.SequenceNumber] {
			n.Prepared[msg.SequenceNumber] = true
			fmt.Printf("Node %s PREPARED for Seq %d (Votes: %d)\n", n.ID, msg.SequenceNumber, votes)
			// Should broadcast COMMIT here
		}
	}
	return nil
}

func (n *PBFTNode) handleCommit(msg *ConsensusMessage) error {
	votes := n.countVotes(msg.SequenceNumber, msg.View, MessageTypeCommit)
	quorum := n.getQuorum()

	if votes >= quorum {
		if !n.Committed[msg.SequenceNumber] {
			n.Committed[msg.SequenceNumber] = true
			fmt.Printf("Node %s COMMITTED for Seq %d (Votes: %d)\n", n.ID, msg.SequenceNumber, votes)
			// Should Execute block here
		}
	}
	return nil
}

// Helper to count votes
func (n *PBFTNode) countVotes(seq, view uint64, msgType MessageType) int {
	if msgs, ok := n.MsgLog[seq][view][msgType]; ok {
		return len(msgs)
	}
	return 0
}

// getQuorum returns the required number of votes (2f + 1)
// For simplicity, we assume N = len(Peers) + 1 (self)
func (n *PBFTNode) getQuorum() int {
	total := len(n.Peers) + 1
	f := (total - 1) / 3
	return 2*f + 1
}
