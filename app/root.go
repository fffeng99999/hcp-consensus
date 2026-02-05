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
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/spf13/cobra"
)

// NewRootCmd creates a new root command for hcpd.
func NewRootCmd() *cobra.Command {
	// 1. Setup Config
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount("hcp", "hcppub")
	cfg.SetBech32PrefixForValidator("hcpvaloper", "hcpvaloperpub")
	cfg.SetBech32PrefixForConsensusNode("hcpvalcons", "hcpvalconspub")
	cfg.Seal()

	// 2. Setup Encoding/TxConfig
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

	// Set FileResolver to interfaceRegistry to ensure proper resolution in TxConfig
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

	// 3. Define rootCmd
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
				WithAccountRetriever(nil).
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

	// 4. Add subcommands
	rootCmd.AddCommand(
		genutilcli.InitCmd(ModuleBasics, DefaultNodeHome),
		CustomGenesisCoreCommand(txConfig, ModuleBasics, DefaultNodeHome),
		debug.Cmd(),
	)

	server.AddCommands(rootCmd, DefaultNodeHome, newApp, createHcpAppAndExport, addModuleInitFlags)

	// add keybase, auxiliary RPC, query, genesis, and tx child commands
	rootCmd.AddCommand(
		queryCommand(),
		txCommand(),
		keys.Commands(),
	)

	return rootCmd
}

func addModuleInitFlags(startCmd *cobra.Command) {
	// crisistypes.ModuleCdc = app.ModuleBasics.Cdc
}

func queryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "query",
		Aliases:                    []string{"q"},
		Short:                      "Querying subcommands",
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
	cmd.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")

	return cmd
}

func customCollectGenTxsCmd(genBalIterator banktypes.GenesisBalancesIterator, defaultNodeHome string, genTxValidator genutiltypes.MessageValidator, valAddrCodec coreaddress.Codec) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collect-gentxs",
		Short: "Collect genesis txs and output a genesis.json file",
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
		Short:                      "Transactions subcommands",
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
	cmd.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")

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
	// export logic would go here
	return servertypes.ExportedApp{}, nil
}

// CustomGenesisCoreCommand copies logic from genutilcli.GenesisCoreCommand but allows debugging
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
