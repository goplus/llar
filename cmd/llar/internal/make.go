package internal

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/build"
	"github.com/goplus/llar/internal/formula/repo"
	"github.com/goplus/llar/internal/modules"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/mod/module"
	"github.com/spf13/cobra"
)

var makeVerbose bool
var makeOutput string

var makeCmd = &cobra.Command{
	Use:   "make [module@version]",
	Short: "Build a module to FormulaDir",
	Long:  `Make downloads and builds a module to FormulaDir.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runMake,
}

func init() {
	makeCmd.Flags().BoolVarP(&makeVerbose, "verbose", "v", false, "Enable verbose build output")
	makeCmd.Flags().StringVarP(&makeOutput, "output", "o", "", "Output path (directory or .zip file)")
	rootCmd.AddCommand(makeCmd)
}

func runMake(cmd *cobra.Command, args []string) error {
	modPath, version := parseModuleArg(args[0])

	ctx := context.Background()

	// Set up formula store
	formulaDir, err := repo.DefaultDir()
	if err != nil {
		return fmt.Errorf("failed to get formula dir: %w", err)
	}
	formulaRepo, err := vcs.NewRepo("github.com/goplus/llarhub")
	if err != nil {
		return err
	}
	store := repo.New(formulaDir, formulaRepo)

	// Load modules
	mods, err := modules.Load(ctx, module.Version{Path: modPath, Version: version}, modules.Options{
		FormulaStore: store,
	})
	if err != nil {
		return fmt.Errorf("failed to load modules: %w", err)
	}

	// Resolve output path to absolute before build (build may change cwd)
	if makeOutput != "" {
		abs, err := filepath.Abs(makeOutput)
		if err != nil {
			return fmt.Errorf("failed to resolve output path: %w", err)
		}
		makeOutput = abs
	}

	// Handle verbose output
	var savedStdout, savedStderr *os.File
	if !makeVerbose {
		for _, mod := range mods {
			mod.SetStdout(io.Discard)
			mod.SetStderr(io.Discard)
		}

		// Redirect os.Stdout/os.Stderr so subprocess output (cmake, etc.) is also silenced
		savedStdout = os.Stdout
		savedStderr = os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
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

	matrix := formula.Matrix{
		Require: map[string][]string{
			"os":   {runtime.GOOS},
			"arch": {runtime.GOARCH},
		},
	}
	matrixStr := matrix.Combinations()[0]

	// When -o is specified, use a temp workspace so we don't pollute the cache
	buildOpts := build.Options{
		Store:     store,
		MatrixStr: matrixStr,
	}
	if makeOutput != "" {
		tmpDir, err := os.MkdirTemp("", "llar-make-*")
		if err != nil {
			return fmt.Errorf("failed to create temp workspace: %w", err)
		}
		defer os.RemoveAll(tmpDir)
		buildOpts.WorkspaceDir = tmpDir
	}

	builder, err := build.NewBuilder(buildOpts)
	if err != nil {
		return fmt.Errorf("failed to create builder: %w", err)
	}

	results, err := builder.Build(ctx, mods)
	if err != nil {
		return fmt.Errorf("failed to build %s@%s: %w", modPath, version, err)
	}

	// Restore stdout before printing results
	if !makeVerbose {
		os.Stdout = savedStdout
		os.Stderr = savedStderr
	}

	// Print metadata for main module (last in build order)
	if len(results) > 0 {
		main := results[len(results)-1]
		if main.Metadata != "" {
			fmt.Println(main.Metadata)
		}

		// Output build artifacts if -o specified
		if makeOutput != "" {
			if err := outputResult(main.OutputDir, makeOutput); err != nil {
				return fmt.Errorf("failed to write output: %w", err)
			}
		}
	}

	return nil
}

// parseModuleArg parses a module argument in the form "owner/repo@version" or "owner/repo".
func parseModuleArg(arg string) (modPath, version string) {
	for i := len(arg) - 1; i >= 0; i-- {
		if arg[i] == '@' {
			return arg[:i], arg[i+1:]
		}
	}
	return arg, ""
}

// outputResult writes the build output to dest.
// If dest ends with ".zip", creates a zip archive; otherwise copies the directory.
func outputResult(srcDir, dest string) error {
	if strings.HasSuffix(dest, ".zip") {
		return zipDir(srcDir, dest)
	}
	return os.CopyFS(dest, os.DirFS(srcDir))
}

// zipDir creates a zip archive at dest from the contents of srcDir.
func zipDir(srcDir, dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = rel
		header.Method = zip.Deflate

		writer, err := w.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(writer, file)
		return err
	})
}
