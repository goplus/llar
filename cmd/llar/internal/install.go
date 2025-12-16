package internal

import (
	"context"
	"fmt"
	"runtime"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/build"
	"github.com/goplus/llar/internal/vcs"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install [module@version]",
	Short: "Install a module to FormulaDir",
	Long:  `Install downloads and builds a module, then installs the binary to FormulaDir.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	modID, version := parseModuleArg(args[0])

	ctx := context.Background()

	builder := build.NewBuilder()
	if err := builder.Init(ctx, vcs.NewGitVCS(), "https://github.com/aspect-build/llb-formulas"); err != nil {
		return fmt.Errorf("failed to init builder: %w", err)
	}

	matrix := formula.Matrix{
		Require: map[string][]string{
			"os":   {runtime.GOOS},
			"arch": {runtime.GOARCH},
		},
	}

	if err := builder.Build(ctx, modID, version, matrix); err != nil {
		return fmt.Errorf("failed to build %s@%s: %w", modID, version, err)
	}

	fmt.Printf("Successfully installed %s@%s\n", modID, version)
	return nil
}

// parseModuleArg parses a module argument in the form "owner/repo@version" or "owner/repo".
func parseModuleArg(arg string) (modID, version string) {
	for i := len(arg) - 1; i >= 0; i-- {
		if arg[i] == '@' {
			return arg[:i], arg[i+1:]
		}
	}
	return arg, ""
}
