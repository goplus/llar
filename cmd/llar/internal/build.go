package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/build"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/pkgs/mod/versions"
	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the current module",
	Long:  `Build compiles the current module and its dependencies.`,
	RunE:  runBuild,
}

func init() {
	rootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	// Find versions.json in current directory
	versionsPath := filepath.Join(".", "versions.json")
	if _, err := os.Stat(versionsPath); os.IsNotExist(err) {
		return fmt.Errorf("versions.json not found in current directory, run 'llar init' first")
	}

	v, err := versions.Parse(versionsPath, nil)
	if err != nil {
		return fmt.Errorf("failed to parse versions.json: %w", err)
	}

	// Get the current version from the first entry in deps, or use "latest"
	var currentVersion string
	for ver := range v.Dependencies {
		currentVersion = ver
		break
	}
	if currentVersion == "" {
		currentVersion = "latest"
	}

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

	if err := builder.Build(ctx, v.ModuleID, currentVersion, matrix); err != nil {
		return fmt.Errorf("failed to build: %w", err)
	}

	fmt.Printf("Successfully built %s@%s\n", v.ModuleID, currentVersion)
	return nil
}
