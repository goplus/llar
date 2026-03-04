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
	"github.com/goplus/llar/internal/modules/modlocal"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/mod/module"
	"github.com/spf13/cobra"
)

var makeVerbose bool
var makeOutput string

// newRemoteStore creates the remote formula store. Overridable for testing.
var newRemoteStore = func() (repo.Store, error) {
	formulaDir, err := repo.DefaultDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get formula dir: %w", err)
	}
	formulaRepo, err := vcs.NewRepo("github.com/goplus/llarhub")
	if err != nil {
		return nil, err
	}
	return repo.New(formulaDir, formulaRepo), nil
}

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
	pattern, version, isLocal, err := parseModuleArg(args[0])
	if err != nil {
		return err
	}

	ctx := context.Background()

	// Resolve output path to absolute before build (build may change cwd)
	if makeOutput != "" {
		abs, err := filepath.Abs(makeOutput)
		if err != nil {
			return fmt.Errorf("failed to resolve output path: %w", err)
		}
		makeOutput = abs
	}

	matrix := formula.Matrix{
		Require: map[string][]string{
			"os":   {runtime.GOOS},
			"arch": {runtime.GOARCH},
		},
	}
	matrixStr := matrix.Combinations()[0]

	// Set up remote formula store (always needed for deps)
	remoteStore, err := newRemoteStore()
	if err != nil {
		return err
	}

	if !isLocal {
		return buildModule(ctx, remoteStore, pattern, version, matrixStr)
	}

	// Resolve local pattern
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	localMods, err := modlocal.Resolve(cwd, pattern)
	if err != nil {
		return err
	}

	// Build overlay: local modules from disk, deps from remote
	locals := make(map[string]string, len(localMods))
	for _, m := range localMods {
		locals[m.Path] = m.Dir
	}
	store := repo.NewOverlayStore(remoteStore, locals)

	for _, m := range localMods {
		ver := m.Version
		if ver == "" {
			ver = version // global @version from arg
		}
		if err := buildModule(ctx, store, m.Path, ver, matrixStr); err != nil {
			return err
		}
	}
	return nil
}

// buildModule loads and builds a single module.
func buildModule(ctx context.Context, store repo.Store, modPath, version, matrixStr string) error {
	mods, err := modules.Load(ctx, module.Version{Path: modPath, Version: version}, modules.Options{
		FormulaStore: store,
	})
	if err != nil {
		return fmt.Errorf("failed to load modules: %w", err)
	}

	// Handle verbose output
	var savedStdout, savedStderr *os.File
	if !makeVerbose {
		for _, mod := range mods {
			mod.SetStdout(io.Discard)
			mod.SetStderr(io.Discard)
		}

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

	if len(results) > 0 {
		main := results[len(results)-1]
		if main.Metadata != "" {
			fmt.Println(main.Metadata)
		}
		if makeOutput != "" {
			if err := outputResult(main.OutputDir, makeOutput); err != nil {
				return fmt.Errorf("failed to write output: %w", err)
			}
		}
	}

	return nil
}

// parseModuleArg parses a module argument, detecting local patterns (. or ./ prefix).
// Returns the pattern (with ./ prefix stripped), version, and whether the argument is local.
// Returns an error for invalid patterns like ".@version" (use "./@version" instead).
func parseModuleArg(arg string) (pattern, version string, isLocal bool, err error) {
	if strings.HasPrefix(arg, ".@") {
		return "", "", false, fmt.Errorf("invalid local pattern %q: use \"./@version\" instead of \".@version\"", arg)
	}

	if arg == "." || strings.HasPrefix(arg, "./") {
		isLocal = true
		pattern = strings.TrimPrefix(arg, "./")
		if arg == "." {
			pattern = ""
		}
	} else {
		pattern = arg
	}

	// TODO(MeteorsLiu): support wildcard patterns with "...".
	// For now, disable all "..." patterns in `llar make`.
	if strings.Contains(pattern, "...") {
		return "", "", false, fmt.Errorf("invalid pattern %q: \"...\" wildcard is not supported", arg)
	}

	for i := len(pattern) - 1; i >= 0; i-- {
		if pattern[i] == '@' {
			version = pattern[i+1:]
			pattern = pattern[:i]
			break
		}
	}

	// Parent directory references are unsupported for local ./... patterns.
	// Use "." to walk up and find the nearest versions.json.
	if isLocal {
		for _, part := range strings.Split(pattern, "/") {
			if part == ".." {
				return "", "", false, fmt.Errorf("invalid local pattern %q: \"..\" is not supported; use \".\" instead", arg)
			}
		}
	}
	return
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
