package tpbft

import (
	"fmt"
	"sync"
)

// PBFTNode 表示 tPBFT 共识网络中的一个节点
type PBFTNode struct {
	ID       string
	Peers    []string
	View     uint64
	Sequence uint64

	// 消息日志：Sequence -> View -> Type -> NodeID -> Message
	MsgLog map[uint64]map[uint64]map[MessageType]map[string]*ConsensusMessage

	// 状态追踪
	Prepared  map[uint64]bool // 序列号 -> 是否已准备
	Committed map[uint64]bool // 序列号 -> 是否已提交

	// 互斥锁保护状态
	mu sync.RWMutex
}

// NewPBFTNode 创建一个新的 PBFT 节点实例
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

// HandleMessage 处理收到的共识消息
func (n *PBFTNode) HandleMessage(msg *ConsensusMessage) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// 基础校验
	if msg.View < n.View {
		return nil // 忽略旧视图的消息
	}

	// 存储消息到本地日志
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
	// 在真实实现中，这里需要对提案进行验证
	// 当前版本中直接认为提案有效，并广播 PREPARE 消息
	// 实际广播一般通过回调或通道完成，这里只更新本地状态

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
			// 此处理论上应广播 COMMIT 消息
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
			// 此处理论上应执行区块提交逻辑
		}
	}
	return nil
}

// countVotes 统计指定序列号和视图下某种类型消息的投票数
func (n *PBFTNode) countVotes(seq, view uint64, msgType MessageType) int {
	if msgs, ok := n.MsgLog[seq][view][msgType]; ok {
		return len(msgs)
	}
	return 0
}

// getQuorum 返回达到共识所需的最小投票数（2f + 1）
// 为简化计算，这里假设总节点数 N = len(Peers) + 1（包含自身）
func (n *PBFTNode) getQuorum() int {
	total := len(n.Peers) + 1
	f := (total - 1) / 3
	return 2*f + 1
}
