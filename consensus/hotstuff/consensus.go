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

// StakingKeeper 定义了共识模块从质押模块需要的接口能力
type StakingKeeper interface {
	GetValidatorByConsAddr(ctx context.Context, consAddr sdk.ConsAddress) (stakingtypes.Validator, error)
	GetAllValidators(ctx context.Context) ([]stakingtypes.Validator, error)
	TotalBondedTokens(ctx context.Context) (math.Int, error)
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
}

// HotStuffConsensus 实现了 HotStuff 共识引擎
type HotStuffConsensus struct {
	mu      sync.RWMutex
	running bool

	// HotStuff 共识特有的字段
	Node              *HotStuffNode
	TrustScorer       *TrustScorer
	ValidatorSelector *ValidatorSelector

	// Config 保存 HotStuff 共识相关配置
	viewTimeout time.Duration

	stakingKeeper StakingKeeper
}

// NewHotStuffConsensus 创建一个新的 HotStuff 共识实例
func NewHotStuffConsensus() *HotStuffConsensus {
	// 初始化信任评分器和验证人选择器
	scorer := NewTrustScorer()
	// 默认验证人列表为空，后续会根据实际状态更新
	selector := NewValidatorSelector([]string{"local-node"})

	// Node 使用空配置初始化，如果独立运行需要在外部设置具体参数
	node := NewHotStuffNode("local-node", []string{})
	node.ValidatorSelector = selector

	return &HotStuffConsensus{
		Node:              node,
		TrustScorer:       scorer,
		ValidatorSelector: selector,
		viewTimeout:       1000 * time.Millisecond,
	}
}

// SetStakingKeeper 设置质押模块依赖
func (h *HotStuffConsensus) SetStakingKeeper(k StakingKeeper) {
	h.stakingKeeper = k
}

// Start 启动共识引擎
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

// Stop 停止共识引擎
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
			// 处理视图超时
			h.newView()
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (h *HotStuffConsensus) newView() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 增加节点当前视图号
	h.Node.View++
	currentView := h.Node.View

	// 获取新视图对应的领导者
	leader := h.ValidatorSelector.GetLeader(currentView)

	fmt.Printf("Starting View %d, Leader: %s\n", currentView, leader)

	// 创建 NewView 消息
	// 在 HotStuff 中，副本会向下一任领导者发送 NEW-VIEW 消息
	msg := &ConsensusMessage{
		Type:          MessageTypeNewView,
		View:          currentView,
		NodeID:        h.Node.ID,
		Justification: h.Node.PrepareQC, // Send highest QC
	}

	// 在真实网络中，这里会向领导者发送消息
	// 当前实现通过自我处理或打印日志进行模拟
	if h.Node.ID == leader {
		// 当前节点为领导者，直接处理来自自身的 NewView 消息
		// 实际实现中应等待至少 N-f 个 NewView 消息
		h.Node.HandleMessage(msg)
	} else {
		// 向领导者发送消息（模拟）
		// network.Send(leader, msg)
		fmt.Printf("Sending NewView to leader %s\n", leader)
	}
}

// BeginBlock 实现 ConsensusEngine 接口，在区块开始时被调用
func (h *HotStuffConsensus) BeginBlock(ctx sdk.Context) {
	// 在区块开始时执行的逻辑，例如检查作恶证据等
}

// EndBlock 实现 ConsensusEngine 接口，在区块结束时被调用
func (h *HotStuffConsensus) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	// 在区块结束时执行的逻辑，例如更新验证人集合
	return nil
}
