package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	coreaddress "cosmossdk.io/core/address"
	"cosmossdk.io/log"
	txsigning "cosmossdk.io/x/tx/signing"
	cmtcfg "github.com/cometbft/cometbft/config"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/debug"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	"github.com/cosmos/cosmos-sdk/codec"
	sdkaddress "github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authcmd "github.com/cosmos/cosmos-sdk/x/auth/client/cli"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/spf13/cobra"
)

// NewRootCmd 创建 hcpd 的根命令
func NewRootCmd() *cobra.Command {
	// 1. 初始化链级配置（Bech32 前缀等）
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount("hcp", "hcppub")
	cfg.SetBech32PrefixForValidator("hcpvaloper", "hcpvaloperpub")
	cfg.SetBech32PrefixForConsensusNode("hcpvalcons", "hcpvalconspub")
	cfg.Seal()

	// 2. 初始化编码相关配置与 TxConfig
	signingOptions := txsigning.Options{
		AddressCodec:          sdkaddress.NewBech32Codec("hcp"),
		ValidatorAddressCodec: sdkaddress.NewBech32Codec("hcpvaloper"),
	}

	interfaceRegistry, err := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles:     proto.HybridResolver,
		SigningOptions: signingOptions,
	})
	if err != nil {
		panic(err)
	}

	// 将 FileResolver 设置为 interfaceRegistry，确保 TxConfig 能正确解析类型
	signingOptions.FileResolver = interfaceRegistry

	cryptocodec.RegisterInterfaces(interfaceRegistry)
	ModuleBasics.RegisterInterfaces(interfaceRegistry)
	appCodec := codec.NewProtoCodec(interfaceRegistry)
	legacyAmino := codec.NewLegacyAmino()

	txConfig, err := authtx.NewTxConfigWithOptions(appCodec, authtx.ConfigOptions{
		SigningOptions: &signingOptions,
	})
	if err != nil {
		panic(err)
	}

	if txConfig.SigningContext() == nil {
		panic("SigningContext is nil")
	}
	if txConfig.SigningContext().ValidatorAddressCodec() == nil {
		panic("ValidatorAddressCodec is nil")
	}

	// 3. 定义根命令 rootCmd
	rootCmd := &cobra.Command{
		Use:   "hcpd",
		Short: "HCP Consensus Node Daemon",
		Long:  "High-frequency trading blockchain consensus performance testing system",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// set the default command outputs
			cmd.SetOut(cmd.OutOrStdout())
			cmd.SetErr(cmd.ErrOrStderr())

			clientCtx := client.Context{}.
				WithCmdContext(cmd.Context()).
				WithCodec(appCodec).
				WithInterfaceRegistry(interfaceRegistry).
				WithTxConfig(txConfig).
				WithLegacyAmino(legacyAmino).
				WithInput(os.Stdin).
				WithAccountRetriever(authtypes.AccountRetriever{}).
				WithHomeDir(DefaultNodeHome).
				WithViper("HCP")

			clientCtx, err = client.ReadPersistentCommandFlags(clientCtx, cmd.Flags())
			if err != nil {
				return err
			}

			clientCtx, err = config.ReadFromClientConfig(clientCtx)
			if err != nil {
				return err
			}

			if err := client.SetCmdClientContextHandler(clientCtx, cmd); err != nil {
				return err
			}

			return server.InterceptConfigsPreRunHandler(cmd, "", nil, cmtcfg.DefaultConfig())
		},
	}

	// 4. 添加子命令
	rootCmd.AddCommand(
		genutilcli.InitCmd(ModuleBasics, DefaultNodeHome),
		CustomGenesisCoreCommand(txConfig, ModuleBasics, DefaultNodeHome),
		debug.Cmd(),
	)

	server.AddCommands(rootCmd, DefaultNodeHome, newApp, createHcpAppAndExport, addModuleInitFlags)

	// 添加密钥管理、辅助 RPC、查询、创世和交易等子命令
	rootCmd.AddCommand(
		queryCommand(),
		txCommand(),
		keys.Commands(),
	)

	return rootCmd
}

func addModuleInitFlags(startCmd *cobra.Command) {
	startCmd.Flags().String("consensus-engine", "tpbft", "共识引擎类型")
	startCmd.Flags().Int("votor-node-count", 4, "Votor 验证者数量")
	startCmd.Flags().Float64("votor-faulty-ratio", 0, "Votor 模拟故障比例(0-1)")
	startCmd.Flags().Float64("votor-fast-threshold", 0.8, "Votor 快速路径阈值(0-1)")
	startCmd.Flags().Float64("votor-slow-threshold", 0.6, "Votor 慢速路径阈值(0-1)")
	startCmd.Flags().Float64("votor-local-timeout-ms", 150, "Votor 本地超时(ms)")
	startCmd.Flags().Float64("votor-base-latency-ms", 0, "Votor 基础网络时延(ms)")
	startCmd.Flags().Int("merkle-tx-count", 1000, "并行Merkle每块交易数")
	startCmd.Flags().Int("merkle-tx-size", 512, "并行Merkle交易大小(Bytes)")
	startCmd.Flags().Int("merkle-k", 1, "并行Merkle子块数量")
	startCmd.Flags().Int("merkle-repeat", 30, "并行Merkle重复次数")
	startCmd.Flags().Int("hierarchical-node-count", 32, "分层共识节点总数")
	startCmd.Flags().Int("hierarchical-group-count", 0, "分层共识组数")
	startCmd.Flags().Int("hierarchical-group-size", 0, "分层共识每组节点数")
	startCmd.Flags().Int("hierarchical-message-bytes", 256, "分层共识单消息字节数")
	startCmd.Flags().Float64("hierarchical-base-latency-ms", 1, "分层共识阶段基准时延(ms)")
	startCmd.Flags().Float64("hierarchical-phase-weight-inner", 1, "分层共识组内阶段权重")
	startCmd.Flags().Float64("hierarchical-phase-weight-outer", 1, "分层共识组间阶段权重")
	startCmd.Flags().String("hierarchical-sig-algo", "bls", "分层TPBFT阈值签名算法")
	startCmd.Flags().Float64("hierarchical-sig-gen-ms", 0, "分层TPBFT单次签名耗时(ms)")
	startCmd.Flags().Float64("hierarchical-sig-verify-ms", 0, "分层TPBFT单次验签耗时(ms)")
	startCmd.Flags().Float64("hierarchical-sig-agg-ms", 0, "分层TPBFT聚合签名耗时(ms)")
	startCmd.Flags().String("hierarchical-outer-mode", "threshold", "分层TPBFT代表层签名模式")
	startCmd.Flags().String("hierarchical-outer-sig-algo", "", "分层TPBFT代表层签名算法")
	startCmd.Flags().Float64("hierarchical-outer-sig-gen-ms", 0, "分层TPBFT代表层单次签名耗时(ms)")
	startCmd.Flags().Float64("hierarchical-outer-sig-verify-ms", 0, "分层TPBFT代表层单次验签耗时(ms)")
	startCmd.Flags().Float64("hierarchical-outer-sig-agg-ms", 0, "分层TPBFT代表层聚合签名耗时(ms)")
	startCmd.Flags().Bool("hierarchical-batch-verify", false, "分层TPBFT批量验签开关")
	startCmd.Flags().Float64("hierarchical-batch-verify-gain", 1, "分层TPBFT批量验签加速比")
	startCmd.Flags().Float64("hierarchical-sig-gen-parallelism", 1, "分层TPBFT签名生成并行度")
	startCmd.Flags().Float64("hierarchical-sig-verify-parallelism", 1, "分层TPBFT验签并行度")
	startCmd.Flags().Float64("hierarchical-sig-agg-parallelism", 1, "分层TPBFT聚合并行度")
	startCmd.Flags().Int("hierarchical-batch-size", 200, "分层TPBFT每块交易批量")
}

func queryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "query",
		Aliases:                    []string{"q"},
		Short:                      "查询相关子命令",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		rpc.ValidatorCommand(),
		authcmd.QueryTxsByEventsCmd(),
		authcmd.QueryTxCmd(),
	)

	ModuleBasics.AddQueryCommands(cmd)
	cmd.PersistentFlags().String(flags.FlagChainID, "", "网络的链 ID")

	return cmd
}

func customCollectGenTxsCmd(genBalIterator banktypes.GenesisBalancesIterator, defaultNodeHome string, genTxValidator genutiltypes.MessageValidator, valAddrCodec coreaddress.Codec) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collect-gentxs",
		Short: "收集合约创世交易并输出 genesis.json 文件",
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			cdc := clientCtx.Codec

			serverCtx := server.GetServerContextFromCmd(cmd)
			config := serverCtx.Config
			config.SetRoot(clientCtx.HomeDir)

			nodeID, valPubKey, err := genutil.InitializeNodeValidatorFiles(config)
			if err != nil {
				return err
			}

			genDoc, err := genutiltypes.AppGenesisFromFile(config.GenesisFile())
			if err != nil {
				return err
			}

			genTxsDir := filepath.Join(clientCtx.HomeDir, "config", "gentx")
			initCfg := genutiltypes.NewInitConfig(genDoc.ChainID, genTxsDir, nodeID, valPubKey)

			fmt.Printf("DEBUG: customCollectGenTxsCmd: valAddrCodec: %v, Type: %T\n", valAddrCodec, valAddrCodec)
			if valAddrCodec == nil {
				panic("valAddrCodec is nil in customCollectGenTxsCmd")
			}

			_, err = genutil.GenAppStateFromConfig(cdc, clientCtx.TxConfig, config, initCfg, genDoc, genBalIterator, genTxValidator, valAddrCodec)
			return err
		},
	}
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	return cmd
}

func txCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "tx",
		Short:                      "交易相关子命令",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		authcmd.GetSignCommand(),
		authcmd.GetSignBatchCommand(),
		authcmd.GetMultiSignCommand(),
		authcmd.GetMultiSignBatchCmd(),
		authcmd.GetValidateSignaturesCommand(),
		authcmd.GetBroadcastCommand(),
		authcmd.GetEncodeCommand(),
		authcmd.GetDecodeCommand(),
	)

	ModuleBasics.AddTxCommands(cmd)
	cmd.PersistentFlags().String(flags.FlagChainID, "", "网络的链 ID")

	return cmd
}

func newApp(logger log.Logger, db dbm.DB, traceStore io.Writer, appOpts servertypes.AppOptions) servertypes.Application {
	return NewApp(logger, db, traceStore, true, appOpts)
}

func createHcpAppAndExport(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	height int64,
	forZeroHeight bool,
	jailAllowedAddrs []string,
	appOpts servertypes.AppOptions,
	modulesToExport []string,
) (servertypes.ExportedApp, error) {
	// 导出逻辑应当在此实现，当前仅返回空结构
	return servertypes.ExportedApp{}, nil
}

// CustomGenesisCoreCommand 复用 genutilcli.GenesisCoreCommand 的逻辑，并加入调试能力
func CustomGenesisCoreCommand(txConfig client.TxConfig, moduleBasics module.BasicManager, defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "genesis",
		Short:                      "Application's genesis-related subcommands",
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	gentxModule := moduleBasics[genutiltypes.ModuleName].(genutil.AppModuleBasic)

	validatorCodec := txConfig.SigningContext().ValidatorAddressCodec()
	if validatorCodec == nil {
		panic("CustomGenesisCoreCommand: ValidatorAddressCodec is nil!")
	}

	cmd.AddCommand(
		genutilcli.GenTxCmd(moduleBasics, txConfig, banktypes.GenesisBalancesIterator{}, defaultNodeHome, validatorCodec),
		genutilcli.MigrateGenesisCmd(genutilcli.MigrationMap),
		// genutilcli.CollectGenTxsCmd(banktypes.GenesisBalancesIterator{}, defaultNodeHome, gentxModule.GenTxValidator, address.NewBech32Codec("hcpvaloper")),
		customCollectGenTxsCmd(banktypes.GenesisBalancesIterator{}, defaultNodeHome, gentxModule.GenTxValidator, sdkaddress.NewBech32Codec("hcpvaloper")),
		genutilcli.ValidateGenesisCmd(moduleBasics),
		genutilcli.AddGenesisAccountCmd(defaultNodeHome, txConfig.SigningContext().AddressCodec()),
	)

	return cmd
}
