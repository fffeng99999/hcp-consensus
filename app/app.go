package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"cosmossdk.io/x/tx/signing"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	codec "github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/bank"
	bankcli "github.com/cosmos/cosmos-sdk/x/bank/client/cli"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/consensus"
	consensuskeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingcli "github.com/cosmos/cosmos-sdk/x/staking/client/cli"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/spf13/cobra"

	// 导入共识相关模块
	"github.com/fffeng99999/hcp-consensus/consensus/common"
	"github.com/fffeng99999/hcp-consensus/consensus/hierarchical"
	"github.com/fffeng99999/hcp-consensus/consensus/hierarchical_hotspot_tpbft"
	"github.com/fffeng99999/hcp-consensus/consensus/hierarchical_lightweight_tpbft"
	"github.com/fffeng99999/hcp-consensus/consensus/hierarchical_tpbft"
	"github.com/fffeng99999/hcp-consensus/consensus/hierarchical_tpbft_parallel_block"
	"github.com/fffeng99999/hcp-consensus/consensus/hotstuff"
	"github.com/fffeng99999/hcp-consensus/consensus/ibft"
	"github.com/fffeng99999/hcp-consensus/consensus/pow"
	"github.com/fffeng99999/hcp-consensus/consensus/raft"
	"github.com/fffeng99999/hcp-consensus/consensus/tpbft"
	"github.com/fffeng99999/hcp-consensus/consensus/tpbft_parallel"
	"github.com/fffeng99999/hcp-consensus/consensus/tpbft_parallel_block"
	"github.com/fffeng99999/hcp-consensus/consensus/votor"
)

// BankAppModuleBasic 包装 bank 模块的基础功能，用于自定义交易命令的地址编码器
type BankAppModuleBasic struct {
	bank.AppModuleBasic
}

// GetTxCmd 返回 bank 模块的交易命令，使用 hcp Bech32 前缀创建地址编码器
func (b BankAppModuleBasic) GetTxCmd() *cobra.Command {
	addrCodec := address.NewBech32Codec("hcp")
	return bankcli.NewTxCmd(addrCodec)
}

// StakingAppModuleBasic 包装 staking 模块的基础功能，用于自定义交易命令的地址编码器
type StakingAppModuleBasic struct {
	staking.AppModuleBasic
}

// GetTxCmd 返回 staking 模块的交易命令，使用 hcp 和 hcpvaloper Bech32 前缀创建地址编码器
func (b StakingAppModuleBasic) GetTxCmd() *cobra.Command {
	valAddrCodec := address.NewBech32Codec("hcpvaloper")
	addrCodec := address.NewBech32Codec("hcp")
	return stakingcli.NewTxCmd(valAddrCodec, addrCodec)
}

var (
	// DefaultNodeHome 应用守护进程使用的默认数据目录
	DefaultNodeHome string

	// ModuleBasics 定义了由 BasicManager 管理的基础模块，
	// 负责如编码注册、创世状态校验等与其他模块无强依赖的能力
	ModuleBasics = module.NewBasicManager(
		auth.AppModuleBasic{},
		BankAppModuleBasic{},
		StakingAppModuleBasic{},
		consensus.AppModuleBasic{},
		genutil.AppModuleBasic{GenTxValidator: genutiltypes.DefaultMessageValidator},
	)
)

// init 在包导入时执行，初始化默认节点主目录为当前用户目录下的 .hcp 文件夹
func init() {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}

	DefaultNodeHome = filepath.Join(userHomeDir, ".hcp")
}

// appName 定义应用名称常量
const appName = "hcpd"

// App 扩展了 ABCI 应用的基础实现，集成 Cosmos SDK 各模块与自定义共识引擎
type App struct {
	*baseapp.BaseApp

	cdc               *codec.LegacyAmino
	appCodec          codec.Codec
	interfaceRegistry codectypes.InterfaceRegistry
	txConfig          client.TxConfig

	// keys 保存各子存储（KVStore）的键
	keys map[string]*storetypes.KVStoreKey

	// keepers 各功能模块的 Keeper
	AccountKeeper   authkeeper.AccountKeeper
	BankKeeper      bankkeeper.Keeper
	StakingKeeper   *stakingkeeper.Keeper
	ConsensusKeeper consensuskeeper.Keeper

	// ModuleManager 管理所有模块的生命周期和路由
	ModuleManager *module.Manager

	// ConsensusEngine 抽象的共识引擎实现（Raft / HotStuff / tPBFT 等）
	ConsensusEngine common.ConsensusEngine
}

// NewApp 创建并返回一个完成初始化的 App 实例
func NewApp(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	loadLatest bool,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) *App {
	// 初始化接口注册表，配置地址编码器
	interfaceRegistry, err := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles: proto.HybridResolver,
		SigningOptions: signing.Options{
			AddressCodec:          address.NewBech32Codec("hcp"),
			ValidatorAddressCodec: address.NewBech32Codec("hcpvaloper"),
		},
	})
	if err != nil {
		panic(err)
	}
	appCodec := codec.NewProtoCodec(interfaceRegistry)
	legacyAmino := codec.NewLegacyAmino()
	txConfig := authtx.NewTxConfig(appCodec, authtx.DefaultSignModes)

	// 注册各种接口实现
	cryptocodec.RegisterInterfaces(interfaceRegistry)
	ModuleBasics.RegisterInterfaces(interfaceRegistry)

	// 确定链 ID（ChainID）
	var chainID string
	if v, ok := appOpts.Get("chain-id").(string); ok {
		chainID = v
	}
	if chainID == "" {
		// 尝试从 genesis 文件读取链 ID
		homeDir, _ := appOpts.Get("home").(string)
		if homeDir != "" {
			genesisPath := filepath.Join(homeDir, "config", "genesis.json")
			if content, err := os.ReadFile(genesisPath); err == nil {
				var genesis struct {
					ChainID string `json:"chain_id"`
				}
				if err := json.Unmarshal(content, &genesis); err == nil {
					chainID = genesis.ChainID
					fmt.Printf("DEBUG: Found ChainID in genesis: %s\n", chainID)
				}
			}
		}
	}

	if chainID != "" {
		baseAppOptions = append(baseAppOptions, baseapp.SetChainID(chainID))
	}

	// 创建 BaseApp 实例
	bApp := baseapp.NewBaseApp(appName, logger, db, txConfig.TxDecoder(), baseAppOptions...)
	bApp.SetCommitMultiStoreTracer(traceStore)
	bApp.SetInterfaceRegistry(interfaceRegistry)
	bApp.SetTxEncoder(txConfig.TxEncoder())

	// 创建各模块存储键
	keys := storetypes.NewKVStoreKeys(
		authtypes.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey,
		consensustypes.StoreKey,
	)

	// 读取共识引擎类型配置，默认为 tpbft
	engineType := "tpbft"
	if appOpts.Get("consensus-engine") != nil {
		if v, ok := appOpts.Get("consensus-engine").(string); ok {
			engineType = v
		}
	}

	// 以下为辅助函数，用于从 appOpts 中读取各类配置参数
	readInt := func(key string, fallback int) int {
		if appOpts.Get(key) == nil {
			return fallback
		}
		switch v := appOpts.Get(key).(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				return parsed
			}
		}
		return fallback
	}
	readFloat := func(key string, fallback float64) float64 {
		if appOpts == nil {
			return fallback
		}
		if appOpts.Get(key) == nil {
			return fallback
		}
		switch v := appOpts.Get(key).(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int64:
			return float64(v)
		case string:
			if parsed, err := strconv.ParseFloat(v, 64); err == nil {
				return parsed
			}
		}
		return fallback
	}
	readString := func(key string, fallback string) string {
		if appOpts == nil {
			return fallback
		}
		if appOpts.Get(key) == nil {
			return fallback
		}
		switch v := appOpts.Get(key).(type) {
		case string:
			if v != "" {
				return v
			}
		}
		return fallback
	}
	readBool := func(key string, fallback bool) bool {
		if appOpts == nil {
			return fallback
		}
		if appOpts.Get(key) == nil {
			return fallback
		}
		switch v := appOpts.Get(key).(type) {
		case bool:
			return v
		case string:
			if parsed, err := strconv.ParseBool(v); err == nil {
				return parsed
			}
		}
		return fallback
	}

	// 根据配置创建对应的共识引擎实例
	var consensusEngine common.ConsensusEngine
	switch engineType {
	case "pow":
		consensusEngine = pow.NewPoW(pow.Config{
			NodeCount:      readInt("pow-node-count", 4),
			Difficulty:     readInt("pow-difficulty", 12),
			TargetBlockMs:  readFloat("pow-target-block-ms", 12000),
			TxPerBlock:     readInt("pow-tx-per-block", 1000),
			OrphanBaseRate: readFloat("pow-orphan-base-rate", 0.01),
		})
	case "votor":
		consensusEngine = votor.NewVotor(votor.Config{
			NodeCount:      readInt("votor-node-count", 4),
			FaultyRatio:    readFloat("votor-faulty-ratio", 0.0),
			FastThreshold:  readFloat("votor-fast-threshold", 0.8),
			SlowThreshold:  readFloat("votor-slow-threshold", 0.6),
			LocalTimeoutMs: readFloat("votor-local-timeout-ms", 150),
			BaseLatencyMs:  readFloat("votor-base-latency-ms", 0),
		})
	case "ibft":
		consensusEngine = ibft.NewIBFT(ibft.Config{
			NodeCount:     readInt("ibft-node-count", 32),
			FaultyRatio:   readFloat("ibft-faulty-ratio", 0.0),
			BaseLatencyMs: readFloat("ibft-base-latency-ms", 1),
			JitterMs:      readFloat("ibft-jitter-ms", 50),
			TimeoutMs:     readFloat("ibft-timeout-ms", 150),
			MessageBytes:  readInt("ibft-message-bytes", 256),
			MaxRounds:     readInt("ibft-max-rounds", 8),
		})
	case "hierarchical":
		consensusEngine = hierarchical.NewHierarchicalConsensus(hierarchical.Config{
			NodeCount:        readInt("hierarchical-node-count", 32),
			GroupCount:       readInt("hierarchical-group-count", 0),
			GroupSize:        readInt("hierarchical-group-size", 0),
			MessageBytes:     readInt("hierarchical-message-bytes", 256),
			BaseLatencyMs:    readFloat("hierarchical-base-latency-ms", 1),
			PhaseWeightInner: readFloat("hierarchical-phase-weight-inner", 1),
			PhaseWeightOuter: readFloat("hierarchical-phase-weight-outer", 1),
		})
	case "hierarchical-tpbft":
		consensusEngine = hierarchical_tpbft.NewHierarchicalTPBFT(hierarchical_tpbft.Config{
			NodeCount:            readInt("hierarchical-node-count", 32),
			GroupCount:           readInt("hierarchical-group-count", 0),
			GroupSize:            readInt("hierarchical-group-size", 0),
			MessageBytes:         readInt("hierarchical-message-bytes", 256),
			BaseLatencyMs:        readFloat("hierarchical-base-latency-ms", 1),
			PhaseWeightInner:     readFloat("hierarchical-phase-weight-inner", 1),
			PhaseWeightOuter:     readFloat("hierarchical-phase-weight-outer", 1),
			SigAlgorithm:         readString("hierarchical-sig-algo", "bls"),
			SigGenMs:             readFloat("hierarchical-sig-gen-ms", 0),
			SigVerifyMs:          readFloat("hierarchical-sig-verify-ms", 0),
			SigAggMs:             readFloat("hierarchical-sig-agg-ms", 0),
			OuterSigMode:         readString("hierarchical-outer-mode", "threshold"),
			OuterSigAlgorithm:    readString("hierarchical-outer-sig-algo", ""),
			OuterSigGenMs:        readFloat("hierarchical-outer-sig-gen-ms", 0),
			OuterSigVerifyMs:     readFloat("hierarchical-outer-sig-verify-ms", 0),
			OuterSigAggMs:        readFloat("hierarchical-outer-sig-agg-ms", 0),
			BatchVerify:          readBool("hierarchical-batch-verify", false),
			BatchVerifyGain:      readFloat("hierarchical-batch-verify-gain", 1),
			SigGenParallelism:    readFloat("hierarchical-sig-gen-parallelism", 1),
			SigVerifyParallelism: readFloat("hierarchical-sig-verify-parallelism", 1),
			SigAggParallelism:    readFloat("hierarchical-sig-agg-parallelism", 1),
			BatchSize:            readInt("hierarchical-batch-size", 200),
		})
	case "raft":
		consensusEngine = raft.NewRaftConsensus(raft.Config{
			NodeCount:              readInt("raft-node-count", 4),
			ElectionTimeoutMs:      readFloat("raft-election-timeout-ms", 150),
			HeartbeatIntervalMs:    readFloat("raft-heartbeat-interval-ms", 50),
			ElectionTimeoutRangeMs: readFloat("raft-election-timeout-range-ms", 150),
			SnapshotDistance:       readInt("raft-snapshot-distance", 10000),
			MaxLogEntriesPerRPC:    readInt("raft-max-log-entries-per-rpc", 500),
			MessageBytes:           readInt("raft-message-bytes", 256),
			FaultyRatio:            readFloat("raft-faulty-ratio", 0),
			MaxValidators:          readInt("raft-max-validators", 100),
		})
	case "hotstuff":
		consensusEngine = hotstuff.NewHotStuffConsensus(hotstuff.Config{
			NodeCount:          readInt("hotstuff-node-count", 4),
			FaultyRatio:        readFloat("hotstuff-faulty-ratio", 0),
			ViewTimeoutMs:      readFloat("hotstuff-view-timeout-ms", 5000),
			TimeoutExponent:    readFloat("hotstuff-timeout-exponent", 2.0),
			BaseLatencyMs:      readFloat("hotstuff-base-latency-ms", 1),
			JitterMs:           readFloat("hotstuff-jitter-ms", 0.5),
			MessageBytes:       readInt("hotstuff-message-bytes", 256),
			PipelineDepth:      readInt("hotstuff-pipeline-depth", 3),
			EnableThresholdSig: readBool("hotstuff-enable-threshold-sig", false),
		})
	case "tpbft-parallel":
		consensusEngine = tpbft_parallel.NewTPBFTParallel(tpbft_parallel.Config{
			TxCount:   readInt("merkle-tx-count", 1000),
			TxSize:    readInt("merkle-tx-size", 512),
			SubBlockK: readInt("merkle-k", 1),
			Repeat:    readInt("merkle-repeat", 30),
		})
	case "tpbft-parallel-block":
		consensusEngine = tpbft_parallel_block.NewTPBFTParallelBlock(tpbft_parallel_block.Config{
			SubBlockK: readInt("merkle-k", 1),
		})
	case "noop":
		consensusEngine = noOpConsensusEngine{}
	case "hierarchical-lightweight-tpbft":
		consensusEngine = hierarchical_lightweight_tpbft.NewHierarchicalLightweightTPBFT(hierarchical_lightweight_tpbft.Config{
			NodeCount:            readInt("hierarchical-node-count", 32),
			GroupCount:           readInt("hierarchical-group-count", 0),
			GroupSize:            readInt("hierarchical-group-size", 0),
			MessageBytes:         readInt("hierarchical-message-bytes", 256),
			BaseLatencyMs:        readFloat("hierarchical-base-latency-ms", 1),
			PhaseWeightInner:     readFloat("hierarchical-phase-weight-inner", 1),
			PhaseWeightOuter:     readFloat("hierarchical-phase-weight-outer", 1),
			SigAlgorithm:         readString("hierarchical-sig-algo", "bls"),
			SigGenMs:             readFloat("hierarchical-sig-gen-ms", 0),
			SigVerifyMs:          readFloat("hierarchical-sig-verify-ms", 0),
			SigAggMs:             readFloat("hierarchical-sig-agg-ms", 0),
			OuterSigMode:         readString("hierarchical-outer-mode", "threshold"),
			OuterSigAlgorithm:    readString("hierarchical-outer-sig-algo", ""),
			OuterSigGenMs:        readFloat("hierarchical-outer-sig-gen-ms", 0),
			OuterSigVerifyMs:     readFloat("hierarchical-outer-sig-verify-ms", 0),
			OuterSigAggMs:        readFloat("hierarchical-outer-sig-agg-ms", 0),
			BatchVerify:          readBool("hierarchical-batch-verify", false),
			BatchVerifyGain:      readFloat("hierarchical-batch-verify-gain", 1),
			SigGenParallelism:    readFloat("hierarchical-sig-gen-parallelism", 1),
			SigVerifyParallelism: readFloat("hierarchical-sig-verify-parallelism", 1),
			SigAggParallelism:    readFloat("hierarchical-sig-agg-parallelism", 1),
			BatchSize:            readInt("hierarchical-batch-size", 200),
			SubConsensusType:     readString("sub-consensus", "pbft"),
			RaftHeartbeatMs:      readFloat("raft-heartbeat-ms", 50),
			RaftElectionMs:       readFloat("raft-election-ms", 200),
			FaultInject:          readBool("fault-inject", false),
			FaultAfterSec:        readInt("fault-after-sec", 60),
		})
	case "hierarchical-hotspot-tpbft":
		consensusEngine = hierarchical_hotspot_tpbft.NewHierarchicalHotspotTPBFT(hierarchical_hotspot_tpbft.Config{
			NodeCount:               readInt("hierarchical-node-count", 32),
			GroupCount:              readInt("hierarchical-group-count", 0),
			GroupSize:               readInt("hierarchical-group-size", 0),
			MessageBytes:            readInt("hierarchical-message-bytes", 256),
			BaseLatencyMs:           readFloat("hierarchical-base-latency-ms", 1),
			PhaseWeightInner:        readFloat("hierarchical-phase-weight-inner", 1),
			PhaseWeightOuter:        readFloat("hierarchical-phase-weight-outer", 1),
			SigAlgorithm:            readString("hierarchical-sig-algo", "bls"),
			SigGenMs:                readFloat("hierarchical-sig-gen-ms", 0),
			SigVerifyMs:             readFloat("hierarchical-sig-verify-ms", 0),
			SigAggMs:                readFloat("hierarchical-sig-agg-ms", 0),
			OuterSigMode:            readString("hierarchical-outer-mode", "threshold"),
			OuterSigAlgorithm:       readString("hierarchical-outer-sig-algo", ""),
			OuterSigGenMs:           readFloat("hierarchical-outer-sig-gen-ms", 0),
			OuterSigVerifyMs:        readFloat("hierarchical-outer-sig-verify-ms", 0),
			OuterSigAggMs:           readFloat("hierarchical-outer-sig-agg-ms", 0),
			BatchVerify:             readBool("hierarchical-batch-verify", false),
			BatchVerifyGain:         readFloat("hierarchical-batch-verify-gain", 1),
			SigGenParallelism:       readFloat("hierarchical-sig-gen-parallelism", 1),
			SigVerifyParallelism:    readFloat("hierarchical-sig-verify-parallelism", 1),
			SigAggParallelism:       readFloat("hierarchical-sig-agg-parallelism", 1),
			BatchSize:               readInt("hierarchical-batch-size", 200),
			GroupingStrategy:        readString("grouping-strategy", "random"),
			ZipfAlpha:               readFloat("zipf-alpha", 0),
			CrossGroupPenaltyFactor: readFloat("cross-group-penalty-factor", 0.5),
		})
	case "hierarchical-tpbft-parallel-block":
		consensusEngine = hierarchical_tpbft_parallel_block.NewHierarchicalTPBFTParallelBlock(hierarchical_tpbft_parallel_block.Config{
			NodeCount:            readInt("hierarchical-node-count", 32),
			GroupCount:           readInt("hierarchical-group-count", 0),
			GroupSize:            readInt("hierarchical-group-size", 0),
			MessageBytes:         readInt("hierarchical-message-bytes", 256),
			BaseLatencyMs:        readFloat("hierarchical-base-latency-ms", 1),
			PhaseWeightInner:     readFloat("hierarchical-phase-weight-inner", 1),
			PhaseWeightOuter:     readFloat("hierarchical-phase-weight-outer", 1),
			SigAlgorithm:         readString("hierarchical-sig-algo", "bls"),
			SigGenMs:             readFloat("hierarchical-sig-gen-ms", 0),
			SigVerifyMs:          readFloat("hierarchical-sig-verify-ms", 0),
			SigAggMs:             readFloat("hierarchical-sig-agg-ms", 0),
			OuterSigMode:         readString("hierarchical-outer-mode", "threshold"),
			OuterSigAlgorithm:    readString("hierarchical-outer-sig-algo", ""),
			OuterSigGenMs:        readFloat("hierarchical-outer-sig-gen-ms", 0),
			OuterSigVerifyMs:     readFloat("hierarchical-outer-sig-verify-ms", 0),
			OuterSigAggMs:        readFloat("hierarchical-outer-sig-agg-ms", 0),
			BatchVerify:          readBool("hierarchical-batch-verify", false),
			BatchVerifyGain:      readFloat("hierarchical-batch-verify-gain", 1),
			SigGenParallelism:    readFloat("hierarchical-sig-gen-parallelism", 1),
			SigVerifyParallelism: readFloat("hierarchical-sig-verify-parallelism", 1),
			SigAggParallelism:    readFloat("hierarchical-sig-agg-parallelism", 1),
			BatchSize:            readInt("hierarchical-batch-size", 200),
			SubBlockK:            readInt("merkle-k", 1),
		})
	case "tpbft":
		fallthrough
	default:
		consensusEngine = tpbft.NewTPBFT()
	}

	// 检查是否需要并行区块引擎，用于提案观察
	var parallelEngine *tpbft_parallel_block.TPBFTParallelBlock
	if engine, ok := consensusEngine.(*tpbft_parallel_block.TPBFTParallelBlock); ok {
		parallelEngine = engine
	}
	var hierarchicalParallelEngine *hierarchical_tpbft_parallel_block.HierarchicalTPBFTParallelBlock
	if engine, ok := consensusEngine.(*hierarchical_tpbft_parallel_block.HierarchicalTPBFTParallelBlock); ok {
		hierarchicalParallelEngine = engine
	}
	// 设置 PrepareProposal 钩子，用于在并行区块引擎中观察提案
	proposalHandler := baseapp.NewDefaultProposalHandler(bApp.Mempool(), bApp)
	prepareHandler := proposalHandler.PrepareProposalHandler()
	bApp.SetPrepareProposal(func(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
		resp, err := prepareHandler(ctx, req)
		if err == nil && parallelEngine != nil {
			parallelEngine.ObserveProposal(req.Height, resp.Txs)
		}
		if err == nil && hierarchicalParallelEngine != nil {
			hierarchicalParallelEngine.ObserveProposal(req.Height, resp.Txs)
		}
		return resp, err
	})
	// 设置 ProcessProposal 钩子，用于在并行区块引擎中观察提案
	processHandler := proposalHandler.ProcessProposalHandler()
	bApp.SetProcessProposal(func(ctx sdk.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
		if parallelEngine != nil {
			parallelEngine.ObserveProposal(req.Height, req.Txs)
		}
		if hierarchicalParallelEngine != nil {
			hierarchicalParallelEngine.ObserveProposal(req.Height, req.Txs)
		}
		return processHandler(ctx, req)
	})

	// 创建 App 实例
	app := &App{
		BaseApp:           bApp,
		cdc:               legacyAmino,
		appCodec:          appCodec,
		interfaceRegistry: interfaceRegistry,
		txConfig:          txConfig,
		keys:              keys,
		ConsensusEngine:   consensusEngine,
	}

	// 初始化各模块 Keeper
	app.AccountKeeper = authkeeper.NewAccountKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[authtypes.StoreKey]),
		authtypes.ProtoBaseAccount,
		map[string][]string{
			stakingtypes.BondedPoolName:    {authtypes.Burner, authtypes.Staking},
			stakingtypes.NotBondedPoolName: {authtypes.Burner, authtypes.Staking},
		},
		address.NewBech32Codec("hcp"),
		"hcp",
		authtypes.NewModuleAddress("gov").String(),
	)

	app.BankKeeper = bankkeeper.NewBaseKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[banktypes.StoreKey]),
		app.AccountKeeper,
		map[string]bool{
			stakingtypes.BondedPoolName:    true,
			stakingtypes.NotBondedPoolName: true,
		},
		authtypes.NewModuleAddress("gov").String(),
		logger,
	)

	app.ConsensusKeeper = consensuskeeper.NewKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[consensustypes.StoreKey]),
		authtypes.NewModuleAddress("gov").String(),
		runtime.EventService{},
	)

	// 设置 BaseApp 的参数存储
	app.BaseApp.SetParamStore(app.ConsensusKeeper.ParamsStore)

	app.StakingKeeper = stakingkeeper.NewKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[stakingtypes.StoreKey]),
		app.AccountKeeper,
		app.BankKeeper,
		authtypes.NewModuleAddress("gov").String(),
		address.NewBech32Codec("hcpvaloper"),
		address.NewBech32Codec("hcpvalcons"),
	)

	// 创建模块管理器
	app.ModuleManager = module.NewManager(
		auth.NewAppModule(appCodec, app.AccountKeeper, nil, nil),
		bank.NewAppModule(appCodec, app.BankKeeper, app.AccountKeeper, nil),
		consensus.NewAppModule(appCodec, app.ConsensusKeeper),
	)
	if !readBool("engine-sdk-minimal", false) {
		app.ModuleManager = module.NewManager(
			auth.NewAppModule(appCodec, app.AccountKeeper, nil, nil),
			bank.NewAppModule(appCodec, app.BankKeeper, app.AccountKeeper, nil),
			staking.NewAppModule(appCodec, app.StakingKeeper, app.AccountKeeper, app.BankKeeper, nil),
			consensus.NewAppModule(appCodec, app.ConsensusKeeper),
			genutil.NewAppModule(app.AccountKeeper, app.StakingKeeper, app, txConfig),
		)
	}

	// 设置创世模块初始化顺序
	if readBool("engine-sdk-minimal", false) {
		app.ModuleManager.SetOrderInitGenesis(
			authtypes.ModuleName,
			banktypes.ModuleName,
			consensustypes.ModuleName,
		)
	} else {
		app.ModuleManager.SetOrderInitGenesis(
			authtypes.ModuleName,
			banktypes.ModuleName,
			stakingtypes.ModuleName,
			consensustypes.ModuleName,
			genutiltypes.ModuleName,
		)
	}

	// 注册各模块的 gRPC / Msg 等服务
	app.ModuleManager.RegisterServices(module.NewConfigurator(app.appCodec, app.MsgServiceRouter(), app.GRPCQueryRouter()))

	// 设置初始化链钩子，根据配置决定是否跳过创世初始化
	if readBool("engine-sdk-skip-init-genesis", false) {
		app.SetInitChainer(func(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
			return &abci.ResponseInitChain{}, nil
		})
	} else {
		app.SetInitChainer(app.InitChainer)
	}

	// 注册 BeginBlocker 和 EndBlocker
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetEndBlocker(app.EndBlocker)

	// 挂载 KV 存储
	for _, key := range keys {
		app.MountStore(key, storetypes.StoreTypeIAVL)
	}

	// 加载最新版本
	if loadLatest {
		if err := app.LoadLatestVersion(); err != nil {
			panic(err)
		}
	}

	// 初始化共识引擎依赖
	if engine, ok := app.ConsensusEngine.(*tpbft.TPBFT); ok {
		engine.SetStakingKeeper(app.StakingKeeper)
	}
	if engine, ok := app.ConsensusEngine.(*tpbft_parallel.TPBFTParallel); ok {
		engine.SetStakingKeeper(app.StakingKeeper)
	}
	if engine, ok := app.ConsensusEngine.(*tpbft_parallel_block.TPBFTParallelBlock); ok {
		engine.SetStakingKeeper(app.StakingKeeper)
	}
	if engine, ok := app.ConsensusEngine.(*hierarchical_tpbft_parallel_block.HierarchicalTPBFTParallelBlock); ok {
		engine.SetStakingKeeper(app.StakingKeeper)
	}

	// 启动共识引擎
	if err := app.ConsensusEngine.Start(); err != nil {
		logger.Error("Failed to start consensus engine", "error", err)
	}

	return app
}

// InitChainer 在链初始化时处理应用级别的初始化逻辑
func (app *App) InitChainer(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	var genesisState map[string]json.RawMessage
	if err := json.Unmarshal(req.AppStateBytes, &genesisState); err != nil {
		panic(err)
	}
	return app.ModuleManager.InitGenesis(ctx, app.appCodec, genesisState)
}

// BeginBlocker 实现 BeginBlock 钩子逻辑
func (app *App) BeginBlocker(ctx sdk.Context) (sdk.BeginBlock, error) {
	// 1. 调用各标准模块的 BeginBlock 逻辑
	_, err := app.ModuleManager.BeginBlock(ctx)
	if err != nil {
		return sdk.BeginBlock{}, err
	}

	// 2. 触发共识引擎的 BeginBlock 钩子
	app.ConsensusEngine.BeginBlock(ctx)

	return sdk.BeginBlock{}, nil
}

// EndBlocker 实现 EndBlock 钩子逻辑
func (app *App) EndBlocker(ctx sdk.Context) (sdk.EndBlock, error) {
	// 1. 调用各标准模块的 EndBlock 逻辑
	res, err := app.ModuleManager.EndBlock(ctx)
	if err != nil {
		return sdk.EndBlock{}, err
	}

	// 2. 触发共识引擎的 EndBlock 钩子
	validatorUpdates := app.ConsensusEngine.EndBlock(ctx)

	// 3. 合并验证人更新（若存在）
	// 若共识引擎返回更新，则在原有基础上追加
	// 注意：质押模块的 EndBlock 也可能返回验证人更新
	if len(validatorUpdates) > 0 {
		res.ValidatorUpdates = append(res.ValidatorUpdates, validatorUpdates...)
	}

	return res, nil
}

// Commit 实现 ABCI Commit 钩子，用于记录底层存储写入延迟
func (app *App) Commit() (*abci.ResponseCommit, error) {
	start := time.Now()
	res, err := app.BaseApp.Commit()
	elapsed := time.Since(start)
	app.Logger().Info("rocksdb_write", "duration_ms", float64(elapsed.Milliseconds()))
	return res, err
}

// Name 返回应用名称
func (app *App) Name() string { return app.BaseApp.Name() }

// AppCodec 返回应用使用的编码器
func (app *App) AppCodec() codec.Codec {
	return app.appCodec
}

// InterfaceRegistry 返回应用使用的接口注册表
func (app *App) InterfaceRegistry() codectypes.InterfaceRegistry {
	return app.interfaceRegistry
}

// RegisterAPIRoutes 在给定的 API 服务上注册各模块的 HTTP 路由
func (app *App) RegisterAPIRoutes(apiSvr *api.Server, apiConfig config.APIConfig) {
	clientCtx := apiSvr.ClientCtx
	// 注册通过 grpc-gateway 暴露的交易路由
	authtx.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	// 注册通过 grpc-gateway 暴露的 Tendermint 查询路由（当前注释掉）
	// tmservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)

	// 为所有模块注册传统 REST 路由和 grpc-gateway 路由
	ModuleBasics.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
}

// TxConfig 返回交易配置
func (app *App) TxConfig() client.TxConfig {
	return app.txConfig
}

// RegisterNodeService 注册节点相关的 gRPC 服务（当前为空实现）
func (app *App) RegisterNodeService(clientCtx client.Context, cfg config.Config) {
	// TODO: 实现节点服务注册逻辑
}

// RegisterTendermintService 实现 Application.RegisterTendermintService 接口（当前为空实现）
func (app *App) RegisterTendermintService(clientCtx client.Context) {
	// TODO: 实现 Tendermint 服务注册逻辑
}

// RegisterTxService 实现 Application.RegisterTxService 接口
// 注册交易相关的 gRPC 服务，包括 MsgServiceRouter 和 QueryRouter
func (app *App) RegisterTxService(clientCtx client.Context) {
	authtx.RegisterTxService(app.GRPCQueryRouter(), clientCtx, app.BaseApp.Simulate, app.interfaceRegistry)
}

// noOpConsensusEngine 空操作共识引擎，用于测试或不需要共识的场景
type noOpConsensusEngine struct{}

// Start 启动空操作共识引擎
func (noOpConsensusEngine) Start() error { return nil }

// Stop 停止空操作共识引擎
func (noOpConsensusEngine) Stop() error { return nil }

// BeginBlock 空操作 BeginBlock
func (noOpConsensusEngine) BeginBlock(ctx sdk.Context) {}

// EndBlock 空操作 EndBlock，返回空的验证人更新
func (noOpConsensusEngine) EndBlock(ctx sdk.Context) []abci.ValidatorUpdate {
	return nil
}
