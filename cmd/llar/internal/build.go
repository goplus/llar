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

var buildVerbose bool

var buildCmd = &cobra.Command{
	Use:   "build [directory]",
	Short: "Build the current module",
	Long:  `Build compiles the current module and its dependencies.
If directory is specified, looks for versions.json and formula in that directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBuild,
}

func init() {
	buildCmd.Flags().BoolVarP(&buildVerbose, "verbose", "v", false, "Enable verbose build output")
	rootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	versionsPath := filepath.Join(dir, "versions.json")
	if _, err := os.Stat(versionsPath); os.IsNotExist(err) {
		return fmt.Errorf("versions.json not found in %s", dir)
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

	opts := build.BuildOptions{
		Verbose:  buildVerbose,
		LocalDir: dir,
	}
	buildList, err := builder.Build(ctx, v.ModuleID, currentVersion, matrix, opts)
	if err != nil {
		return fmt.Errorf("failed to build: %w", err)
	}

	// Print pkgconfig info for main module (first in buildList)
	if len(buildList) > 0 {
		main := buildList[0]
		printPkgConfigInfo(main.ID, main.Version, matrix)
	}

	return nil
}
