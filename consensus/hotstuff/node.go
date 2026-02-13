package hotstuff

import (
	"fmt"
	"sync"
)

// HotStuffNode represents a node in the HotStuff consensus network
type HotStuffNode struct {
	ID    string
	Peers []string
	View  uint64

	// Message logs
	MsgLog map[uint64]map[MessageType]map[string]*ConsensusMessage
	// Vote logs: View -> Phase -> NodeID -> Vote
	VoteLog map[uint64]map[MessageType]map[string]*VoteMessage

	// State tracking
	LockedQC  *QuorumCertificate // The highest QC for which a node has voted
	PrepareQC *QuorumCertificate // The highest QC that has been prepared

	// Helpers
	ValidatorSelector *ValidatorSelector

	// State
	mu sync.RWMutex
}

// NewHotStuffNode creates a new HotStuff node
func NewHotStuffNode(id string, peers []string) *HotStuffNode {
	return &HotStuffNode{
		ID:      id,
		Peers:   peers,
		View:    0,
		MsgLog:  make(map[uint64]map[MessageType]map[string]*ConsensusMessage),
		VoteLog: make(map[uint64]map[MessageType]map[string]*VoteMessage),
	}
}

// HandleMessage processes an incoming consensus message
func (n *HotStuffNode) HandleMessage(msg *ConsensusMessage) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Basic validation
	if msg.View < n.View {
		return nil // Ignore old view messages
	}

	// Store message
	n.storeMessage(msg)

	switch msg.Type {
	case MessageTypeNewView:
		return n.handleNewView(msg)
	case MessageTypePrepare:
		return n.handlePrepare(msg)
	case MessageTypePreCommit:
		return n.handlePreCommit(msg)
	case MessageTypeCommit:
		return n.handleCommit(msg)
	case MessageTypeDecide:
		return n.handleDecide(msg)
	}
	return nil
}

// HandleVote processes an incoming vote message (Leader side)
func (n *HotStuffNode) HandleVote(vote *VoteMessage) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if vote.View < n.View {
		return nil
	}

	n.storeVote(vote)

	// Check quorum
	// For simplicity, let's assume quorum is 1 (myself) or simplistic logic
	// In real logic: if count(votes) >= 2f+1

	// Here we just trigger next phase if we have a vote from ourselves (since we are local-node)
	// Or simply proceed.

	return n.checkQuorumAndProceed(vote.View, vote.Type)
}

func (n *HotStuffNode) storeMessage(msg *ConsensusMessage) {
	if _, ok := n.MsgLog[msg.View]; !ok {
		n.MsgLog[msg.View] = make(map[MessageType]map[string]*ConsensusMessage)
	}
	if _, ok := n.MsgLog[msg.View][msg.Type]; !ok {
		n.MsgLog[msg.View][msg.Type] = make(map[string]*ConsensusMessage)
	}
	n.MsgLog[msg.View][msg.Type][msg.NodeID] = msg
}

func (n *HotStuffNode) storeVote(vote *VoteMessage) {
	if _, ok := n.VoteLog[vote.View]; !ok {
		n.VoteLog[vote.View] = make(map[MessageType]map[string]*VoteMessage)
	}
	if _, ok := n.VoteLog[vote.View][vote.Type]; !ok {
		n.VoteLog[vote.View][vote.Type] = make(map[string]*VoteMessage)
	}
	n.VoteLog[vote.View][vote.Type][vote.NodeID] = vote
}

func (n *HotStuffNode) handleNewView(msg *ConsensusMessage) error {
	// Leader logic: Collect NewView messages
	// Simplified: If I am leader, and I receive NewView (from myself or others),
	// I should eventually propose.

	fmt.Printf("Node %s received NewView for View %d from %s\n", n.ID, msg.View, msg.NodeID)

	// Assume we have enough NewView messages or just proceed for demo
	leader := n.getLeader(msg.View)
	if n.ID == leader {
		// Propose
		n.propose(msg.View)
	}
	return nil
}

func (n *HotStuffNode) propose(view uint64) {
	fmt.Printf("Node %s PROPOSING for View %d\n", n.ID, view)

	// Create Prepare message
	msg := &ConsensusMessage{
		Type:          MessageTypePrepare,
		View:          view,
		NodeID:        n.ID,
		Digest:        fmt.Sprintf("block-%d", view),
		Justification: n.PrepareQC, // Use high QC
	}

	n.broadcast(msg)

	// Handle my own proposal (as replica)
	go n.HandleMessage(msg)
}

func (n *HotStuffNode) handlePrepare(msg *ConsensusMessage) error {
	fmt.Printf("Node %s received Prepare for View %d from %s\n", n.ID, msg.View, msg.NodeID)

	// Check safety (extends LockedQC or higher view)
	safe := false
	if msg.Justification == nil {
		safe = true // Genesis
	} else if n.LockedQC == nil {
		safe = true
	} else if msg.Justification.View > n.LockedQC.View {
		safe = true
	}

	if safe {
		// Vote PREPARE
		vote := &VoteMessage{
			Type:      MessageTypePrepare,
			View:      msg.View,
			BlockHash: msg.Digest,
			NodeID:    n.ID,
		}
		leader := n.getLeader(msg.View)
		n.sendVote(vote, leader)
	}
	return nil
}

func (n *HotStuffNode) handlePreCommit(msg *ConsensusMessage) error {
	fmt.Printf("Node %s received PreCommit for View %d\n", n.ID, msg.View)

	// Update PrepareQC
	n.PrepareQC = msg.Justification

	// Vote PRECOMMIT
	vote := &VoteMessage{
		Type:      MessageTypePreCommit,
		View:      msg.View,
		BlockHash: msg.Digest,
		NodeID:    n.ID,
	}
	leader := n.getLeader(msg.View)
	n.sendVote(vote, leader)
	return nil
}

func (n *HotStuffNode) handleCommit(msg *ConsensusMessage) error {
	fmt.Printf("Node %s received Commit for View %d\n", n.ID, msg.View)

	// Update LockedQC
	n.LockedQC = msg.Justification

	// Vote COMMIT
	vote := &VoteMessage{
		Type:      MessageTypeCommit,
		View:      msg.View,
		BlockHash: msg.Digest,
		NodeID:    n.ID,
	}
	leader := n.getLeader(msg.View)
	n.sendVote(vote, leader)
	return nil
}

func (n *HotStuffNode) handleDecide(msg *ConsensusMessage) error {
	fmt.Printf("Node %s received Decide for View %d\n", n.ID, msg.View)
	// Execute block
	return nil
}

func (n *HotStuffNode) checkQuorumAndProceed(view uint64, phase MessageType) error {
	// Count votes
	votes := n.VoteLog[view][phase]
	if len(votes) >= 1 { // Threshold = 1 for local test
		// Form QC
		qc := &QuorumCertificate{
			View:      view,
			NodeID:    n.ID,
			BlockHash: "block-hash", // simplified
		}

		var nextMsg *ConsensusMessage

		switch phase {
		case MessageTypePrepare:
			nextMsg = &ConsensusMessage{
				Type:          MessageTypePreCommit,
				View:          view,
				NodeID:        n.ID,
				Justification: qc,
			}
		case MessageTypePreCommit:
			nextMsg = &ConsensusMessage{
				Type:          MessageTypeCommit,
				View:          view,
				NodeID:        n.ID,
				Justification: qc,
			}
		case MessageTypeCommit:
			nextMsg = &ConsensusMessage{
				Type:          MessageTypeDecide,
				View:          view,
				NodeID:        n.ID,
				Justification: qc,
			}
		default:
			return nil
		}

		if nextMsg != nil {
			fmt.Printf("Node %s forming QC and broadcasting %v\n", n.ID, nextMsg.Type)
			n.broadcast(nextMsg)
			go n.HandleMessage(nextMsg)
		}
	}
	return nil
}

func (n *HotStuffNode) getLeader(view uint64) string {
	if n.ValidatorSelector != nil {
		return n.ValidatorSelector.GetLeader(view)
	}
	return "local-node"
}

func (n *HotStuffNode) broadcast(msg *ConsensusMessage) {
	// In simulation, we print
	// fmt.Printf("BROADCAST: Type %v View %d\n", msg.Type, msg.View)
}

func (n *HotStuffNode) sendVote(vote *VoteMessage, leader string) {
	// If I am the leader, handle it
	if n.ID == leader {
		go n.HandleVote(vote)
	} else {
		fmt.Printf("SEND VOTE to %s\n", leader)
	}
}
