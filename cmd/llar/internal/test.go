package internal

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/build"
	"github.com/goplus/llar/internal/evaluator"
	"github.com/goplus/llar/internal/formula/repo"
	"github.com/goplus/llar/internal/modules"
	"github.com/goplus/llar/internal/modules/modlocal"
	"github.com/goplus/llar/internal/trace"
	"github.com/goplus/llar/mod/module"
	"github.com/spf13/cobra"
)

var testVerbose bool
var testAuto bool
var testTraceDump bool

var testCmd = &cobra.Command{
	Use:   "test [module@version]",
	Short: "Build a module and run onTest",
	Long:  `Test builds a module and executes onTest callbacks. Use --auto to evaluate which matrix combinations must run tests.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runTest,
}

func init() {
	testCmd.Flags().BoolVarP(&testVerbose, "verbose", "v", false, "Enable verbose build/test output")
	testCmd.Flags().BoolVar(&testAuto, "auto", false, "Automatically evaluate matrix combinations before running onTest")
	testCmd.Flags().BoolVar(&testTraceDump, "trace-dump", false, "Print intercepted build trace for each auto probe")
	rootCmd.AddCommand(testCmd)
}

func runTest(cmd *cobra.Command, args []string) error {
	pattern, version, isLocal, err := parseModuleArg(args[0])
	if err != nil {
		return err
	}
	if testAuto && isLocal {
		return fmt.Errorf("--auto does not support local patterns yet")
	}
	if testTraceDump && !testAuto {
		return fmt.Errorf("--trace-dump requires --auto")
	}

	ctx := context.Background()
	remoteStore, err := newRemoteStore()
	if err != nil {
		return err
	}

	if !isLocal {
		return testModule(ctx, remoteStore, pattern, version)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	localMods, err := modlocal.Resolve(cwd, pattern)
	if err != nil {
		return err
	}

	locals := make(map[string]string, len(localMods))
	for _, m := range localMods {
		locals[m.Path] = m.Dir
	}
	store := repo.NewOverlayStore(remoteStore, locals)
	for _, m := range localMods {
		ver := m.Version
		if ver == "" {
			ver = version
		}
		if err := testModule(ctx, store, m.Path, ver); err != nil {
			return err
		}
	}
	return nil
}

func testModule(ctx context.Context, store repo.Store, modPath, version string) error {
	combos := []string{defaultMatrixCombo()}
	if testAuto {
		matrix, err := loadModuleMatrix(ctx, store, modPath, version)
		if err != nil {
			return err
		}
		if matrix.CombinationCount() == 0 {
			matrix = defaultRuntimeMatrix()
		}
		moduleArg := modPath
		if version != "" {
			moduleArg = modPath + "@" + version
		}
		var trusted bool
		combos, trusted, err = evaluator.Watch(ctx, matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
			return collectBuildTrace(ctx, store, moduleArg, modPath, version, combo)
		})
		if err != nil {
			return fmt.Errorf("automatic matrix evaluation failed: %w", err)
		}
		if !trusted {
			fmt.Fprintln(os.Stderr, "warning: automatic matrix evaluation is not trusted")
		}
	}

	savedVerbose, savedOutput := makeVerbose, makeOutput
	makeVerbose, makeOutput = testVerbose, ""
	defer func() {
		makeVerbose, makeOutput = savedVerbose, savedOutput
	}()

	for _, combo := range combos {
		if err := buildModuleWithRunTest(ctx, store, modPath, version, combo, true); err != nil {
			return fmt.Errorf("test failed for %s@%s [%s]: %w", modPath, version, combo, err)
		}
		if testVerbose {
			fmt.Printf("ok %s@%s [%s]\n", modPath, version, combo)
		}
	}
	return nil
}

func loadModuleMatrix(ctx context.Context, store repo.Store, modPath, version string) (formula.Matrix, error) {
	mods, err := modules.Load(ctx, module.Version{Path: modPath, Version: version}, modules.Options{FormulaStore: store})
	if err != nil {
		return formula.Matrix{}, fmt.Errorf("failed to load modules: %w", err)
	}
	for _, mod := range mods {
		if mod.Path != modPath {
			continue
		}
		if version != "" && mod.Version != version {
			continue
		}
		return mod.Matrix, nil
	}
	return formula.Matrix{}, nil
}

func defaultRuntimeMatrix() formula.Matrix {
	return formula.Matrix{
		Require: map[string][]string{
			"os":   {runtime.GOOS},
			"arch": {runtime.GOARCH},
		},
	}
}

func defaultMatrixCombo() string {
	matrix := defaultRuntimeMatrix()
	return matrix.Combinations()[0]
}

func collectBuildTrace(ctx context.Context, store repo.Store, moduleArg, modPath, version, combo string) (evaluator.ProbeResult, error) {
	mods, err := modules.Load(ctx, module.Version{Path: modPath, Version: version}, modules.Options{
		FormulaStore: store,
	})
	if err != nil {
		return evaluator.ProbeResult{}, fmt.Errorf("failed to load modules for %s: %w", moduleArg, err)
	}

	for _, mod := range mods {
		mod.SetStdout(io.Discard)
		mod.SetStderr(io.Discard)
	}

	// Probe builds must stay quiet because formulas may write directly to process stdout/stderr.
	savedStdout, savedStderr := os.Stdout, os.Stderr
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return evaluator.ProbeResult{}, fmt.Errorf("failed to open devnull for %s: %w", moduleArg, err)
	}
	defer func() {
		devNull.Close()
		os.Stdout = savedStdout
		os.Stderr = savedStderr
	}()
	os.Stdout = devNull
	os.Stderr = devNull

	builder, err := build.NewBuilder(build.Options{
		Store:     store,
		MatrixStr: combo,
		Trace:     true,
	})
	if err != nil {
		return evaluator.ProbeResult{}, fmt.Errorf("failed to create trace builder for %s: %w", moduleArg, err)
	}

	results, err := builder.Build(ctx, mods)
	if err != nil {
		return evaluator.ProbeResult{}, fmt.Errorf("failed to trace build for %s [%s]: %w", moduleArg, combo, err)
	}
	if len(results) == 0 {
		return evaluator.ProbeResult{}, nil
	}
	result := results[len(results)-1]
	records := result.Trace
	if testTraceDump {
		if _, err := io.WriteString(savedStderr, formatTraceDump(moduleArg, combo, records, result.InputDigests)); err != nil {
			return evaluator.ProbeResult{}, fmt.Errorf("failed to write trace dump for %s [%s]: %w", moduleArg, combo, err)
		}
	}
	outputManifest, err := evaluator.BuildOutputManifest(result.OutputDir, result.Metadata)
	if err != nil {
		return evaluator.ProbeResult{}, fmt.Errorf("failed to build output manifest for %s [%s]: %w", moduleArg, combo, err)
	}
	return evaluator.ProbeResult{
		Records:          records,
		Scope:            result.TraceScope,
		TraceDiagnostics: result.TraceDiagnostics,
		InputDigests:     result.InputDigests,
		OutputManifest:   outputManifest,
	}, nil
}

func formatTraceDump(moduleArg, combo string, records []trace.Record, inputDigests map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "TRACE %s [%s]\n", moduleArg, combo)
	if len(inputDigests) > 0 {
		b.WriteString("DIGESTS\n")
		for _, path := range sortedDigestPaths(inputDigests) {
			fmt.Fprintf(&b, "   %s = %s\n", path, inputDigests[path])
		}
	}
	if len(records) == 0 {
		b.WriteString("(no records)\n")
		return b.String()
	}
	for i, rec := range records {
		fmt.Fprintf(&b, "%d. argv: %s\n", i+1, strings.Join(rec.Argv, " "))
		if rec.Cwd != "" {
			fmt.Fprintf(&b, "   cwd: %s\n", rec.Cwd)
		}
		if len(rec.Inputs) > 0 {
			fmt.Fprintf(&b, "   inputs: %s\n", strings.Join(rec.Inputs, ", "))
		}
		if len(rec.Changes) > 0 {
			fmt.Fprintf(&b, "   changes: %s\n", strings.Join(rec.Changes, ", "))
		}
	}
	return b.String()
}

func sortedDigestPaths(inputDigests map[string]string) []string {
	if len(inputDigests) == 0 {
		return nil
	}
	paths := make([]string, 0, len(inputDigests))
	for path := range inputDigests {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}
