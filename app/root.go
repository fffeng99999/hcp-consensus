package app

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// NewRootCmd creates a new root command for hcpd.
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "hcpd",
		Short: "HCP Consensus Node Daemon",
		Long:  "High-frequency trading blockchain consensus performance testing system",
	}

	initRootCmd(rootCmd)
	return rootCmd
}

func initRootCmd(rootCmd *cobra.Command) {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount("hcp", "hcppub")
	cfg.SetBech32PrefixForValidator("hcpvaloper", "hcpvaloperpub")
	cfg.SetBech32PrefixForConsensusNode("hcpvalcons", "hcpvalconspub")
	cfg.Seal()

	rootCmd.AddCommand(
		server.StartCmd(NewApp, DefaultNodeHome),
		server.ExportCmd(NewApp, DefaultNodeHome),
		config.Cmd(),
	)
}
