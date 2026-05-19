package core

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// MessageType 定义共识消息类型
type MessageType uint8

const (
	MsgUnknown MessageType = iota
	// PBFT / tPBFT 消息
	MsgPrePrepare
	MsgPrepare
	MsgCommit
	MsgViewChange
	MsgNewView
	// HotStuff 消息
	MsgNewViewHS
	MsgPrepareHS
	MsgPreCommitHS
	MsgCommitHS
	MsgDecideHS
	// Raft 消息
	MsgAppendEntries
	MsgAppendEntriesRsp
	MsgRequestVote
	MsgRequestVoteRsp
	// 客户端消息
	MsgClientRequest
	MsgClientReply
	// 分层消息
	MsgInnerPropose
	MsgInnerPrepare
	MsgInnerCommit
	MsgOuterPropose
	MsgOuterPrepare
	MsgOuterCommit
)

func (m MessageType) String() string {
	switch m {
	case MsgPrePrepare:
		return "PrePrepare"
	case MsgPrepare:
		return "Prepare"
	case MsgCommit:
		return "Commit"
	case MsgViewChange:
		return "ViewChange"
	case MsgNewView:
		return "NewView"
	case MsgNewViewHS:
		return "NewViewHS"
	case MsgPrepareHS:
		return "PrepareHS"
	case MsgPreCommitHS:
		return "PreCommitHS"
	case MsgCommitHS:
		return "CommitHS"
	case MsgDecideHS:
		return "DecideHS"
	case MsgAppendEntries:
		return "AppendEntries"
	case MsgAppendEntriesRsp:
		return "AppendEntriesRsp"
	case MsgRequestVote:
		return "RequestVote"
	case MsgRequestVoteRsp:
		return "RequestVoteRsp"
	case MsgClientRequest:
		return "ClientRequest"
	case MsgClientReply:
		return "ClientReply"
	case MsgInnerPropose:
		return "InnerPropose"
	case MsgInnerPrepare:
		return "InnerPrepare"
	case MsgInnerCommit:
		return "InnerCommit"
	case MsgOuterPropose:
		return "OuterPropose"
	case MsgOuterPrepare:
		return "OuterPrepare"
	case MsgOuterCommit:
		return "OuterCommit"
	default:
		return "Unknown"
	}
}

// Tx 交易
type Tx struct {
	ID        string
	Payload   []byte
	Timestamp time.Time
	From      string
	Nonce     uint64
	// 以下为延迟测量字段
	SubmitTime    time.Time // 客户端提交时间
	ProposeTime   time.Time // 被提议进区块的时间
	CommitTime    time.Time // 区块提交时间（由各节点设置）
}

func NewTx(payload []byte, from string, nonce uint64) *Tx {
	h := sha256.Sum256(payload)
	return &Tx{
		ID:         hex.EncodeToString(h[:8]),
		Payload:    payload,
		Timestamp:  time.Now(),
		From:       from,
		Nonce:      nonce,
		SubmitTime: time.Time{},
	}
}

// Block 区块
type Block struct {
	Height    uint64
	Hash      string
	PrevHash  string
	Txs       []*Tx
	Proposer  string
	Timestamp time.Time
	QC        *QuorumCertificate
}

func (b *Block) ComputeHash() string {
	h := sha256.New()
	fmt.Fprintf(h, "%d|%s|%s|%d", b.Height, b.PrevHash, b.Proposer, len(b.Txs))
	for _, tx := range b.Txs {
		h.Write([]byte(tx.ID))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// QuorumCertificate 法定人数证书
type QuorumCertificate struct {
	BlockHash string
	Height    uint64
	View      uint64
	Sigs      map[string][]byte
}

// Message 共识网络消息
type Message struct {
	Type      MessageType
	From      string
	To        string // 空表示广播
	View      uint64
	Height    uint64
	Block     *Block
	BlockHash string
	QC        *QuorumCertificate
	Tx        *Tx
	Sigs      map[string][]byte
	Data      []byte
	Timestamp time.Time
}

// NodeConfig 节点配置
type NodeConfig struct {
	NodeID       string
	Addr         string
	AllNodes     []string
	PrivateKey   ed25519.PrivateKey
	PublicKeys   map[string]ed25519.PublicKey
	IsByzantine  bool
}

// ConsensusEngine 统一共识引擎接口
type ConsensusEngine interface {
	// Init 初始化引擎
	Init(cfg *NodeConfig, network Network, txPool TxPool, exec Executor) error
	// Start 启动引擎
	Start() error
	// Stop 停止引擎
	Stop() error
	// SubmitTx 客户端提交交易
	SubmitTx(tx *Tx) error
	// GetStatus 获取状态
	GetStatus() EngineStatus
}

// EngineStatus 引擎状态
type EngineStatus struct {
	NodeID          string
	Height          uint64
	View            uint64
	IsLeader        bool
	LeaderID        string
	PendingTxCount  int
	CommittedBlocks uint64
	TPS             float64
	AvgLatencyMs    float64
	P50LatencyMs    float64
	P95LatencyMs    float64
	P99LatencyMs    float64
	CpuPercent      float64
}

// Network 网络层抽象
type Network interface {
	// Send 发送点对点消息
	Send(msg *Message) error
	// Broadcast 广播消息
	Broadcast(msg *Message) error
	// RegisterHandler 注册消息处理器
	RegisterHandler(nodeID string, handler func(*Message))
	// SetLatency 设置网络延迟（ms）
	SetLatency(latencyMs float64)
	// SetBandwidth 设置带宽限制（Mbps）
	SetBandwidth(mbps float64)
	// GetMetrics 获取网络指标
	GetMetrics() NetworkMetrics
}

// NetworkMetrics 网络指标
type NetworkMetrics struct {
	TotalMessages   uint64
	TotalBytes      uint64
	BroadcastCount  uint64
	AvgLatencyMs    float64
}

// TxPool 交易池接口
type TxPool interface {
	AddTx(tx *Tx) error
	GetTxs(max int) []*Tx
	RemoveTxs(txIDs []string)
	PendingCount() int
}

// Executor 状态执行接口
type Executor interface {
	ExecuteBlock(block *Block) error
	GetStateHash() string
}

// Signer 签名工具
type Signer struct {
	PrivKey ed25519.PrivateKey
	PubKeys map[string]ed25519.PublicKey
}

func (s *Signer) Sign(data []byte) []byte {
	return ed25519.Sign(s.PrivKey, data)
}

func (s *Signer) Verify(nodeID string, data, sig []byte) bool {
	pk, ok := s.PubKeys[nodeID]
	if !ok {
		return false
	}
	return ed25519.Verify(pk, data, sig)
}
