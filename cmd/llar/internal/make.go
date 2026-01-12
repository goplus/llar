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
	"github.com/goplus/llar/internal/modules"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/pkgs/mod/module"
	"github.com/spf13/cobra"
)

var makeVerbose bool

var makeCmd = &cobra.Command{
	Use:   "make [module@version]",
	Short: "Build a module to FormulaDir",
	Long:  `Make downloads and builds a module to FormulaDir.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runMake,
}

func init() {
	makeCmd.Flags().BoolVarP(&makeVerbose, "verbose", "v", false, "Enable verbose build output")
	rootCmd.AddCommand(makeCmd)
}

func runMake(cmd *cobra.Command, args []string) error {
	modID, version := parseModuleArg(args[0])

	ctx := context.Background()

	formulaRepo, err := vcs.NewRepo("github.com/MeteorsLiu/llarmvp-formula")
	if err != nil {
		return err
	}
	// Load packages using modload
	modules, err := modules.Load(ctx, module.Version{Path: modID, Version: version}, modules.Options{
		FormulaRepo: formulaRepo,
	})
	if err != nil {
		return fmt.Errorf("failed to load packages: %w", err)
	}

	matrix := formula.Matrix{
		Require: map[string][]string{
			"os":   {runtime.GOOS},
			"arch": {runtime.GOARCH},
		},
	}

	// Handle verbose output
	var savedStdout, savedStderr *os.File
	if !makeVerbose {
		// Redirect stdout/stderr for formulas
		for _, mod := range modules {
			mod.SetStdout(io.Discard)
			mod.SetStderr(io.Discard)
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

	mainModule := module.Version{Path: modID, Version: version}
	results, err := build.NewBuilder(matrix).Build(ctx, mainModule, modules)
	if err != nil {
		return fmt.Errorf("failed to build %s@%s: %w", modID, version, err)
	}

	// Restore stdout/stderr before printing pkgconfig info
	if !makeVerbose {
		os.Stdout = savedStdout
		os.Stderr = savedStderr
	}

	// Print pkgconfig info for main module (first in formulas)
	if len(results) > 0 {
		main := results[0]
		printPkgConfigInfo(main, matrix)
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
func printPkgConfigInfo(main build.Result, matrix formula.Matrix) error {
	pkgconfigDir := filepath.Join(main.OutputDir, "lib", "pkgconfig")

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
