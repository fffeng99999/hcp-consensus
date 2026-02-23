package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

	// Import consensus modules
	"github.com/fffeng99999/hcp-consensus/consensus/common"
	"github.com/fffeng99999/hcp-consensus/consensus/hotstuff"
	"github.com/fffeng99999/hcp-consensus/consensus/raft"
	"github.com/fffeng99999/hcp-consensus/consensus/tpbft"
)

type BankAppModuleBasic struct {
	bank.AppModuleBasic
}

func (b BankAppModuleBasic) GetTxCmd() *cobra.Command {
	addrCodec := address.NewBech32Codec("hcp")
	return bankcli.NewTxCmd(addrCodec)
}

type StakingAppModuleBasic struct {
	staking.AppModuleBasic
}

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

func init() {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}

	DefaultNodeHome = filepath.Join(userHomeDir, ".hcp")
}

const appName = "hcpd"

// App 扩展了 ABCI 应用的基础实现
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
		// Try to read from genesis file
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

	bApp := baseapp.NewBaseApp(appName, logger, db, txConfig.TxDecoder(), baseAppOptions...)
	bApp.SetCommitMultiStoreTracer(traceStore)
	bApp.SetInterfaceRegistry(interfaceRegistry)
	bApp.SetTxEncoder(txConfig.TxEncoder())

	keys := storetypes.NewKVStoreKeys(
		authtypes.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey,
		consensustypes.StoreKey,
	)

	// 根据配置决定使用的共识引擎类型
	engineType := "tpbft" // 默认使用 tPBFT
	if appOpts.Get("consensus-engine") != nil {
		if v, ok := appOpts.Get("consensus-engine").(string); ok {
			engineType = v
		}
	}

	var consensusEngine common.ConsensusEngine
	switch engineType {
	case "raft":
		consensusEngine = raft.NewRaftConsensus()
	case "hotstuff":
		consensusEngine = hotstuff.NewHotStuffConsensus()
	case "tpbft":
		fallthrough
	default:
		consensusEngine = tpbft.NewTPBFT()
	}

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
		staking.NewAppModule(appCodec, app.StakingKeeper, app.AccountKeeper, app.BankKeeper, nil),
		consensus.NewAppModule(appCodec, app.ConsensusKeeper),
		genutil.NewAppModule(app.AccountKeeper, app.StakingKeeper, app, txConfig),
	)

	app.ModuleManager.SetOrderInitGenesis(
		authtypes.ModuleName,
		banktypes.ModuleName,
		stakingtypes.ModuleName,
		consensustypes.ModuleName,
		genutiltypes.ModuleName,
	)

	// 注册各模块的 gRPC / Msg 等服务
	app.ModuleManager.RegisterServices(module.NewConfigurator(app.appCodec, app.MsgServiceRouter(), app.GRPCQueryRouter()))

	app.SetInitChainer(app.InitChainer)
	app.SetBeginBlocker(app.BeginBlocker) // 注册 BeginBlocker
	app.SetEndBlocker(app.EndBlocker)     // 注册 EndBlocker

	// 挂载 KV 存储
	for _, key := range keys {
		app.MountStore(key, storetypes.StoreTypeIAVL)
	}

	if loadLatest {
		if err := app.LoadLatestVersion(); err != nil {
			panic(err)
		}
	}

	// 初始化共识引擎依赖
	if engine, ok := app.ConsensusEngine.(*tpbft.TPBFT); ok {
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
