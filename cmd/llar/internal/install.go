package internal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/build"
	"github.com/goplus/llar/internal/env"
	"github.com/goplus/llar/internal/vcs"
	"github.com/spf13/cobra"
)

var installVerbose bool

var installCmd = &cobra.Command{
	Use:   "install [module@version]",
	Short: "Install a module to FormulaDir",
	Long:  `Install downloads and builds a module, then installs the binary to FormulaDir.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runInstall,
}

func init() {
	installCmd.Flags().BoolVarP(&installVerbose, "verbose", "v", false, "Enable verbose build output")
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	modID, version := parseModuleArg(args[0])

	ctx := context.Background()

	builder := build.NewBuilder()
	if err := builder.Init(ctx, vcs.NewGitVCS(), "https://github.com/MeteorsLiu/llarmvp-formula"); err != nil {
		return fmt.Errorf("failed to init builder: %w", err)
	}

	matrix := formula.Matrix{
		Require: map[string][]string{
			"os":   {runtime.GOOS},
			"arch": {runtime.GOARCH},
		},
	}

	opts := build.BuildOptions{
		Verbose: installVerbose,
	}
	buildList, err := builder.Build(ctx, modID, version, matrix, opts)
	if err != nil {
		return fmt.Errorf("failed to build %s@%s: %w", modID, version, err)
	}

	// Print pkgconfig info for main module (first in buildList)
	if len(buildList) > 0 {
		main := buildList[0]
		printPkgConfigInfo(main.ID, main.Version, matrix)
	}

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

// printPkgConfigInfo uses pkg-config to print useful information about installed packages.
func printPkgConfigInfo(modID, version string, matrix formula.Matrix) error {
	formulaDir, err := env.FormulaDir()
	if err != nil {
		return err
	}

	buildDir := filepath.Join(formulaDir, modID, "build", version, matrix.String())
	pkgconfigDir := filepath.Join(buildDir, "lib", "pkgconfig")

	entries, err := os.ReadDir(pkgconfigDir)
	if err != nil {
		return err
	}

	// Find all .pc files
	var pkgNames []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".pc") {
			pkgNames = append(pkgNames, strings.TrimSuffix(entry.Name(), ".pc"))
		}
	}

	if len(pkgNames) == 0 {
		return nil
	}

	// Set PKG_CONFIG_PATH
	pkgConfigPath := os.Getenv("PKG_CONFIG_PATH")
	if pkgConfigPath != "" {
		pkgConfigPath = pkgconfigDir + ":" + pkgConfigPath
	} else {
		pkgConfigPath = pkgconfigDir
	}

	for _, pkgName := range pkgNames {
		cmd := exec.Command("pkg-config", "--libs", "--cflags", pkgName)
		cmd.Env = append(os.Environ(), "PKG_CONFIG_PATH="+pkgConfigPath)
		if out, err := cmd.Output(); err == nil {
			if result := strings.TrimSpace(string(out)); result != "" {
				fmt.Printf("%s: %s\n", pkgName, result)
			}
		}
	}

	return nil
}
