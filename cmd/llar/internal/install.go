package internal

import (
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install [module@version]",
	Short: "Install a module",
	Long:  `Install downloads, builds, and installs a module.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	panic("TODO")
}
