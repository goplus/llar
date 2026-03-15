package build

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/evaluator"
	"github.com/goplus/llar/internal/modules"
	"github.com/goplus/llar/internal/trace"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/mod/module"
)

// ---------------------------------------------------------------------------
// E2E tests: full pipeline from .gox formula → modules.Load → Build
// ---------------------------------------------------------------------------

// TestE2E_ReadSourceFile verifies that a formula can read files from the
// formula store via proj.readFile during onBuild.
func TestE2E_ReadSourceFile(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/readcfg", Version: "1.0.0"}
	results, _ := loadAndBuild(t, b, store, main)

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	// config.txt contains "-lreadcfg"
	if strings.TrimSpace(results[0].Metadata) != "-lreadcfg" {
		t.Errorf("metadata = %q, want %q", results[0].Metadata, "-lreadcfg")
	}
}

// TestE2E_DepResultInjection verifies that a formula can access its
// dependency's build result via ctx.buildResult during onBuild.
// test/depresult depends on test/liba. Its onBuild reads liba's result
// and combines it: liba_metadata + " -lDR".
func TestE2E_DepResultInjection(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/depresult", Version: "1.0.0"}
	results, mods := loadAndBuild(t, b, store, main)

	// Should have 2 results: liba + depresult
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	r, ok := findResult(results, b, mods, "test/depresult")
	if !ok {
		t.Fatal("missing result for test/depresult")
	}
	// liba sets "-lA", so depresult should see it and produce "-lA -lDR"
	if r.Metadata != "-lA -lDR" {
		t.Errorf("depresult metadata = %q, want %q", r.Metadata, "-lA -lDR")
	}

	// Verify liba was also built
	libaR, ok := findResult(results, b, mods, "test/liba")
	if !ok {
		t.Fatal("missing result for test/liba")
	}
	if libaR.Metadata != "-lA" {
		t.Errorf("liba metadata = %q, want %q", libaR.Metadata, "-lA")
	}
}

// TestE2E_DiamondDeps verifies correct handling of diamond dependency graphs.
// test/diamond depends on both test/liba and test/libb.
// test/libb also depends on test/liba.
// So the graph is: diamond -> liba, diamond -> libb -> liba.
func TestE2E_DiamondDeps(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/diamond", Version: "1.0.0"}
	results, mods := loadAndBuild(t, b, store, main)

	// Should have 3 results: liba, libb, diamond
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	// Verify all metadata
	wantMeta := map[string]string{
		"test/liba":    "-lA",
		"test/libb":    "-lB",
		"test/diamond": "-lDiamond",
	}
	for path, want := range wantMeta {
		r, ok := findResult(results, b, mods, path)
		if !ok {
			t.Errorf("missing result for %s", path)
			continue
		}
		if r.Metadata != want {
			t.Errorf("%s metadata = %q, want %q", path, r.Metadata, want)
		}
	}
}

// TestE2E_MatrixVariation verifies that building the same module with
// different matrix strings produces separate cached results and install dirs.
func TestE2E_MatrixVariation(t *testing.T) {
	store := setupTestStore(t)
	wsDir := t.TempDir()

	matrices := []string{"amd64-linux", "arm64-darwin", "x86_64-linux|zlibON"}

	for _, matrix := range matrices {
		b := setupBuilder(t, store, matrix)
		b.workspaceDir = wsDir // shared workspace

		main := module.Version{Path: "test/ctxcheck", Version: "1.0.0"}
		results, _ := loadAndBuild(t, b, store, main)

		// ctxcheck sets metadata to the matrix string
		if results[0].Metadata != matrix {
			t.Errorf("matrix=%q: metadata = %q, want %q", matrix, results[0].Metadata, matrix)
		}
	}

	// Verify each matrix has its own install directory
	for _, matrix := range matrices {
		b := &Builder{workspaceDir: wsDir, matrix: matrix}
		dir, _ := b.installDir("test/ctxcheck", "1.0.0")
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("installDir not created for matrix %q: %v", matrix, err)
		}
	}
}

// TestE2E_CacheAcrossRebuilds verifies that a second build of the same
// module returns cached results without re-executing the formula.
func TestE2E_CacheAcrossRebuilds(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/liba", Version: "1.0.0"}

	// First build
	results1, _ := loadAndBuild(t, b, store, main)

	// Verify cache file exists
	cacheDir, _ := b.cacheDir("test/liba")
	cachePath := filepath.Join(cacheDir, cacheFile)
	info1, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("cache not written after first build: %v", err)
	}

	// Second build (should hit cache)
	results2, _ := loadAndBuild(t, b, store, main)

	if results1[0].Metadata != results2[0].Metadata {
		t.Errorf("rebuild metadata changed: %q → %q", results1[0].Metadata, results2[0].Metadata)
	}

	// Cache file should not be rewritten (same mtime)
	info2, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("cache disappeared after rebuild: %v", err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("cache file was rewritten on rebuild (should be cache hit)")
	}
}

// TestE2E_ErrorInChain verifies that when a dependency in a chain fails,
// the entire build fails and no downstream modules are built.
func TestE2E_ErrorInChain(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	// errmod fails during onBuild
	main := module.Version{Path: "test/errmod", Version: "1.0.0"}
	ctx := context.Background()
	mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}

	_, err = b.Build(ctx, mods)
	if err == nil {
		t.Fatal("Build(errmod) should fail")
	}

	// errmod's cache should NOT be written
	_, cacheErr := b.loadCache("test/errmod")
	if cacheErr == nil {
		t.Error("cache should not exist for failed build")
	}
}

// TestE2E_RebuildAfterCacheClear verifies that clearing the cache forces
// a full rebuild that produces the same results.
func TestE2E_RebuildAfterCacheClear(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/liba", Version: "1.0.0"}

	// First build
	results1, _ := loadAndBuild(t, b, store, main)

	// Clear the cache
	cacheDir, _ := b.cacheDir("test/liba")
	os.RemoveAll(cacheDir)

	// Rebuild
	results2, _ := loadAndBuild(t, b, store, main)

	if results1[0].Metadata != results2[0].Metadata {
		t.Errorf("metadata changed after cache clear: %q → %q",
			results1[0].Metadata, results2[0].Metadata)
	}
}

// ---------------------------------------------------------------------------
// Real build tests: actual source download + compilation
// ---------------------------------------------------------------------------

// TestE2E_RealZlibBuild downloads zlib source via real git clone and
// compiles it with cmake. Verifies the full pipeline end-to-end:
// formula loading → VCS sync → cmake configure/build/install → artifact check.
func TestE2E_RealZlibBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real build test in short mode")
	}
	if _, err := exec.LookPath("cmake"); err != nil {
		t.Skip("cmake not found, skipping real build test")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found, skipping real build test")
	}

	store := setupTestStore(t)
	matrix := runtime.GOARCH + "-" + runtime.GOOS

	b := &Builder{
		store:        store,
		matrix:       matrix,
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			return vcs.NewRepo(repoPath)
		},
	}

	main := module.Version{Path: "madler/zlib", Version: "v1.3.1"}
	ctx := context.Background()
	mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}

	results, err := b.Build(ctx, mods)
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Metadata != "-lz" {
		t.Errorf("metadata = %q, want %q", results[0].Metadata, "-lz")
	}

	// Verify build artifacts exist in installDir
	installDir, _ := b.installDir("madler/zlib", "v1.3.1")

	// Check static library
	libDir := filepath.Join(installDir, "lib")
	libEntries, err := os.ReadDir(libDir)
	if err != nil {
		t.Fatalf("lib dir not found at %s: %v", libDir, err)
	}
	hasLib := false
	for _, e := range libEntries {
		if strings.HasPrefix(e.Name(), "libz") {
			hasLib = true
			break
		}
	}
	if !hasLib {
		t.Errorf("no libz* found in %s", libDir)
	}

	// Check header
	headerPath := filepath.Join(installDir, "include", "zlib.h")
	if _, err := os.Stat(headerPath); err != nil {
		t.Errorf("zlib.h not found at %s: %v", headerPath, err)
	}
}

// TestE2E_TraceCapture_RealZlibBuild logs the captured OnBuild trace so it can
// be inspected directly via `go test -v`.
func TestE2E_TraceCapture_RealZlibBuild(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("trace capture test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping trace capture test in short mode")
	}
	for _, tool := range []string{"cmake", "git", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping trace capture test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := runtime.GOARCH + "-" + runtime.GOOS

	b := &Builder{
		store:        store,
		matrix:       matrix,
		trace:        true,
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			return vcs.NewRepo(repoPath)
		},
	}

	main := module.Version{Path: "madler/zlib", Version: "v1.3.1"}
	ctx := context.Background()
	mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}

	results, err := b.Build(ctx, mods)
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if len(results[0].Trace) == 0 {
		t.Fatal("trace is empty")
	}

	dump := formatTraceRecordsForTest(results[0].Trace)
	logPath := writeTraceLogForTest(t, dump)

	t.Logf("captured %d trace records", len(results[0].Trace))
	t.Logf("trace log written to %s", logPath)
}

func TestE2E_LocalTracecmakeBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping local build test in short mode")
	}
	if _, err := exec.LookPath("cmake"); err != nil {
		t.Skip("cmake not found, skipping local build test")
	}

	store := setupTestStore(t)
	b := setupBuilder(t, store, runtime.GOARCH+"-"+runtime.GOOS)

	main := module.Version{Path: "test/tracecmake", Version: "1.0.0"}
	results, _ := loadAndBuild(t, b, store, main)

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Metadata != "-ltracecore" {
		t.Fatalf("metadata = %q, want %q", results[0].Metadata, "-ltracecore")
	}

	installDir, _ := b.installDir("test/tracecmake", "1.0.0")
	if _, err := os.Stat(filepath.Join(installDir, "include", "trace.h")); err != nil {
		t.Fatalf("trace.h not found at %s: %v", installDir, err)
	}
	if _, err := os.Stat(filepath.Join(installDir, "include", "trace_config.h")); err != nil {
		t.Fatalf("trace_config.h not found at %s: %v", installDir, err)
	}
	if _, err := os.Stat(filepath.Join(installDir, "bin", "tracecli")); err != nil {
		if _, err2 := os.Stat(filepath.Join(installDir, "bin", "tracecli.exe")); err2 != nil {
			t.Fatalf("tracecli binary not found at %s: %v", installDir, err)
		}
	}
	libEntries, err := os.ReadDir(filepath.Join(installDir, "lib"))
	if err != nil {
		t.Fatalf("lib dir not found at %s: %v", installDir, err)
	}
	hasLib := false
	for _, e := range libEntries {
		if strings.HasPrefix(e.Name(), "libtracecore") {
			hasLib = true
			break
		}
	}
	if !hasLib {
		t.Fatalf("no libtracecore* artifact found in %s/lib", installDir)
	}
}

func TestE2E_TraceAnalyze_LocalTracecmakeBuild(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("trace analysis test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping trace analysis test in short mode")
	}
	for _, tool := range []string{"cmake", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping trace analysis test", tool)
		}
	}

	store := setupTestStore(t)
	b := setupBuilder(t, store, runtime.GOARCH+"-"+runtime.GOOS)
	b.trace = true

	main := module.Version{Path: "test/tracecmake", Version: "1.0.0"}
	results, _ := loadAndBuild(t, b, store, main)

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Metadata != "-ltracecore" {
		t.Fatalf("metadata = %q, want %q", results[0].Metadata, "-ltracecore")
	}
	if len(results[0].Trace) == 0 {
		t.Fatal("trace is empty")
	}

	summary := evaluator.DebugSummary(results[0].Trace, evaluator.DebugSummaryOptions{
		Scope:            results[0].TraceScope,
		RoleSampleLimit:  12,
		InterestingLimit: 12,
		InterestingTokens: []string{
			"/_build/trace_config.h",
			"/TryCompile-",
			"/lib/libtracecore.a",
			"/include/trace.h",
			"/include/trace_config.h",
		},
	})
	logPath := writeGraphLogForTest(t, summary)

	t.Logf("graph summary written to %s", logPath)
	if !summaryHasTokenRole(summary, "/_build/trace_config.h", "propagating") {
		t.Fatalf("expected generated header to be propagating, summary:\n%s", summary)
	}
	if !summaryHasTokenRole(summary, "/TryCompile-", "tooling") {
		t.Fatalf("expected try_compile subtree to be tooling, summary:\n%s", summary)
	}
	if !summaryHasTokenRole(summary, "/include/trace.h", "delivery") {
		t.Fatalf("expected installed header to be delivery, summary:\n%s", summary)
	}
	if !summaryHasTokenRole(summary, "/include/trace_config.h", "delivery") {
		t.Fatalf("expected installed generated header to be delivery, summary:\n%s", summary)
	}
}

func probeResultFromBuildResult(result Result) evaluator.ProbeResult {
	return evaluator.ProbeResult{
		Records:      result.Trace,
		Scope:        result.TraceScope,
		InputDigests: maps.Clone(result.InputDigests),
	}
}

func TestE2E_Watch_RealOptionClassification_LocalTraceoptions(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"api":  {"api-off", "api-on"},
			"cli":  {"cli-off", "cli-on"},
			"ship": {"ship-off", "ship-on"},
		},
		DefaultOptions: map[string][]string{
			"api":  {"api-off"},
			"cli":  {"cli-off"},
			"ship": {"ship-off"},
		},
	}

	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)
	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		b := setupBuilder(t, store, combo)
		b.trace = true

		main := module.Version{Path: "test/traceoptions", Version: "1.0.0"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}

		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		report.AddCombo(combo, probeResultFromBuildResult(result), evaluator.DebugSummaryOptions{
			RoleSampleLimit:  6,
			InterestingLimit: 6,
			InterestingTokens: []string{
				"/_build/trace_options.h",
				"/TryCompile-",
				"/lib/libtracecore.a",
				"/include/trace_alias.h",
				"/bin/tracecli",
			},
		})
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "api-off-cli-off-ship-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"api-on-cli-off-ship-off",
			"api-off-cli-on-ship-off",
			"api-off-cli-off-ship-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		pairs := [][2]string{
			{"api-on-cli-off-ship-off", "api-off-cli-on-ship-off"},
			{"api-on-cli-off-ship-off", "api-off-cli-off-ship-on"},
			{"api-off-cli-on-ship-off", "api-off-cli-off-ship-on"},
		}
		for _, pair := range pairs {
			left, leftOK := resultsByCombo[pair[0]]
			right, rightOK := resultsByCombo[pair[1]]
			if !leftOK || !rightOK {
				continue
			}
			report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
				BaseLabel:       baselineCombo,
				LeftLabel:       pair[0],
				RightLabel:      pair[1],
				PathSampleLimit: 8,
			})
		}
	}

	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("option trace records written to %s", traceLogPath)

	want := []string{
		"api-off-cli-off-ship-off",
		"api-off-cli-off-ship-on",
		"api-off-cli-on-ship-off",
		"api-on-cli-off-ship-off",
		"api-on-cli-on-ship-off",
	}
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func formatTraceCombosForTest(results map[string]evaluator.ProbeResult) string {
	if len(results) == 0 {
		return ""
	}
	combos := slices.Sorted(maps.Keys(results))
	var b strings.Builder
	for i, combo := range combos {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("COMBO ")
		b.WriteString(combo)
		b.WriteByte('\n')
		if len(results[combo].InputDigests) > 0 {
			b.WriteString("DIGESTS\n")
			for _, path := range slices.Sorted(maps.Keys(results[combo].InputDigests)) {
				b.WriteString("   ")
				b.WriteString(path)
				b.WriteString(" = ")
				b.WriteString(results[combo].InputDigests[path])
				b.WriteByte('\n')
			}
		}
		b.WriteString(formatTraceRecordsForTest(results[combo].Records))
	}
	return b.String()
}

func formatTraceRecordsForTest(records []trace.Record) string {
	var b strings.Builder
	for i, rec := range records {
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". argv: ")
		b.WriteString(strings.Join(rec.Argv, " "))
		b.WriteByte('\n')
		if rec.Cwd != "" {
			b.WriteString("   cwd: ")
			b.WriteString(rec.Cwd)
			b.WriteByte('\n')
		}
		if rec.PID != 0 {
			b.WriteString("   pid: ")
			b.WriteString(strconv.FormatInt(rec.PID, 10))
			b.WriteByte('\n')
		}
		if rec.ParentPID != 0 {
			b.WriteString("   ppid: ")
			b.WriteString(strconv.FormatInt(rec.ParentPID, 10))
			b.WriteByte('\n')
		}
		if len(rec.Inputs) > 0 {
			b.WriteString("   inputs: ")
			b.WriteString(strings.Join(rec.Inputs, ", "))
			b.WriteByte('\n')
		}
		if len(rec.Changes) > 0 {
			b.WriteString("   changes: ")
			b.WriteString(strings.Join(rec.Changes, ", "))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func writeTraceLogForTest(t *testing.T, dump string) string {
	t.Helper()

	path := os.Getenv("LLAR_TRACE_LOG")
	if path == "" {
		path = defaultTestLogPath(t, "trace")
	}

	if err := os.WriteFile(path, []byte(dump), 0o644); err != nil {
		t.Fatalf("write trace log %s: %v", path, err)
	}
	return path
}

func writeGraphLogForTest(t *testing.T, dump string) string {
	t.Helper()

	path := os.Getenv("LLAR_GRAPH_LOG")
	if path == "" {
		path = defaultTestLogPath(t, "graph")
	}

	if err := os.WriteFile(path, []byte(dump), 0o644); err != nil {
		t.Fatalf("write graph log %s: %v", path, err)
	}
	return path
}

func defaultTestLogPath(t *testing.T, kind string) string {
	t.Helper()

	root := projectRootForTest(t)
	dir := filepath.Join(root, ".llar-e2e-logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create test log dir %s: %v", dir, err)
	}
	filename := fmt.Sprintf("%s-%s-%d.log", sanitizeTestLogName(t.Name()), kind, time.Now().UnixNano())
	return filepath.Join(dir, filename)
}

func projectRootForTest(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate e2e_test.go: runtime.Caller failed")
	}
	for dir := filepath.Dir(file); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("find project root from %s: go.mod not found", file)
		}
	}
}

func sanitizeTestLogName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func summaryHasTokenRole(summary, token, role string) bool {
	for _, line := range strings.Split(summary, "\n") {
		if strings.Contains(line, token) && strings.Contains(line, "=> "+role) {
			return true
		}
	}
	return false
}

// TestE2E_RealLibpngBuild builds libpng with its zlib dependency using cmake.use.
// Verifies: formula dep resolution → zlib built first → cmake.use injects zlib →
// libpng configure/build/install succeeds → artifacts exist.
func TestE2E_RealLibpngBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real build test in short mode")
	}
	if _, err := exec.LookPath("cmake"); err != nil {
		t.Skip("cmake not found, skipping real build test")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found, skipping real build test")
	}

	store := setupTestStore(t)
	matrix := runtime.GOARCH + "-" + runtime.GOOS

	b := &Builder{
		store:        store,
		matrix:       matrix,
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			return vcs.NewRepo(repoPath)
		},
	}

	main := module.Version{Path: "pnggroup/libpng", Version: "v1.6.47"}
	ctx := context.Background()
	mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}

	// Should have 2 modules: zlib + libpng
	if len(mods) != 2 {
		t.Fatalf("got %d modules, want 2", len(mods))
	}

	results, err := b.Build(ctx, mods)
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// Verify zlib was built (first in order)
	zlibR, ok := findResult(results, b, mods, "madler/zlib")
	if !ok {
		t.Fatal("missing result for madler/zlib")
	}
	if zlibR.Metadata != "-lz" {
		t.Errorf("zlib metadata = %q, want %q", zlibR.Metadata, "-lz")
	}

	// Verify libpng was built
	pngR, ok := findResult(results, b, mods, "pnggroup/libpng")
	if !ok {
		t.Fatal("missing result for pnggroup/libpng")
	}
	if pngR.Metadata != "-lpng" {
		t.Errorf("libpng metadata = %q, want %q", pngR.Metadata, "-lpng")
	}

	// Verify libpng build artifacts
	pngInstallDir, _ := b.installDir("pnggroup/libpng", "v1.6.47")

	// Check library
	libDir := filepath.Join(pngInstallDir, "lib")
	libEntries, err := os.ReadDir(libDir)
	if err != nil {
		t.Fatalf("lib dir not found at %s: %v", libDir, err)
	}
	hasLib := false
	for _, e := range libEntries {
		if strings.HasPrefix(e.Name(), "libpng") {
			hasLib = true
			break
		}
	}
	if !hasLib {
		t.Errorf("no libpng* found in %s", libDir)
	}

	// Check header
	headerPath := filepath.Join(pngInstallDir, "include", "libpng16", "png.h")
	if _, err := os.Stat(headerPath); err != nil {
		// Some cmake configs install directly to include/
		headerPath = filepath.Join(pngInstallDir, "include", "png.h")
		if _, err := os.Stat(headerPath); err != nil {
			t.Errorf("png.h not found in include/ or include/libpng16/")
		}
	}
}

// TestE2E_RealFreetypeBuild builds freetype with its transitive dependencies:
// freetype -> {libpng, zlib}, libpng -> zlib (diamond).
// Demonstrates: onRequire dynamic dep extraction from meson wrap files →
// diamond dep resolution → cmake.use injection → pkg-config metadata extraction.
func TestE2E_RealFreetypeBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real build test in short mode")
	}
	for _, tool := range []string{"cmake", "git", "pkg-config"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real build test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := runtime.GOARCH + "-" + runtime.GOOS

	b := &Builder{
		store:        store,
		matrix:       matrix,
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			return vcs.NewRepo(repoPath)
		},
	}

	main := module.Version{Path: "freetype/freetype", Version: "VER-2-13-3"}
	ctx := context.Background()
	mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}

	// Should have 3 modules: zlib + libpng + freetype
	if len(mods) != 3 {
		t.Fatalf("got %d modules, want 3 (zlib, libpng, freetype)", len(mods))
	}
	t.Logf("resolved modules: %v", mods)

	results, err := b.Build(ctx, mods)
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	// Verify freetype metadata from pkg-config contains -lfreetype
	ftR, ok := findResult(results, b, mods, "freetype/freetype")
	if !ok {
		t.Fatal("missing result for freetype/freetype")
	}
	if !strings.Contains(ftR.Metadata, "-lfreetype") {
		t.Errorf("freetype metadata = %q, want it to contain %q", ftR.Metadata, "-lfreetype")
	}
	t.Logf("freetype metadata (from pkg-config): %s", strings.TrimSpace(ftR.Metadata))

	// Verify freetype build artifacts
	ftInstallDir, _ := b.installDir("freetype/freetype", "VER-2-13-3")

	// Check library
	libDir := filepath.Join(ftInstallDir, "lib")
	libEntries, err := os.ReadDir(libDir)
	if err != nil {
		t.Fatalf("lib dir not found at %s: %v", libDir, err)
	}
	hasLib := false
	for _, e := range libEntries {
		if strings.HasPrefix(e.Name(), "libfreetype") {
			hasLib = true
			break
		}
	}
	if !hasLib {
		t.Errorf("no libfreetype* found in %s", libDir)
	}

	// Check header
	headerPath := filepath.Join(ftInstallDir, "include", "freetype2", "freetype", "freetype.h")
	if _, err := os.Stat(headerPath); err != nil {
		headerPath = filepath.Join(ftInstallDir, "include", "freetype2", "ft2build.h")
		if _, err := os.Stat(headerPath); err != nil {
			t.Errorf("freetype headers not found in include/freetype2/")
		}
	}
}

func TestE2E_LoadBoostFormula(t *testing.T) {
	store := setupTestStore(t)
	_, err := modules.Load(context.Background(), module.Version{
		Path:    "boostorg/boost",
		Version: "boost-1.90.0",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(boostorg/boost) failed: %v", err)
	}
}

func TestE2E_LoadFmtFormula(t *testing.T) {
	store := setupTestStore(t)
	_, err := modules.Load(context.Background(), module.Version{
		Path:    "fmtlib/fmt",
		Version: "11.1.4",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(fmtlib/fmt) failed: %v", err)
	}
}

func TestE2E_LoadLibjpegTurboFormula(t *testing.T) {
	store := setupTestStore(t)
	_, err := modules.Load(context.Background(), module.Version{
		Path:    "libjpeg-turbo/libjpeg-turbo",
		Version: "3.1.3",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(libjpeg-turbo/libjpeg-turbo) failed: %v", err)
	}
}

func TestE2E_LoadSqliteFormula(t *testing.T) {
	store := setupTestStore(t)
	_, err := modules.Load(context.Background(), module.Version{
		Path:    "sqlite/sqlite",
		Version: "3.45.3",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(sqlite/sqlite) failed: %v", err)
	}
}

func TestE2E_LoadPocoFormula(t *testing.T) {
	store := setupTestStore(t)
	_, err := modules.Load(context.Background(), module.Version{
		Path:    "pocoproject/poco",
		Version: "poco-1.14.2-release",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(pocoproject/poco) failed: %v", err)
	}
}

func TestE2E_LoadPCRE2Formula(t *testing.T) {
	store := setupTestStore(t)
	_, err := modules.Load(context.Background(), module.Version{
		Path:    "PCRE2Project/pcre2",
		Version: "pcre2-10.45",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(PCRE2Project/pcre2) failed: %v", err)
	}
}

func TestE2E_LoadPugixmlFormula(t *testing.T) {
	store := setupTestStore(t)
	_, err := modules.Load(context.Background(), module.Version{
		Path:    "zeux/pugixml",
		Version: "1.15",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(zeux/pugixml) failed: %v", err)
	}
}

func TestE2E_LoadExpatFormula(t *testing.T) {
	store := setupTestStore(t)
	_, err := modules.Load(context.Background(), module.Version{
		Path:    "libexpat/libexpat",
		Version: "2.6.4",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(libexpat/libexpat) failed: %v", err)
	}
}

func TestE2E_LoadCjsonFormula(t *testing.T) {
	store := setupTestStore(t)
	_, err := modules.Load(context.Background(), module.Version{
		Path:    "DaveGamble/cJSON",
		Version: "1.7.19",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(DaveGamble/cJSON) failed: %v", err)
	}
}

func TestE2E_LoadCAresFormula(t *testing.T) {
	store := setupTestStore(t)
	_, err := modules.Load(context.Background(), module.Version{
		Path:    "c-ares/c-ares",
		Version: "1.34.5",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(c-ares/c-ares) failed: %v", err)
	}
}

func TestE2E_LoadLibwebpFormula(t *testing.T) {
	store := setupTestStore(t)
	_, err := modules.Load(context.Background(), module.Version{
		Path:    "webmproject/libwebp",
		Version: "1.5.0",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(webmproject/libwebp) failed: %v", err)
	}
}

func TestE2E_LoadLibtiffFormula(t *testing.T) {
	store := setupTestStore(t)
	_, err := modules.Load(context.Background(), module.Version{
		Path:    "libsdl-org/libtiff",
		Version: "4.7.1",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(libsdl-org/libtiff) failed: %v", err)
	}
}

func TestE2E_LoadZstdFormula(t *testing.T) {
	store := setupTestStore(t)
	_, err := modules.Load(context.Background(), module.Version{
		Path:    "facebook/zstd",
		Version: "1.5.7",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(facebook/zstd) failed: %v", err)
	}
}

func TestE2E_LoadYamlCppFormula(t *testing.T) {
	store := setupTestStore(t)

	mods, err := modules.Load(context.Background(), module.Version{
		Path:    "jbeder/yaml-cpp",
		Version: "0.9.0",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(jbeder/yaml-cpp) failed: %v", err)
	}
	if len(mods) != 1 {
		t.Fatalf("got %d modules, want 1", len(mods))
	}
}

func TestE2E_LoadSpdlogFormula(t *testing.T) {
	store := setupTestStore(t)

	mods, err := modules.Load(context.Background(), module.Version{
		Path:    "gabime/spdlog",
		Version: "1.17.0",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(gabime/spdlog) failed: %v", err)
	}
	if len(mods) != 1 {
		t.Fatalf("got %d modules, want 1", len(mods))
	}
}

func TestE2E_RealFmtBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real fmt build test in short mode")
	}
	for _, tool := range []string{"cmake", "c++"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real fmt build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newFmtReleaseRepo(t, "11.1.4")
	b := &Builder{
		store:        store,
		matrix:       "osapi-on-unicode-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/fmtlib/fmt" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "fmtlib/fmt", Version: "11.1.4"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	fmtR, ok := findResult(results, b, mods, "fmtlib/fmt")
	if !ok {
		t.Fatal("missing result for fmtlib/fmt")
	}
	if !strings.Contains(fmtR.Metadata, "-lfmt") {
		t.Fatalf("metadata = %q, want it to contain -lfmt", fmtR.Metadata)
	}

	installDir, _ := b.installDir("fmtlib/fmt", "11.1.4")
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libfmt") {
		t.Fatalf("missing libfmt* under %s", filepath.Join(installDir, "lib"))
	}
}

func TestE2E_RealLibjpegTurboBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real libjpeg-turbo build test in short mode")
	}
	for _, tool := range []string{"cmake", "cc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real libjpeg-turbo build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newLibjpegTurboReleaseRepo(t, "3.1.3")
	b := &Builder{
		store:        store,
		matrix:       "arithdec-on-arithenc-on-tools-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/libjpeg-turbo/libjpeg-turbo" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "libjpeg-turbo/libjpeg-turbo", Version: "3.1.3"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	jpegR, ok := findResult(results, b, mods, "libjpeg-turbo/libjpeg-turbo")
	if !ok {
		t.Fatal("missing result for libjpeg-turbo/libjpeg-turbo")
	}
	if !strings.Contains(jpegR.Metadata, "-ljpeg") {
		t.Fatalf("metadata = %q, want it to contain -ljpeg", jpegR.Metadata)
	}

	installDir, _ := b.installDir("libjpeg-turbo/libjpeg-turbo", "3.1.3")
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libjpeg") {
		t.Fatalf("missing libjpeg* under %s", filepath.Join(installDir, "lib"))
	}
	if !dirHasPrefix(filepath.Join(installDir, "bin"), "cjpeg") {
		t.Fatalf("missing cjpeg* under %s", filepath.Join(installDir, "bin"))
	}
}

func TestE2E_RealSqliteBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real sqlite build test in short mode")
	}
	for _, tool := range []string{"cc", "ar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real sqlite build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newSqliteReleaseRepo(t, "3.45.3")
	b := &Builder{
		store:        store,
		matrix:       "dbstat-on-json1-on-rtree-on-soundex-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/sqlite/sqlite" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "sqlite/sqlite", Version: "3.45.3"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	sqliteR, ok := findResult(results, b, mods, "sqlite/sqlite")
	if !ok {
		t.Fatal("missing result for sqlite/sqlite")
	}
	if !strings.Contains(sqliteR.Metadata, "-lsqlite3") {
		t.Fatalf("metadata = %q, want it to contain -lsqlite3", sqliteR.Metadata)
	}

	installDir, _ := b.installDir("sqlite/sqlite", "3.45.3")
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libsqlite3") {
		t.Fatalf("missing libsqlite3* under %s", filepath.Join(installDir, "lib"))
	}
	for _, hdr := range []string{"sqlite3.h", "sqlite3ext.h"} {
		if _, err := os.Stat(filepath.Join(installDir, "include", hdr)); err != nil {
			t.Fatalf("missing %s under %s/include", hdr, installDir)
		}
	}
}

func TestE2E_RealPocoBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real Poco build test in short mode")
	}
	for _, tool := range []string{"cmake", "c++"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real Poco build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newPocoReleaseRepo(t, "poco-1.14.2-release")
	b := &Builder{
		store:        store,
		matrix:       "encodings-on-json-on-net-on-xml-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/pocoproject/poco" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "pocoproject/poco", Version: "poco-1.14.2-release"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	pocoR, ok := findResult(results, b, mods, "pocoproject/poco")
	if !ok {
		t.Fatal("missing result for pocoproject/poco")
	}
	for _, lib := range []string{"-lPocoFoundation", "-lPocoEncodings", "-lPocoJSON", "-lPocoNet", "-lPocoXML"} {
		if !strings.Contains(pocoR.Metadata, lib) {
			t.Fatalf("metadata = %q, want it to contain %q", pocoR.Metadata, lib)
		}
	}

	installDir, _ := b.installDir("pocoproject/poco", "poco-1.14.2-release")
	for _, lib := range []string{"libPocoFoundation", "libPocoEncodings", "libPocoJSON", "libPocoNet", "libPocoXML"} {
		if !dirHasPrefix(filepath.Join(installDir, "lib"), lib) {
			t.Fatalf("missing %s* under %s", lib, filepath.Join(installDir, "lib"))
		}
	}
}

func TestE2E_RealPCRE2Build(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real PCRE2 build test in short mode")
	}
	for _, tool := range []string{"cmake", "cc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real PCRE2 build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newPcre2ReleaseRepo(t, "pcre2-10.45")
	b := &Builder{
		store:        store,
		matrix:       "grep-on-width16-on-width32-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/PCRE2Project/pcre2" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "PCRE2Project/pcre2", Version: "pcre2-10.45"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	pcre2R, ok := findResult(results, b, mods, "PCRE2Project/pcre2")
	if !ok {
		t.Fatal("missing result for PCRE2Project/pcre2")
	}
	for _, lib := range []string{"-lpcre2-8", "-lpcre2-posix", "-lpcre2-16", "-lpcre2-32"} {
		if !strings.Contains(pcre2R.Metadata, lib) {
			t.Fatalf("metadata = %q, want it to contain %q", pcre2R.Metadata, lib)
		}
	}

	installDir, _ := b.installDir("PCRE2Project/pcre2", "pcre2-10.45")
	for _, lib := range []string{"libpcre2-8", "libpcre2-posix", "libpcre2-16", "libpcre2-32"} {
		if !dirHasPrefix(filepath.Join(installDir, "lib"), lib) {
			t.Fatalf("missing %s* under %s", lib, filepath.Join(installDir, "lib"))
		}
	}
	if !dirHasPrefix(filepath.Join(installDir, "bin"), "pcre2grep") {
		t.Fatalf("missing pcre2grep* under %s", filepath.Join(installDir, "bin"))
	}
}

func TestE2E_RealPugixmlBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real pugixml build test in short mode")
	}
	for _, tool := range []string{"cmake", "c++"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real pugixml build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newPugixmlReleaseRepo(t, "1.15")
	b := &Builder{
		store:        store,
		matrix:       "compact-on-noexceptions-on-noxpath-on-wchar-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/zeux/pugixml" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "zeux/pugixml", Version: "1.15"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	pugiR, ok := findResult(results, b, mods, "zeux/pugixml")
	if !ok {
		t.Fatal("missing result for zeux/pugixml")
	}
	if !strings.Contains(pugiR.Metadata, "-lpugixml") {
		t.Fatalf("metadata = %q, want it to contain -lpugixml", pugiR.Metadata)
	}

	installDir, _ := b.installDir("zeux/pugixml", "1.15")
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libpugixml") {
		t.Fatalf("missing libpugixml* under %s", filepath.Join(installDir, "lib"))
	}
	for _, hdr := range []string{"pugixml.hpp", "pugiconfig.hpp"} {
		if _, err := os.Stat(filepath.Join(installDir, "include", hdr)); err != nil {
			t.Fatalf("missing %s under %s/include", hdr, installDir)
		}
	}
}

func TestE2E_RealExpatBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real expat build test in short mode")
	}
	for _, tool := range []string{"cmake", "cc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real expat build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newExpatReleaseRepo(t, "2.6.4")
	b := &Builder{
		store:        store,
		matrix:       "ge-on-large_size-on-min_size-on-ns-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/libexpat/libexpat" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "libexpat/libexpat", Version: "2.6.4"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	expatR, ok := findResult(results, b, mods, "libexpat/libexpat")
	if !ok {
		t.Fatal("missing result for libexpat/libexpat")
	}
	if !strings.Contains(expatR.Metadata, "-lexpat") {
		t.Fatalf("metadata = %q, want it to contain -lexpat", expatR.Metadata)
	}

	installDir, _ := b.installDir("libexpat/libexpat", "2.6.4")
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libexpat") {
		t.Fatalf("missing libexpat* under %s", filepath.Join(installDir, "lib"))
	}
	for _, hdr := range []string{"expat.h", "expat_external.h", "expat_config.h"} {
		if _, err := os.Stat(filepath.Join(installDir, "include", hdr)); err != nil {
			t.Fatalf("missing %s under %s/include", hdr, installDir)
		}
	}
}

func TestE2E_RealCjsonBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real cJSON build test in short mode")
	}
	for _, tool := range []string{"cmake", "cc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real cJSON build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newCjsonReleaseRepo(t, "1.7.19")
	b := &Builder{
		store:        store,
		matrix:       "locales-on-utils-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/DaveGamble/cJSON" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "DaveGamble/cJSON", Version: "1.7.19"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	modR, ok := findResult(results, b, mods, "DaveGamble/cJSON")
	if !ok {
		t.Fatal("missing result for DaveGamble/cJSON")
	}
	if !strings.Contains(modR.Metadata, "-lcjson") || !strings.Contains(modR.Metadata, "-lcjson_utils") {
		t.Fatalf("metadata = %q, want it to contain -lcjson and -lcjson_utils", modR.Metadata)
	}

	installDir, _ := b.installDir("DaveGamble/cJSON", "1.7.19")
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libcjson") {
		t.Fatalf("missing libcjson* under %s", filepath.Join(installDir, "lib"))
	}
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libcjson_utils") {
		t.Fatalf("missing libcjson_utils* under %s", filepath.Join(installDir, "lib"))
	}
}

func TestE2E_RealCAresBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real c-ares build test in short mode")
	}
	for _, tool := range []string{"cmake", "cc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real c-ares build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newCAresReleaseRepo(t, "1.34.5")
	b := &Builder{
		store:        store,
		matrix:       "threads-on-tools-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/c-ares/c-ares" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "c-ares/c-ares", Version: "1.34.5"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	modR, ok := findResult(results, b, mods, "c-ares/c-ares")
	if !ok {
		t.Fatal("missing result for c-ares/c-ares")
	}
	if !strings.Contains(modR.Metadata, "-lcares") {
		t.Fatalf("metadata = %q, want it to contain -lcares", modR.Metadata)
	}

	installDir, _ := b.installDir("c-ares/c-ares", "1.34.5")
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libcares") {
		t.Fatalf("missing libcares* under %s", filepath.Join(installDir, "lib"))
	}
	if !dirHasPrefix(filepath.Join(installDir, "bin"), "adig") && !dirHasPrefix(filepath.Join(installDir, "bin"), "ahost") {
		t.Fatalf("missing c-ares tools under %s", filepath.Join(installDir, "bin"))
	}
}

func TestE2E_RealLibwebpBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real libwebp build test in short mode")
	}
	for _, tool := range []string{"cmake", "cc", "c++"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real libwebp build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newLibwebpReleaseRepo(t, "1.5.0")
	b := &Builder{
		store:        store,
		matrix:       "cwebp-on-mux-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/webmproject/libwebp" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "webmproject/libwebp", Version: "1.5.0"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	modR, ok := findResult(results, b, mods, "webmproject/libwebp")
	if !ok {
		t.Fatal("missing result for webmproject/libwebp")
	}
	if !strings.Contains(modR.Metadata, "-lwebp") || !strings.Contains(modR.Metadata, "-lwebpmux") {
		t.Fatalf("metadata = %q, want it to contain -lwebp and -lwebpmux", modR.Metadata)
	}

	installDir, _ := b.installDir("webmproject/libwebp", "1.5.0")
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libwebp") {
		t.Fatalf("missing libwebp* under %s", filepath.Join(installDir, "lib"))
	}
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libwebpmux") {
		t.Fatalf("missing libwebpmux* under %s", filepath.Join(installDir, "lib"))
	}
	if !dirHasPrefix(filepath.Join(installDir, "bin"), "cwebp") {
		t.Fatalf("missing cwebp under %s", filepath.Join(installDir, "bin"))
	}
}

func TestE2E_RealLibtiffBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real libtiff build test in short mode")
	}
	for _, tool := range []string{"cmake", "cc", "c++"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real libtiff build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newLibtiffReleaseRepo(t, "4.7.1")
	b := &Builder{
		store:        store,
		matrix:       "cxx-on-tools-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/libsdl-org/libtiff" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "libsdl-org/libtiff", Version: "4.7.1"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	modR, ok := findResult(results, b, mods, "libsdl-org/libtiff")
	if !ok {
		t.Fatal("missing result for libsdl-org/libtiff")
	}
	if !strings.Contains(modR.Metadata, "-ltiff") || !strings.Contains(modR.Metadata, "-ltiffxx") {
		t.Fatalf("metadata = %q, want it to contain -ltiff and -ltiffxx", modR.Metadata)
	}

	installDir, _ := b.installDir("libsdl-org/libtiff", "4.7.1")
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libtiff") {
		t.Fatalf("missing libtiff* under %s", filepath.Join(installDir, "lib"))
	}
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libtiffxx") {
		t.Fatalf("missing libtiffxx* under %s", filepath.Join(installDir, "lib"))
	}
	if !dirHasPrefix(filepath.Join(installDir, "bin"), "tiffinfo") {
		t.Fatalf("missing tiff tools under %s", filepath.Join(installDir, "bin"))
	}
}

func TestE2E_RealZstdBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real zstd build test in short mode")
	}
	for _, tool := range []string{"cmake", "cc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real zstd build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newZstdReleaseRepo(t, "1.5.7")
	b := &Builder{
		store:        store,
		matrix:       "programs-on-threading-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/facebook/zstd" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "facebook/zstd", Version: "1.5.7"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	modR, ok := findResult(results, b, mods, "facebook/zstd")
	if !ok {
		t.Fatal("missing result for facebook/zstd")
	}
	if !strings.Contains(modR.Metadata, "-lzstd") {
		t.Fatalf("metadata = %q, want it to contain -lzstd", modR.Metadata)
	}

	installDir, _ := b.installDir("facebook/zstd", "1.5.7")
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libzstd") {
		t.Fatalf("missing libzstd* under %s", filepath.Join(installDir, "lib"))
	}
	if !dirHasPrefix(filepath.Join(installDir, "bin"), "zstd") {
		t.Fatalf("missing zstd under %s", filepath.Join(installDir, "bin"))
	}
}

func TestE2E_RealYamlCppBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real yaml-cpp build test in short mode")
	}
	for _, tool := range []string{"cmake", "c++"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real yaml-cpp build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newYamlCppReleaseRepo(t, "0.9.0")
	b := &Builder{
		store:        store,
		matrix:       "contrib-on-tools-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/jbeder/yaml-cpp" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "jbeder/yaml-cpp", Version: "0.9.0"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	modR, ok := findResult(results, b, mods, "jbeder/yaml-cpp")
	if !ok {
		t.Fatal("missing result for jbeder/yaml-cpp")
	}
	if !strings.Contains(modR.Metadata, "-lyaml-cpp") {
		t.Fatalf("metadata = %q, want it to contain -lyaml-cpp", modR.Metadata)
	}

	installDir, _ := b.installDir("jbeder/yaml-cpp", "0.9.0")
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libyaml-cpp") {
		t.Fatalf("missing libyaml-cpp* under %s", filepath.Join(installDir, "lib"))
	}
	// Upstream builds parse/read/sandbox when YAML_CPP_BUILD_TOOLS=ON, but 0.9.0 does
	// not install those binaries. Smoke test only asserts the installed library.
}

func TestE2E_RealSpdlogBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real spdlog build test in short mode")
	}
	for _, tool := range []string{"cmake", "c++"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real spdlog build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newSpdlogReleaseRepo(t, "1.17.0")
	b := &Builder{
		store:        store,
		matrix:       "noexceptions-on-wchar-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/gabime/spdlog" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "gabime/spdlog", Version: "1.17.0"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	modR, ok := findResult(results, b, mods, "gabime/spdlog")
	if !ok {
		t.Fatal("missing result for gabime/spdlog")
	}
	if !strings.Contains(modR.Metadata, "-lspdlog") {
		t.Fatalf("metadata = %q, want it to contain -lspdlog", modR.Metadata)
	}

	installDir, _ := b.installDir("gabime/spdlog", "1.17.0")
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "libspdlog") {
		t.Fatalf("missing libspdlog* under %s", filepath.Join(installDir, "lib"))
	}
}
func TestE2E_LoadUriparserFormula(t *testing.T) {
	store := setupTestStore(t)
	_, err := modules.Load(context.Background(), module.Version{
		Path:    "uriparser/uriparser",
		Version: "0.9.8",
	}, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(uriparser/uriparser) failed: %v", err)
	}
}

func TestE2E_RealUriparserBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real uriparser build test in short mode")
	}
	for _, tool := range []string{"cmake", "cc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real uriparser build test", tool)
		}
	}

	store := setupTestStore(t)
	releaseRepo := newUriparserReleaseRepo(t, "0.9.8")
	b := &Builder{
		store:        store,
		matrix:       "tools-on-wchar-on",
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			if repoPath != "github.com/uriparser/uriparser" {
				return nil, fmt.Errorf("unexpected repo path %q", repoPath)
			}
			return releaseRepo, nil
		},
	}

	main := module.Version{Path: "uriparser/uriparser", Version: "0.9.8"}
	results, mods := loadAndBuild(t, b, store, main)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	modR, ok := findResult(results, b, mods, "uriparser/uriparser")
	if !ok {
		t.Fatal("missing result for uriparser/uriparser")
	}
	if !strings.Contains(modR.Metadata, "-luriparser") {
		t.Fatalf("metadata = %q, want it to contain -luriparser", modR.Metadata)
	}

	installDir, _ := b.installDir("uriparser/uriparser", "0.9.8")
	if !dirHasPrefix(filepath.Join(installDir, "lib"), "liburiparser") {
		t.Fatalf("missing liburiparser* under %s", filepath.Join(installDir, "lib"))
	}
	if _, err := os.Stat(filepath.Join(installDir, "bin", "uriparse")); err != nil {
		t.Fatalf("missing uriparse under %s", filepath.Join(installDir, "bin"))
	}
}

// TestE2E_Watch_RealOptionClassification_BoostProgramOptionsTimer validates
// the current graph reduction logic against a real install-heavy library.
// It uses the official Boost release tarball because the boostorg/boost git
// superproject is submodule-based and cannot be built from the current VCS sync
// behavior used by Builder.
func TestE2E_Watch_RealOptionClassification_BoostProgramOptionsTimer(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Boost option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping heavy Boost option classification test in short mode")
	}
	for _, tool := range []string{"c++", "cp", "mkdir", "sh", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping heavy Boost option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"program_options": {"program_options-off", "program_options-on"},
			"timer":           {"timer-off", "timer-on"},
		},
		DefaultOptions: map[string][]string{
			"program_options": {"program_options-off"},
			"timer":           {"timer-off"},
		},
	}

	releaseRepo := newBoostReleaseRepo(t, "boost-1.90.0")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("Boost probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/boostorg/boost" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "boostorg/boost", Version: "boost-1.90.0"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}

		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		records := result.Trace
		t.Logf("Boost probe done: %s (%d trace records)", combo, len(records))
		report.AddCombo(combo, probeResultFromBuildResult(result), evaluator.DebugSummaryOptions{
			RoleSampleLimit:  8,
			InterestingLimit: 8,
			InterestingTokens: []string{
				"/libs/timer/",
				"/libs/program_options/",
				"/libboost_timer",
				"/libboost_program_options",
				"/include/boost/timer",
				"/include/boost/program_options",
			},
		})
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "program_options-off-timer-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		for _, combo := range []string{"program_options-on-timer-off", "program_options-off-timer-on"} {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		left, leftOK := resultsByCombo["program_options-on-timer-off"]
		right, rightOK := resultsByCombo["program_options-off-timer-on"]
		if leftOK && rightOK {
			report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
				BaseLabel:       baselineCombo,
				LeftLabel:       "program_options-on-timer-off",
				RightLabel:      "program_options-off-timer-on",
				PathSampleLimit: 8,
			})
		}
	}

	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("Boost option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("Boost option trace records written to %s", traceLogPath)

	want := []string{
		"program_options-off-timer-off",
		"program_options-off-timer-on",
		"program_options-on-timer-off",
	}
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_PocoJsonEncodings(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Poco option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real Poco option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "c++", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real Poco option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"encodings": {"encodings-off", "encodings-on"},
			"json":      {"json-off", "json-on"},
			"net":       {"net-off", "net-on"},
			"xml":       {"xml-off", "xml-on"},
		},
		DefaultOptions: map[string][]string{
			"encodings": {"encodings-off"},
			"json":      {"json-off"},
			"net":       {"net-off"},
			"xml":       {"xml-off"},
		},
	}

	releaseRepo := newPocoReleaseRepo(t, "poco-1.14.2-release")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("Poco probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/pocoproject/poco" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "pocoproject/poco", Version: "poco-1.14.2-release"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}

		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("Poco probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult

		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  10,
			InterestingLimit: 10,
			InterestingTokens: []string{
				"/JSON/",
				"/Encodings/",
				"/XML/",
				"/Net/",
				"/libPocoJSON",
				"/libPocoEncodings",
				"/libPocoXML",
				"/libPocoNet",
				"/include/Poco/JSON",
				"/include/Poco/TextEncoding",
				"/include/Poco/DOM",
				"/include/Poco/Net",
			},
		})
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "encodings-off-json-off-net-off-xml-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"encodings-on-json-off-net-off-xml-off",
			"encodings-off-json-on-net-off-xml-off",
			"encodings-off-json-off-net-on-xml-off",
			"encodings-off-json-off-net-off-xml-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		for i := 0; i < len(singletons); i++ {
			for j := i + 1; j < len(singletons); j++ {
				left, leftOK := resultsByCombo[singletons[i]]
				right, rightOK := resultsByCombo[singletons[j]]
				if leftOK && rightOK {
					report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
						BaseLabel:       baselineCombo,
						LeftLabel:       singletons[i],
						RightLabel:      singletons[j],
						PathSampleLimit: 8,
					})
				}
			}
		}
	}

	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("Poco option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("Poco option trace records written to %s", traceLogPath)

	want := []string{
		"encodings-off-json-off-net-off-xml-off",
		"encodings-off-json-off-net-off-xml-on",
		"encodings-off-json-off-net-on-xml-off",
		"encodings-off-json-on-net-off-xml-off",
		"encodings-on-json-off-net-off-xml-off",
	}
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_PCRE2WidthsAndGrep(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("PCRE2 option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real PCRE2 option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "cc", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real PCRE2 option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"grep":    {"grep-off", "grep-on"},
			"width16": {"width16-off", "width16-on"},
			"width32": {"width32-off", "width32-on"},
		},
		DefaultOptions: map[string][]string{
			"grep":    {"grep-off"},
			"width16": {"width16-off"},
			"width32": {"width32-off"},
		},
	}

	releaseRepo := newPcre2ReleaseRepo(t, "pcre2-10.45")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("PCRE2 probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/PCRE2Project/pcre2" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "PCRE2Project/pcre2", Version: "pcre2-10.45"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}

		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("PCRE2 probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult

		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  10,
			InterestingLimit: 10,
			InterestingTokens: []string{
				"/src/pcre2grep.c",
				"/libpcre2-8",
				"/libpcre2-16",
				"/libpcre2-32",
				"/bin/pcre2grep",
			},
		})
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "grep-off-width16-off-width32-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"grep-on-width16-off-width32-off",
			"grep-off-width16-on-width32-off",
			"grep-off-width16-off-width32-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		for i := 0; i < len(singletons); i++ {
			for j := i + 1; j < len(singletons); j++ {
				left, leftOK := resultsByCombo[singletons[i]]
				right, rightOK := resultsByCombo[singletons[j]]
				if leftOK && rightOK {
					report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
						BaseLabel:       baselineCombo,
						LeftLabel:       singletons[i],
						RightLabel:      singletons[j],
						PathSampleLimit: 8,
					})
				}
			}
		}
	}

	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("PCRE2 option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("PCRE2 option trace records written to %s", traceLogPath)

	want := matrix.Combinations()
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_FmtOsUnicode(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fmt option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real fmt option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "c++", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real fmt option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"osapi":   {"osapi-off", "osapi-on"},
			"unicode": {"unicode-off", "unicode-on"},
		},
		DefaultOptions: map[string][]string{
			"osapi":   {"osapi-off"},
			"unicode": {"unicode-off"},
		},
	}

	releaseRepo := newFmtReleaseRepo(t, "11.1.4")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("fmt probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/fmtlib/fmt" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "fmtlib/fmt", Version: "11.1.4"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("fmt probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  8,
			InterestingLimit: 8,
			InterestingTokens: []string{
				"/include/fmt/",
				"/libfmt",
			},
		})
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "osapi-off-unicode-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"osapi-on-unicode-off",
			"osapi-off-unicode-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		if left, leftOK := resultsByCombo[singletons[0]]; leftOK {
			if right, rightOK := resultsByCombo[singletons[1]]; rightOK {
				report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
					BaseLabel:       baselineCombo,
					LeftLabel:       singletons[0],
					RightLabel:      singletons[1],
					PathSampleLimit: 8,
				})
			}
		}
	}
	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("fmt option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("fmt option trace records written to %s", traceLogPath)

	want := []string{
		"osapi-off-unicode-off",
		"osapi-off-unicode-on",
		"osapi-on-unicode-off",
	}
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_LibjpegTurboArithmeticAndTools(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("libjpeg-turbo option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real libjpeg-turbo option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "cc", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real libjpeg-turbo option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"arithdec": {"arithdec-off", "arithdec-on"},
			"arithenc": {"arithenc-off", "arithenc-on"},
			"tools":    {"tools-off", "tools-on"},
		},
		DefaultOptions: map[string][]string{
			"arithdec": {"arithdec-off"},
			"arithenc": {"arithenc-off"},
			"tools":    {"tools-off"},
		},
	}

	releaseRepo := newLibjpegTurboReleaseRepo(t, "3.1.3")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("libjpeg-turbo probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/libjpeg-turbo/libjpeg-turbo" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "libjpeg-turbo/libjpeg-turbo", Version: "3.1.3"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("libjpeg-turbo probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  8,
			InterestingLimit: 8,
			InterestingTokens: []string{
				"/libjpeg",
				"/include/jpeglib.h",
				"/bin/cjpeg",
			},
		})
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "arithdec-off-arithenc-off-tools-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"arithdec-on-arithenc-off-tools-off",
			"arithdec-off-arithenc-on-tools-off",
			"arithdec-off-arithenc-off-tools-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		for i := 0; i < len(singletons); i++ {
			for j := i + 1; j < len(singletons); j++ {
				left, leftOK := resultsByCombo[singletons[i]]
				right, rightOK := resultsByCombo[singletons[j]]
				if leftOK && rightOK {
					report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
						BaseLabel:       baselineCombo,
						LeftLabel:       singletons[i],
						RightLabel:      singletons[j],
						PathSampleLimit: 8,
					})
				}
			}
		}
	}
	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("libjpeg-turbo option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("libjpeg-turbo option trace records written to %s", traceLogPath)

	want := []string{
		"arithdec-off-arithenc-off-tools-off",
		"arithdec-off-arithenc-off-tools-on",
		"arithdec-off-arithenc-on-tools-off",
		"arithdec-off-arithenc-on-tools-on",
		"arithdec-on-arithenc-off-tools-off",
		"arithdec-on-arithenc-off-tools-on",
		"arithdec-on-arithenc-on-tools-off",
		"arithdec-on-arithenc-on-tools-on",
	}
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_SqliteFeatureMacros(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("sqlite option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real sqlite option classification test in short mode")
	}
	for _, tool := range []string{"cc", "ar", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real sqlite option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"dbstat":  {"dbstat-off", "dbstat-on"},
			"json1":   {"json1-off", "json1-on"},
			"rtree":   {"rtree-off", "rtree-on"},
			"soundex": {"soundex-off", "soundex-on"},
		},
		DefaultOptions: map[string][]string{
			"dbstat":  {"dbstat-off"},
			"json1":   {"json1-off"},
			"rtree":   {"rtree-off"},
			"soundex": {"soundex-off"},
		},
	}

	releaseRepo := newSqliteReleaseRepo(t, "3.45.3")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("sqlite probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/sqlite/sqlite" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "sqlite/sqlite", Version: "3.45.3"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("sqlite probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  8,
			InterestingLimit: 8,
			InterestingTokens: []string{
				"/sqlite3.c",
				"/sqlite3.o",
				"/libsqlite3.a",
				"/include/sqlite3.h",
			},
		})
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "dbstat-off-json1-off-rtree-off-soundex-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"dbstat-on-json1-off-rtree-off-soundex-off",
			"dbstat-off-json1-on-rtree-off-soundex-off",
			"dbstat-off-json1-off-rtree-on-soundex-off",
			"dbstat-off-json1-off-rtree-off-soundex-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		for i := 0; i < len(singletons); i++ {
			for j := i + 1; j < len(singletons); j++ {
				left, leftOK := resultsByCombo[singletons[i]]
				right, rightOK := resultsByCombo[singletons[j]]
				if leftOK && rightOK {
					report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
						BaseLabel:       baselineCombo,
						LeftLabel:       singletons[i],
						RightLabel:      singletons[j],
						PathSampleLimit: 8,
					})
				}
			}
		}
	}
	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("sqlite option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("sqlite option trace records written to %s", traceLogPath)

	want := []string{
		"dbstat-off-json1-off-rtree-off-soundex-off",
		"dbstat-off-json1-off-rtree-off-soundex-on",
		"dbstat-off-json1-off-rtree-on-soundex-off",
		"dbstat-off-json1-off-rtree-on-soundex-on",
		"dbstat-off-json1-on-rtree-off-soundex-off",
		"dbstat-off-json1-on-rtree-off-soundex-on",
		"dbstat-off-json1-on-rtree-on-soundex-off",
		"dbstat-off-json1-on-rtree-on-soundex-on",
		"dbstat-on-json1-off-rtree-off-soundex-off",
		"dbstat-on-json1-off-rtree-off-soundex-on",
		"dbstat-on-json1-off-rtree-on-soundex-off",
		"dbstat-on-json1-off-rtree-on-soundex-on",
		"dbstat-on-json1-on-rtree-off-soundex-off",
		"dbstat-on-json1-on-rtree-off-soundex-on",
		"dbstat-on-json1-on-rtree-on-soundex-off",
		"dbstat-on-json1-on-rtree-on-soundex-on",
	}
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_PugixmlCoreMacros(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("pugixml option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real pugixml option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "c++", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real pugixml option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"compact":      {"compact-off", "compact-on"},
			"noexceptions": {"noexceptions-off", "noexceptions-on"},
			"noxpath":      {"noxpath-off", "noxpath-on"},
			"wchar":        {"wchar-off", "wchar-on"},
		},
		DefaultOptions: map[string][]string{
			"compact":      {"compact-off"},
			"noexceptions": {"noexceptions-off"},
			"noxpath":      {"noxpath-off"},
			"wchar":        {"wchar-off"},
		},
	}

	releaseRepo := newPugixmlReleaseRepo(t, "1.15")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("pugixml probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/zeux/pugixml" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "zeux/pugixml", Version: "1.15"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("pugixml probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  8,
			InterestingLimit: 8,
			InterestingTokens: []string{
				"/src/pugixml.cpp",
				"/libpugixml",
				"/include/pugixml.hpp",
				"/include/pugiconfig.hpp",
			},
		})
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "compact-off-noexceptions-off-noxpath-off-wchar-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"compact-on-noexceptions-off-noxpath-off-wchar-off",
			"compact-off-noexceptions-on-noxpath-off-wchar-off",
			"compact-off-noexceptions-off-noxpath-on-wchar-off",
			"compact-off-noexceptions-off-noxpath-off-wchar-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		for i := 0; i < len(singletons); i++ {
			for j := i + 1; j < len(singletons); j++ {
				left, leftOK := resultsByCombo[singletons[i]]
				right, rightOK := resultsByCombo[singletons[j]]
				if leftOK && rightOK {
					report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
						BaseLabel:       baselineCombo,
						LeftLabel:       singletons[i],
						RightLabel:      singletons[j],
						PathSampleLimit: 8,
					})
				}
			}
		}
	}
	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("pugixml option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("pugixml option trace records written to %s", traceLogPath)

	want := matrix.Combinations()
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_ExpatCoreMacros(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("expat option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real expat option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "cc", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real expat option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"ge":         {"ge-off", "ge-on"},
			"large_size": {"large_size-off", "large_size-on"},
			"min_size":   {"min_size-off", "min_size-on"},
			"ns":         {"ns-off", "ns-on"},
		},
		DefaultOptions: map[string][]string{
			"ge":         {"ge-off"},
			"large_size": {"large_size-off"},
			"min_size":   {"min_size-off"},
			"ns":         {"ns-off"},
		},
	}

	releaseRepo := newExpatReleaseRepo(t, "2.6.4")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("expat probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/libexpat/libexpat" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "libexpat/libexpat", Version: "2.6.4"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("expat probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  8,
			InterestingLimit: 8,
			InterestingTokens: []string{
				"/_build/expat_config.h",
				"/_build/libexpat.a",
				"/_build/CMakeFiles/expat.dir/lib/xmlparse.c.o",
				"/_build/CMakeFiles/expat.dir/lib/xmlrole.c.o",
				"/_build/CMakeFiles/expat.dir/lib/xmltok.c.o",
				"/lib/xmlparse.c",
				"/lib/xmltok.c",
				"/libexpat",
				"/include/expat.h",
				"/include/expat_config.h",
			},
		})
		report.AddPathFacts(result.Trace, result.TraceScope, "/_build/expat_config.h")
		report.AddPathFacts(result.Trace, result.TraceScope, "/_build/libexpat.a")
		report.AddPathFacts(result.Trace, result.TraceScope, "/_build/CMakeFiles/expat.dir/lib/xmlparse.c.o")
		report.AddPathFacts(result.Trace, result.TraceScope, "/_build/CMakeFiles/expat.dir/lib/xmlrole.c.o")
		report.AddPathFacts(result.Trace, result.TraceScope, "/_build/CMakeFiles/expat.dir/lib/xmltok.c.o")
		report.AddTraceMatches(result.Trace, []string{
			"expat_config.h",
			"xmlparse.c",
			"xmlrole.c",
			"xmltok.c",
		}, 12)
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "ge-off-large_size-off-min_size-off-ns-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"ge-on-large_size-off-min_size-off-ns-off",
			"ge-off-large_size-on-min_size-off-ns-off",
			"ge-off-large_size-off-min_size-on-ns-off",
			"ge-off-large_size-off-min_size-off-ns-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		for i := 0; i < len(singletons); i++ {
			for j := i + 1; j < len(singletons); j++ {
				left, leftOK := resultsByCombo[singletons[i]]
				right, rightOK := resultsByCombo[singletons[j]]
				if leftOK && rightOK {
					report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
						BaseLabel:       baselineCombo,
						LeftLabel:       singletons[i],
						RightLabel:      singletons[j],
						PathSampleLimit: 8,
					})
				}
			}
		}
	}
	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("expat option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("expat option trace records written to %s", traceLogPath)

	want := []string{
		"ge-off-large_size-off-min_size-off-ns-off",
		"ge-off-large_size-off-min_size-off-ns-on",
		"ge-off-large_size-off-min_size-on-ns-off",
		"ge-off-large_size-on-min_size-off-ns-off",
		"ge-off-large_size-on-min_size-on-ns-off",
		"ge-on-large_size-off-min_size-off-ns-off",
	}
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_CjsonLocalesUtils(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("cJSON option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real cJSON option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "cc", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real cJSON option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"locales": {"locales-off", "locales-on"},
			"utils":   {"utils-off", "utils-on"},
		},
		DefaultOptions: map[string][]string{
			"locales": {"locales-off"},
			"utils":   {"utils-off"},
		},
	}

	releaseRepo := newCjsonReleaseRepo(t, "1.7.19")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("cJSON probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/DaveGamble/cJSON" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "DaveGamble/cJSON", Version: "1.7.19"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("cJSON probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  8,
			InterestingLimit: 8,
			InterestingTokens: []string{
				"/cJSON.c",
				"/cJSON_Utils.c",
				"/libcjson",
				"/libcjson_utils",
				"/include/cjson",
			},
		})
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "locales-off-utils-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"locales-on-utils-off",
			"locales-off-utils-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		if left, leftOK := resultsByCombo[singletons[0]]; leftOK {
			if right, rightOK := resultsByCombo[singletons[1]]; rightOK {
				report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
					BaseLabel:       baselineCombo,
					LeftLabel:       singletons[0],
					RightLabel:      singletons[1],
					PathSampleLimit: 8,
				})
			}
		}
	}

	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("cJSON option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("cJSON option trace records written to %s", traceLogPath)

	want := []string{
		"locales-off-utils-off",
		"locales-off-utils-on",
		"locales-on-utils-off",
	}
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_CAresThreadsTools(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("c-ares option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real c-ares option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "cc", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real c-ares option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"threads": {"threads-off", "threads-on"},
			"tools":   {"tools-off", "tools-on"},
		},
		DefaultOptions: map[string][]string{
			"threads": {"threads-off"},
			"tools":   {"tools-off"},
		},
	}

	releaseRepo := newCAresReleaseRepo(t, "1.34.5")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("c-ares probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/c-ares/c-ares" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "c-ares/c-ares", Version: "1.34.5"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("c-ares probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  8,
			InterestingLimit: 8,
			InterestingTokens: []string{
				"/src/lib",
				"/libcares",
				"/bin/adig",
				"/bin/ahost",
			},
		})
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "threads-off-tools-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"threads-on-tools-off",
			"threads-off-tools-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		if left, leftOK := resultsByCombo[singletons[0]]; leftOK {
			if right, rightOK := resultsByCombo[singletons[1]]; rightOK {
				report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
					BaseLabel:       baselineCombo,
					LeftLabel:       singletons[0],
					RightLabel:      singletons[1],
					PathSampleLimit: 8,
				})
			}
		}
	}

	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("c-ares option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("c-ares option trace records written to %s", traceLogPath)

	want := []string{
		"threads-off-tools-off",
		"threads-off-tools-on",
		"threads-on-tools-off",
		"threads-on-tools-on",
	}
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_LibwebpCwebpMux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("libwebp option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real libwebp option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "cc", "c++", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real libwebp option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"cwebp": {"cwebp-off", "cwebp-on"},
			"mux":   {"mux-off", "mux-on"},
		},
		DefaultOptions: map[string][]string{
			"cwebp": {"cwebp-off"},
			"mux":   {"mux-off"},
		},
	}

	releaseRepo := newLibwebpReleaseRepo(t, "1.5.0")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("libwebp probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/webmproject/libwebp" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "webmproject/libwebp", Version: "1.5.0"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("libwebp probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  8,
			InterestingLimit: 8,
			InterestingTokens: []string{
				"/src/mux/",
				"/examples/cwebp",
				"/libwebp",
				"/libwebpmux",
				"/bin/cwebp",
			},
		})
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "cwebp-off-mux-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"cwebp-on-mux-off",
			"cwebp-off-mux-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		if left, leftOK := resultsByCombo[singletons[0]]; leftOK {
			if right, rightOK := resultsByCombo[singletons[1]]; rightOK {
				report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
					BaseLabel:       baselineCombo,
					LeftLabel:       singletons[0],
					RightLabel:      singletons[1],
					PathSampleLimit: 8,
				})
			}
		}
	}

	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("libwebp option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("libwebp option trace records written to %s", traceLogPath)

	want := []string{
		"cwebp-off-mux-off",
		"cwebp-off-mux-on",
		"cwebp-on-mux-off",
	}
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_LibtiffCxxTools(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("libtiff option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real libtiff option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "cc", "c++", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real libtiff option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"cxx":   {"cxx-off", "cxx-on"},
			"tools": {"tools-off", "tools-on"},
		},
		DefaultOptions: map[string][]string{
			"cxx":   {"cxx-off"},
			"tools": {"tools-off"},
		},
	}

	releaseRepo := newLibtiffReleaseRepo(t, "4.7.1")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("libtiff probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/libsdl-org/libtiff" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "libsdl-org/libtiff", Version: "4.7.1"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}

		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("libtiff probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  8,
			InterestingLimit: 8,
			InterestingTokens: []string{
				"/libtiffxx",
				"/tools/",
				"/bin/tiff",
				"/libtiff",
			},
		})
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "cxx-off-tools-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"cxx-on-tools-off",
			"cxx-off-tools-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		if left, leftOK := resultsByCombo[singletons[0]]; leftOK {
			if right, rightOK := resultsByCombo[singletons[1]]; rightOK {
				report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
					BaseLabel:       baselineCombo,
					LeftLabel:       singletons[0],
					RightLabel:      singletons[1],
					PathSampleLimit: 8,
				})
			}
		}
	}

	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("libtiff option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("libtiff option trace records written to %s", traceLogPath)

	want := matrix.Combinations()
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_ZstdProgramsThreading(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("zstd option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real zstd option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "cc", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real zstd option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"programs":  {"programs-off", "programs-on"},
			"threading": {"threading-off", "threading-on"},
		},
		DefaultOptions: map[string][]string{
			"programs":  {"programs-off"},
			"threading": {"threading-off"},
		},
	}

	releaseRepo := newZstdReleaseRepo(t, "1.5.7")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("zstd probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/facebook/zstd" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "facebook/zstd", Version: "1.5.7"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}

		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("zstd probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  8,
			InterestingLimit: 8,
			InterestingTokens: []string{
				"/programs/",
				"/libzstd",
				"/bin/zstd",
			},
		})
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "programs-off-threading-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"programs-on-threading-off",
			"programs-off-threading-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		if left, leftOK := resultsByCombo[singletons[0]]; leftOK {
			if right, rightOK := resultsByCombo[singletons[1]]; rightOK {
				report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
					BaseLabel:       baselineCombo,
					LeftLabel:       singletons[0],
					RightLabel:      singletons[1],
					PathSampleLimit: 8,
				})
			}
		}
	}

	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("zstd option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("zstd option trace records written to %s", traceLogPath)

	want := matrix.Combinations()
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_UriparserWcharTools(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uriparser option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real uriparser option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "cc", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real uriparser option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"tools": {"tools-off", "tools-on"},
			"wchar": {"wchar-off", "wchar-on"},
		},
		DefaultOptions: map[string][]string{
			"tools": {"tools-off"},
			"wchar": {"wchar-off"},
		},
	}

	releaseRepo := newUriparserReleaseRepo(t, "0.9.8")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("uriparser probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/uriparser/uriparser" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "uriparser/uriparser", Version: "0.9.8"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}

		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("uriparser probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  8,
			InterestingLimit: 8,
			InterestingTokens: []string{
				"/uriparser",
				"/bin/uriparse",
			},
		})
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "tools-off-wchar-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"tools-on-wchar-off",
			"tools-off-wchar-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		if left, leftOK := resultsByCombo[singletons[0]]; leftOK {
			if right, rightOK := resultsByCombo[singletons[1]]; rightOK {
				report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
					BaseLabel:       baselineCombo,
					LeftLabel:       singletons[0],
					RightLabel:      singletons[1],
					PathSampleLimit: 8,
				})
			}
		}
	}

	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("uriparser option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("uriparser option trace records written to %s", traceLogPath)

	want := matrix.Combinations()
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_YamlCppContribTools(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("yaml-cpp option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real yaml-cpp option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "c++", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real yaml-cpp option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"contrib": {"contrib-off", "contrib-on"},
			"tools":   {"tools-off", "tools-on"},
		},
		DefaultOptions: map[string][]string{
			"contrib": {"contrib-off"},
			"tools":   {"tools-off"},
		},
	}

	releaseRepo := newYamlCppReleaseRepo(t, "0.9.0")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("yaml-cpp probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/jbeder/yaml-cpp" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "jbeder/yaml-cpp", Version: "0.9.0"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}

		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("yaml-cpp probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  8,
			InterestingLimit: 8,
			InterestingTokens: []string{
				"/yaml-cpp",
				"/bin/parse",
				"/bin/read",
				"/bin/sandbox",
			},
		})
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "contrib-off-tools-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"contrib-on-tools-off",
			"contrib-off-tools-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		if left, leftOK := resultsByCombo[singletons[0]]; leftOK {
			if right, rightOK := resultsByCombo[singletons[1]]; rightOK {
				report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
					BaseLabel:       baselineCombo,
					LeftLabel:       singletons[0],
					RightLabel:      singletons[1],
					PathSampleLimit: 8,
				})
			}
		}
	}

	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("yaml-cpp option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("yaml-cpp option trace records written to %s", traceLogPath)

	want := matrix.Combinations()
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

func TestE2E_Watch_RealOptionClassification_SpdlogNoexceptionsWchar(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("spdlog option classification test requires Linux")
	}
	if testing.Short() {
		t.Skip("skipping real spdlog option classification test in short mode")
	}
	for _, tool := range []string{"cmake", "c++", "strace"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found, skipping real spdlog option classification test", tool)
		}
	}

	store := setupTestStore(t)
	matrix := formula.Matrix{
		Options: map[string][]string{
			"noexceptions": {"noexceptions-off", "noexceptions-on"},
			"wchar":        {"wchar-off", "wchar-on"},
		},
		DefaultOptions: map[string][]string{
			"noexceptions": {"noexceptions-off"},
			"wchar":        {"wchar-off"},
		},
	}

	releaseRepo := newSpdlogReleaseRepo(t, "1.17.0")
	workspaceDir := t.TempDir()
	var report evaluator.DebugReport
	resultsByCombo := make(map[string]evaluator.ProbeResult)

	combos, _, err := evaluator.Watch(context.Background(), matrix, func(ctx context.Context, combo string) (evaluator.ProbeResult, error) {
		t.Logf("spdlog probe start: %s", combo)
		b := &Builder{
			store:        store,
			matrix:       combo,
			trace:        true,
			workspaceDir: workspaceDir,
			newRepo: func(repoPath string) (vcs.Repo, error) {
				if repoPath != "github.com/gabime/spdlog" {
					return nil, fmt.Errorf("unexpected repo path %q", repoPath)
				}
				return releaseRepo, nil
			},
		}

		main := module.Version{Path: "gabime/spdlog", Version: "1.17.0"}
		mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
		if err != nil {
			return evaluator.ProbeResult{}, err
		}

		savedStdout, savedStderr := os.Stdout, os.Stderr
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		defer func() {
			devNull.Close()
			os.Stdout = savedStdout
			os.Stderr = savedStderr
		}()
		os.Stdout = devNull
		os.Stderr = devNull

		results, err := b.Build(ctx, mods)
		if err != nil {
			return evaluator.ProbeResult{}, err
		}
		result := results[len(results)-1]
		t.Logf("spdlog probe done: %s (%d trace records)", combo, len(result.Trace))
		probeResult := probeResultFromBuildResult(result)
		resultsByCombo[combo] = probeResult
		report.AddCombo(combo, probeResult, evaluator.DebugSummaryOptions{
			RoleSampleLimit:  8,
			InterestingLimit: 8,
			InterestingTokens: []string{
				"/spdlog",
				"/libspdlog",
			},
		})
		return probeResult, nil
	})
	if err != nil {
		t.Fatalf("Watch() failed: %v", err)
	}

	baselineCombo := "noexceptions-off-wchar-off"
	if base, ok := resultsByCombo[baselineCombo]; ok {
		singletons := []string{
			"noexceptions-on-wchar-off",
			"noexceptions-off-wchar-on",
		}
		for _, combo := range singletons {
			probe, ok := resultsByCombo[combo]
			if !ok {
				continue
			}
			report.AddDiff(base, probe, evaluator.DebugDiffSummaryOptions{
				BaseLabel:         baselineCombo,
				ProbeLabel:        combo,
				ActionSampleLimit: 8,
			})
		}
		if left, leftOK := resultsByCombo[singletons[0]]; leftOK {
			if right, rightOK := resultsByCombo[singletons[1]]; rightOK {
				report.AddCollision(base, left, right, evaluator.DebugCollisionSummaryOptions{
					BaseLabel:       baselineCombo,
					LeftLabel:       singletons[0],
					RightLabel:      singletons[1],
					PathSampleLimit: 8,
				})
			}
		}
	}

	logPath := writeGraphLogForTest(t, report.String())
	t.Logf("spdlog option classification summary written to %s", logPath)
	traceLogPath := writeTraceLogForTest(t, formatTraceCombosForTest(resultsByCombo))
	t.Logf("spdlog option trace records written to %s", traceLogPath)

	want := []string{
		"noexceptions-off-wchar-off",
		"noexceptions-off-wchar-on",
		"noexceptions-on-wchar-off",
	}
	if !slices.Equal(combos, want) {
		t.Fatalf("Watch() combos = %v, want %v", combos, want)
	}
}

type archiveReleaseRepo struct {
	ref        string
	archiveURL string
	workDir    string

	once      sync.Once
	sourceDir string
	initErr   error
}

func newArchiveReleaseRepo(t *testing.T, ref, archiveURL string) vcs.Repo {
	t.Helper()
	return &archiveReleaseRepo{
		ref:        ref,
		archiveURL: archiveURL,
		workDir:    t.TempDir(),
	}
}

func newBoostReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, boostReleaseArchiveURL(ref))
}

func newFmtReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, fmtReleaseArchiveURL(ref))
}

func newLibjpegTurboReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, libjpegTurboReleaseArchiveURL(ref))
}

func newSqliteReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, sqliteReleaseArchiveURL(ref))
}

func newPocoReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, pocoReleaseArchiveURL(ref))
}

func newPcre2ReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, pcre2ReleaseArchiveURL(ref))
}

func newPugixmlReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, pugixmlReleaseArchiveURL(ref))
}

func newExpatReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, expatReleaseArchiveURL(ref))
}

func newCjsonReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, cjsonReleaseArchiveURL(ref))
}

func newCAresReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, cAresReleaseArchiveURL(ref))
}

func newLibwebpReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, libwebpReleaseArchiveURL(ref))
}

func newLibtiffReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, libtiffReleaseArchiveURL(ref))
}

func newZstdReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, zstdReleaseArchiveURL(ref))
}

func newYamlCppReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, yamlCppReleaseArchiveURL(ref))
}

func newSpdlogReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, spdlogReleaseArchiveURL(ref))
}

func newUriparserReleaseRepo(t *testing.T, ref string) vcs.Repo {
	t.Helper()
	return newArchiveReleaseRepo(t, ref, uriparserReleaseArchiveURL(ref))
}

func (r *archiveReleaseRepo) Tags(ctx context.Context) ([]string, error) {
	return []string{r.ref}, nil
}

func (r *archiveReleaseRepo) Latest(ctx context.Context) (string, error) {
	return r.ref, nil
}

func (r *archiveReleaseRepo) At(ref, localDir string) fs.FS {
	if err := r.prepare(context.Background()); err != nil {
		return os.DirFS(".")
	}
	return os.DirFS(r.sourceDir)
}

func (r *archiveReleaseRepo) Sync(ctx context.Context, ref, path, destDir string) error {
	if ref != "" && ref != r.ref {
		return fmt.Errorf("unsupported archive ref %q, want %q", ref, r.ref)
	}
	if path != "" {
		return fmt.Errorf("archive release repo does not support subdir sync: %q", path)
	}
	if err := r.prepare(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	return copyTreePreserveLinks(r.sourceDir, destDir)
}

func (r *archiveReleaseRepo) prepare(ctx context.Context) error {
	r.once.Do(func() {
		archiveName := "release.tar.gz"
		if strings.HasSuffix(strings.ToLower(r.archiveURL), ".zip") {
			archiveName = "release.zip"
		}
		archivePath := filepath.Join(r.workDir, archiveName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.archiveURL, nil)
		if err != nil {
			r.initErr = err
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			r.initErr = fmt.Errorf("download release archive: %w", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			r.initErr = fmt.Errorf("download release archive: unexpected HTTP %d", resp.StatusCode)
			return
		}

		f, err := os.Create(archivePath)
		if err != nil {
			r.initErr = err
			return
		}
		if _, err := io.Copy(f, resp.Body); err != nil {
			f.Close()
			r.initErr = err
			return
		}
		if err := f.Close(); err != nil {
			r.initErr = err
			return
		}

		extractDir := filepath.Join(r.workDir, "extract")
		if err := os.MkdirAll(extractDir, 0o755); err != nil {
			r.initErr = err
			return
		}
		rootDir, err := extractArchive(archivePath, extractDir)
		if err != nil {
			r.initErr = err
			return
		}
		r.sourceDir = rootDir
	})
	return r.initErr
}

func boostReleaseArchiveURL(ref string) string {
	version := strings.TrimPrefix(ref, "boost-")
	archiveVersion := strings.ReplaceAll(version, ".", "_")
	return fmt.Sprintf("https://archives.boost.io/release/%s/source/boost_%s.tar.gz", version, archiveVersion)
}

func fmtReleaseArchiveURL(ref string) string {
	return fmt.Sprintf("https://github.com/fmtlib/fmt/archive/refs/tags/%s.tar.gz", ref)
}

func libjpegTurboReleaseArchiveURL(ref string) string {
	return fmt.Sprintf("https://github.com/libjpeg-turbo/libjpeg-turbo/releases/download/%s/libjpeg-turbo-%s.tar.gz", ref, ref)
}

func sqliteReleaseArchiveURL(ref string) string {
	parts := strings.Split(ref, ".")
	if len(parts) != 3 {
		panic(fmt.Sprintf("unexpected sqlite version %q", ref))
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		panic(fmt.Sprintf("unexpected sqlite version %q", ref))
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		panic(fmt.Sprintf("unexpected sqlite version %q", ref))
	}
	return fmt.Sprintf("https://sqlite.org/2024/sqlite-amalgamation-%s%02d%02d00.zip", parts[0], minor, patch)
}

func pocoReleaseArchiveURL(ref string) string {
	return fmt.Sprintf("https://github.com/pocoproject/poco/archive/refs/tags/%s.tar.gz", ref)
}

func pcre2ReleaseArchiveURL(ref string) string {
	return fmt.Sprintf("https://github.com/PCRE2Project/pcre2/archive/refs/tags/%s.tar.gz", ref)
}

func pugixmlReleaseArchiveURL(ref string) string {
	version := strings.TrimPrefix(ref, "v")
	return fmt.Sprintf("https://github.com/zeux/pugixml/releases/download/v%s/pugixml-%s.tar.gz", version, version)
}

func expatReleaseArchiveURL(ref string) string {
	tag := ref
	if !strings.HasPrefix(tag, "R_") {
		tag = "R_" + strings.ReplaceAll(ref, ".", "_")
	}
	return fmt.Sprintf("https://github.com/libexpat/libexpat/archive/refs/tags/%s.tar.gz", tag)
}

func cjsonReleaseArchiveURL(ref string) string {
	tag := ref
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + ref
	}
	return fmt.Sprintf("https://github.com/DaveGamble/cJSON/archive/refs/tags/%s.tar.gz", tag)
}

func cAresReleaseArchiveURL(ref string) string {
	return fmt.Sprintf("https://github.com/c-ares/c-ares/releases/download/v%s/c-ares-%s.tar.gz", ref, ref)
}

func libwebpReleaseArchiveURL(ref string) string {
	tag := ref
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + ref
	}
	return fmt.Sprintf("https://github.com/webmproject/libwebp/archive/refs/tags/%s.tar.gz", tag)
}

func libtiffReleaseArchiveURL(ref string) string {
	tag := ref
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + ref
	}
	return fmt.Sprintf("https://github.com/libsdl-org/libtiff/archive/refs/tags/%s.tar.gz", tag)
}

func zstdReleaseArchiveURL(ref string) string {
	tag := ref
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + ref
	}
	return fmt.Sprintf("https://github.com/facebook/zstd/archive/refs/tags/%s.tar.gz", tag)
}

func yamlCppReleaseArchiveURL(ref string) string {
	tag := ref
	if !strings.HasPrefix(tag, "yaml-cpp-") {
		tag = "yaml-cpp-" + ref
	}
	return fmt.Sprintf("https://github.com/jbeder/yaml-cpp/archive/refs/tags/%s.tar.gz", tag)
}

func spdlogReleaseArchiveURL(ref string) string {
	tag := ref
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + ref
	}
	return fmt.Sprintf("https://github.com/gabime/spdlog/archive/refs/tags/%s.tar.gz", tag)
}

func uriparserReleaseArchiveURL(ref string) string {
	tag := ref
	if !strings.HasPrefix(tag, "uriparser-") {
		tag = "uriparser-" + ref
	}
	return fmt.Sprintf("https://github.com/uriparser/uriparser/archive/refs/tags/%s.tar.gz", tag)
}

func extractArchive(archivePath, destDir string) (string, error) {
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		return extractZip(archivePath, destDir)
	}
	return extractTarGz(archivePath, destDir)
}

func copyTreePreserveLinks(srcRoot, dstRoot string) error {
	return filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dstPath := filepath.Join(dstRoot, rel)

		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		switch mode := info.Mode(); {
		case mode.IsDir():
			return os.MkdirAll(dstPath, mode.Perm())
		case mode&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
				return err
			}
			return os.Symlink(target, dstPath)
		case mode.IsRegular():
			if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
				return err
			}
			return copyRegularFile(path, dstPath, mode.Perm())
		default:
			return nil
		}
	})
}

func copyRegularFile(srcPath, dstPath string, perm fs.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

func extractTarGz(archivePath, destDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var rootName string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		name := filepath.Clean(hdr.Name)
		if name == "." {
			continue
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader || name == "pax_global_header" {
			continue
		}
		parts := strings.Split(name, string(filepath.Separator))
		if len(parts) > 0 && rootName == "" && parts[0] != "pax_global_header" {
			rootName = parts[0]
		}
		target := filepath.Join(destDir, name)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, hdr.FileInfo().Mode().Perm()); err != nil {
				return "", err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, hdr.FileInfo().Mode().Perm())
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return "", err
			}
			if err := out.Close(); err != nil {
				return "", err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil && !os.IsExist(err) {
				return "", err
			}
		}
	}

	if rootName == "" {
		return "", fmt.Errorf("empty archive")
	}
	return filepath.Join(destDir, rootName), nil
}

func extractZip(archivePath, destDir string) (string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer zr.Close()

	var rootName string
	for _, f := range zr.File {
		name := filepath.Clean(f.Name)
		if name == "." {
			continue
		}
		parts := strings.Split(name, string(filepath.Separator))
		if len(parts) > 0 && rootName == "" {
			rootName = parts[0]
		}
		target := filepath.Join(destDir, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return "", err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return "", err
		}
		if err := out.Close(); err != nil {
			rc.Close()
			return "", err
		}
		if err := rc.Close(); err != nil {
			return "", err
		}
	}
	if rootName == "" {
		return "", fmt.Errorf("empty archive")
	}
	return filepath.Join(destDir, rootName), nil
}

func dirHasPrefix(dir, prefix string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			return true
		}
	}
	return false
}
