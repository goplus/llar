package internal

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/build"
	"github.com/goplus/llar/internal/env"
	"github.com/goplus/llar/internal/modload"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/pkgs/mod/module"
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

	// Load packages using modload
	formulas, err := modload.LoadPackages(ctx, module.Version{ID: modID, Version: version}, modload.PackageOpts{})
	if err != nil {
		return fmt.Errorf("failed to load packages: %w", err)
	}

	// Convert formulas to build targets
	targets := make([]build.BuildTarget, len(formulas))
	for i, f := range formulas {
		targets[i] = build.BuildTarget{
			Version: f.Version,
			Dir:     f.Dir,
			Project: f.Proj,
			OnBuild: f.OnBuild,
		}
	}

	matrix := formula.Matrix{
		Require: map[string][]string{
			"os":   {runtime.GOOS},
			"arch": {runtime.GOARCH},
		},
	}

	// Handle verbose output
	var savedStdout, savedStderr *os.File
	if !installVerbose {
		// Redirect stdout/stderr for formulas
		for i := range formulas {
			formulas[i].SetStdout(io.Discard)
			formulas[i].SetStderr(io.Discard)
		}

		// Also redirect os.Stdout/os.Stderr
		savedStdout = os.Stdout
		savedStderr = os.Stderr
		devNull, err := os.Open(os.DevNull)
		if err != nil {
			return fmt.Errorf("failed to open devnull: %w", err)
		}
		os.Stdout = devNull
		os.Stderr = devNull
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
	}

	mainModule := module.Version{ID: modID, Version: version}
	if err := builder.Build(ctx, mainModule, targets, matrix); err != nil {
		return fmt.Errorf("failed to build %s@%s: %w", modID, version, err)
	}

	// Restore stdout/stderr before printing pkgconfig info
	if !installVerbose {
		os.Stdout = savedStdout
		os.Stderr = savedStderr
	}

	// Print pkgconfig info for main module (first in formulas)
	if len(formulas) > 0 {
		main := formulas[0]
		printPkgConfigInfo(main.ID, main.Version.Version, matrix)
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
