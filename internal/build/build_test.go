package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/goplus/llar/internal/formula/repo"
	"github.com/goplus/llar/internal/modules"
	"github.com/goplus/llar/internal/trace"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/mod/module"
)

// mod creates a Module with the given path, version, and direct deps.
func mod(path, version string, deps ...*modules.Module) *modules.Module {
	return &modules.Module{
		Path:    path,
		Version: version,
		Deps:    deps,
	}
}

// paths returns the "Path@Version" strings for []*modules.Module.
func paths(mods []*modules.Module) string {
	var s []string
	for _, m := range mods {
		s = append(s, fmt.Sprintf("%s@%s", m.Path, m.Version))
	}
	return strings.Join(s, " ")
}

// versions returns the "Path@Version" strings for []module.Version.
func versions(vers []module.Version) string {
	var s []string
	for _, v := range vers {
		s = append(s, fmt.Sprintf("%s@%s", v.Path, v.Version))
	}
	return strings.Join(s, " ")
}

func TestConstructBuildList(t *testing.T) {
	b := &Builder{}

	t.Run("single module", func(t *testing.T) {
		A := mod("A", "1.0.0")
		got := b.constructBuildList([]*modules.Module{A})
		if want := "A@1.0.0"; paths(got) != want {
			t.Errorf("got %q, want %q", paths(got), want)
		}
	})

	t.Run("linear chain", func(t *testing.T) {
		// A -> B -> C
		C := mod("C", "1.0.0")
		B := mod("B", "1.0.0", C)
		A := mod("A", "1.0.0", B, C) // main has all deps
		got := b.constructBuildList([]*modules.Module{A, B, C})
		if want := "C@1.0.0 B@1.0.0 A@1.0.0"; paths(got) != want {
			t.Errorf("got %q, want %q", paths(got), want)
		}
	})

	t.Run("diamond", func(t *testing.T) {
		// A -> B -> C, A -> D -> C
		C := mod("C", "1.2.0")
		B := mod("B", "1.2.0", C)
		D := mod("D", "1.0.0", C)
		A := mod("A", "1.0.0", B, C, D) // main has all deps
		got := b.constructBuildList([]*modules.Module{A, B, C, D})
		// C first (leaf), then B, then D, then A (root)
		if want := "C@1.2.0 B@1.2.0 D@1.0.0 A@1.0.0"; paths(got) != want {
			t.Errorf("got %q, want %q", paths(got), want)
		}
	})

	t.Run("deep chain", func(t *testing.T) {
		// A -> B -> C -> D -> E
		E := mod("E", "1.0.0")
		D := mod("D", "1.0.0", E)
		C := mod("C", "1.0.0", D)
		B := mod("B", "1.0.0", C)
		A := mod("A", "1.0.0", B, C, D, E)
		got := b.constructBuildList([]*modules.Module{A, B, C, D, E})
		if want := "E@1.0.0 D@1.0.0 C@1.0.0 B@1.0.0 A@1.0.0"; paths(got) != want {
			t.Errorf("got %q, want %q", paths(got), want)
		}
	})

	t.Run("empty", func(t *testing.T) {
		got := b.constructBuildList(nil)
		if len(got) != 0 {
			t.Errorf("got %d modules, want 0", len(got))
		}
	})
}

func TestResolveModTransitiveDeps(t *testing.T) {
	b := &Builder{}

	t.Run("case1: simple", func(t *testing.T) {
		// C -> D
		D := mod("D", "1.0.0")
		C := mod("C", "1.2.0", D)
		B := mod("B", "1.2.0")
		A := mod("A", "1.0.0", B, C, D)
		targets := []*modules.Module{A, B, C, D}

		got := b.resolveModTransitiveDeps(targets, C)
		if want := "D@1.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("case2: diamond", func(t *testing.T) {
		// A -> B -> C, A -> D -> C  (MVS selects C@2.0.0)
		C := mod("C", "2.0.0")
		B := mod("B", "1.2.0", C)
		D := mod("D", "1.0.0", C)
		A := mod("A", "1.0.0", B, C, D)
		targets := []*modules.Module{A, B, C, D}

		got := b.resolveModTransitiveDeps(targets, B)
		if want := "C@2.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("case3: diamond with transitive dep", func(t *testing.T) {
		// A -> B -> C, A -> D -> C -> E  (MVS selects C@2.0.0)
		E := mod("E", "1.0.0")
		C := mod("C", "2.0.0", E)
		B := mod("B", "1.2.0", C)
		D := mod("D", "1.0.0", C)
		A := mod("A", "1.0.0", B, C, D, E)
		targets := []*modules.Module{A, B, C, D, E}

		got := b.resolveModTransitiveDeps(targets, B)
		if want := "E@1.0.0 C@2.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("case4: multiple direct deps", func(t *testing.T) {
		// B -> C, B -> D  (C and D are independent leaves)
		C := mod("C", "1.1.0")
		D := mod("D", "1.0.0")
		B := mod("B", "1.2.0", C, D)
		A := mod("A", "1.0.0", B, C, D)
		targets := []*modules.Module{A, B, C, D}

		got := b.resolveModTransitiveDeps(targets, B)
		if want := "C@1.1.0 D@1.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("case5: dep ordering by topology", func(t *testing.T) {
		// B -> C -> D, B -> D
		D := mod("D", "1.2.0")
		C := mod("C", "1.1.0", D)
		B := mod("B", "1.2.0", C, D)
		A := mod("A", "1.0.0", B, C, D)
		targets := []*modules.Module{A, B, C, D}

		got := b.resolveModTransitiveDeps(targets, B)
		// D before C because C depends on D
		if want := "D@1.2.0 C@1.1.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("leaf module has no deps", func(t *testing.T) {
		D := mod("D", "1.0.0")
		A := mod("A", "1.0.0", D)
		targets := []*modules.Module{A, D}

		got := b.resolveModTransitiveDeps(targets, D)
		if len(got) != 0 {
			t.Errorf("got %q, want empty", versions(got))
		}
	})

	t.Run("deep transitive chain", func(t *testing.T) {
		// A -> B -> C -> D -> E
		E := mod("E", "1.0.0")
		D := mod("D", "1.0.0", E)
		C := mod("C", "1.0.0", D)
		B := mod("B", "1.0.0", C)
		A := mod("A", "1.0.0", B, C, D, E)
		targets := []*modules.Module{A, B, C, D, E}

		got := b.resolveModTransitiveDeps(targets, B)
		if want := "E@1.0.0 D@1.0.0 C@1.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("shared transitive dep", func(t *testing.T) {
		// A -> B -> D, A -> C -> D
		D := mod("D", "2.0.0")
		B := mod("B", "1.0.0", D)
		C := mod("C", "1.0.0", D)
		A := mod("A", "1.0.0", B, C, D)
		targets := []*modules.Module{A, B, C, D}

		// resolve for A: B and C both need D
		got := b.resolveModTransitiveDeps(targets, A)
		// D first (shared leaf), then B, then C
		if want := "D@2.0.0 B@1.0.0 C@1.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("w-shaped cross dependencies", func(t *testing.T) {
		// B -> C -> F, B -> C -> G
		// B -> D -> F, B -> D -> G
		F := mod("F", "1.0.0")
		G := mod("G", "1.0.0")
		C := mod("C", "1.0.0", F, G)
		D := mod("D", "1.0.0", F, G)
		B := mod("B", "1.0.0", C, D)
		A := mod("A", "1.0.0", B, C, D, F, G)
		targets := []*modules.Module{A, B, C, D, F, G}

		got := b.resolveModTransitiveDeps(targets, B)
		// F and G are leaves, then C and D (both depend on F,G), order follows DFS
		if want := "F@1.0.0 G@1.0.0 C@1.0.0 D@1.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("circular dependency", func(t *testing.T) {
		// B -> C -> D -> B (cycle)
		// visited breaks the cycle at B
		D := mod("D", "1.0.0")
		C := mod("C", "1.0.0", D)
		B := mod("B", "1.0.0", C)
		D.Deps = []*modules.Module{B} // close the cycle: D -> B
		A := mod("A", "1.0.0", B, C, D)
		targets := []*modules.Module{A, B, C, D}

		got := b.resolveModTransitiveDeps(targets, B)
		// B is excluded (mod itself), D -> B is a back-edge (B already visited)
		// so: visit(C) -> visit(D) -> visit(B) no-op -> append D -> append C
		if want := "D@1.0.0 C@1.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("wide fan-out", func(t *testing.T) {
		// B -> C, B -> D, B -> E  (all leaves, no inter-deps)
		C := mod("C", "1.0.0")
		D := mod("D", "1.0.0")
		E := mod("E", "1.0.0")
		B := mod("B", "1.0.0", C, D, E)
		A := mod("A", "1.0.0", B, C, D, E)
		targets := []*modules.Module{A, B, C, D, E}

		got := b.resolveModTransitiveDeps(targets, B)
		if want := "C@1.0.0 D@1.0.0 E@1.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})
}

func TestRealOptionFormulasLoad(t *testing.T) {
	store := setupTestStore(t)
	cases := []module.Version{
		{Path: "openssl/openssl", Version: "openssl-3.6.1"},
		{Path: "FFmpeg/FFmpeg", Version: "n8.0.1"},
		{Path: "opencv/opencv", Version: "4.9.0"},
		{Path: "boostorg/boost", Version: "boost-1.90.0"},
		{Path: "pocoproject/poco", Version: "poco-1.14.2-release"},
		{Path: "PCRE2Project/pcre2", Version: "pcre2-10.45"},
		{Path: "fmtlib/fmt", Version: "11.1.4"},
		{Path: "libjpeg-turbo/libjpeg-turbo", Version: "3.1.3"},
		{Path: "sqlite/sqlite", Version: "3.45.3"},
		{Path: "zeux/pugixml", Version: "1.15"},
		{Path: "libexpat/libexpat", Version: "2.6.4"},
		{Path: "DaveGamble/cJSON", Version: "1.7.19"},
		{Path: "c-ares/c-ares", Version: "1.34.5"},
		{Path: "webmproject/libwebp", Version: "1.5.0"},
		{Path: "libsdl-org/libtiff", Version: "4.7.1"},
		{Path: "facebook/zstd", Version: "1.5.7"},
		{Path: "uriparser/uriparser", Version: "0.9.8"},
		{Path: "jbeder/yaml-cpp", Version: "0.9.0"},
		{Path: "gabime/spdlog", Version: "1.17.0"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.Path+"@"+tc.Version, func(t *testing.T) {
			t.Parallel()
			_, err := modules.Load(context.Background(), tc, modules.Options{FormulaStore: store})
			if err != nil {
				t.Fatalf("modules.Load(%s@%s) failed: %v", tc.Path, tc.Version, err)
			}
		})
	}
}

// testFormulaDir and testSourceDir are resolved once at init to avoid
// issues with os.Chdir in Build() changing the working directory.
var (
	testFormulaDir string
	testSourceDir  string
)

func init() {
	testFormulaDir, _ = filepath.Abs("testdata/formulas")
	testSourceDir, _ = filepath.Abs("testdata/sources")
}

// ---------------------------------------------------------------------------
// Test helpers for Build() tests
// ---------------------------------------------------------------------------

// setupTestStore copies testdata/formulas to a temp dir and returns a Store.
// The mock VCS Sync is a no-op since data is already in place.
func setupTestStore(t *testing.T) repo.Store {
	t.Helper()
	storeDir := t.TempDir()
	if err := os.CopyFS(storeDir, os.DirFS(testFormulaDir)); err != nil {
		t.Fatalf("failed to copy testdata: %v", err)
	}
	return repo.New(storeDir, newMockRepo(storeDir))
}

// setupBuilder creates a Builder wired with a test Store and mock source repos.
func setupBuilder(t *testing.T, store repo.Store, matrix string) *Builder {
	t.Helper()
	return &Builder{
		store:        store,
		matrix:       matrix,
		workspaceDir: t.TempDir(),
		newRepo: func(repoPath string) (vcs.Repo, error) {
			modPath := strings.TrimPrefix(repoPath, "github.com/")
			return newMockRepo(filepath.Join(testSourceDir, modPath)), nil
		},
	}
}

// loadAndBuild loads modules via modules.Load then builds them.
func loadAndBuild(t *testing.T, b *Builder, store repo.Store, main module.Version) ([]Result, []*modules.Module) {
	t.Helper()
	ctx := context.Background()
	mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load(%s) failed: %v", main.Path, err)
	}
	results, err := b.Build(ctx, mods)
	if err != nil {
		t.Fatalf("Build(%s) failed: %v", main.Path, err)
	}
	return results, mods
}

func TestBuilderRunOnTest_UsesProvidedOutputDir(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux|a-on-b-on")

	mods, err := modules.Load(context.Background(), module.Version{Path: "test/mergedtest", Version: "1.0.0"}, modules.Options{
		FormulaStore: store,
	})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}

	mergedDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(mergedDir, "include"), 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	for _, name := range []string{"base.h", "a.h", "b.h"} {
		if err := os.WriteFile(filepath.Join(mergedDir, "include", name), []byte(name), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) failed: %v", name, err)
		}
	}

	if err := b.RunOnTest(context.Background(), mods, mergedDir); err != nil {
		t.Fatalf("RunOnTest() unexpected error: %v", err)
	}
}

func TestBuilderRunOnTest_FailsWhenMergedOutputMissingExpectedFile(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux|a-on-b-on")

	mods, err := modules.Load(context.Background(), module.Version{Path: "test/mergedtest", Version: "1.0.0"}, modules.Options{
		FormulaStore: store,
	})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}

	mergedDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(mergedDir, "include"), 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	for _, name := range []string{"base.h", "a.h"} {
		if err := os.WriteFile(filepath.Join(mergedDir, "include", name), []byte(name), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) failed: %v", name, err)
		}
	}

	err = b.RunOnTest(context.Background(), mods, mergedDir)
	if err == nil {
		t.Fatal("RunOnTest() expected error, got nil")
	}
	var onTestErr *OnTestFailureError
	if !errors.As(err, &onTestErr) {
		t.Fatalf("RunOnTest() error = %T, want *OnTestFailureError", err)
	}
}

func TestBuild_RestoresWorkingDirectory(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}

	_, _ = loadAndBuild(t, b, store, module.Version{Path: "test/liba", Version: "1.0.0"})

	gotDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() after Build failed: %v", err)
	}
	if gotDir != origDir {
		t.Fatalf("cwd after Build = %q, want %q", gotDir, origDir)
	}
}

// findResult returns the Result for a given module path.
// Results are in constructBuildList order, so we match via build order.
func findResult(results []Result, b *Builder, mods []*modules.Module, path string) (Result, bool) {
	buildOrder := b.constructBuildList(mods)
	for i, m := range buildOrder {
		if m.Path == path && i < len(results) {
			return results[i], true
		}
	}
	return Result{}, false
}

// ---------------------------------------------------------------------------
// NewBuilder tests
// ---------------------------------------------------------------------------

func TestNewBuilder(t *testing.T) {
	t.Run("with workspace dir", func(t *testing.T) {
		tmpDir := t.TempDir()
		store := setupTestStore(t)
		b, err := NewBuilder(Options{
			Store:        store,
			MatrixStr:    "amd64-linux",
			WorkspaceDir: tmpDir,
		})
		if err != nil {
			t.Fatalf("NewBuilder() error = %v", err)
		}
		if b.workspaceDir != tmpDir {
			t.Errorf("workspaceDir = %q, want %q", b.workspaceDir, tmpDir)
		}
		if b.matrix != "amd64-linux" {
			t.Errorf("matrix = %q, want %q", b.matrix, "amd64-linux")
		}
		if b.store != store {
			t.Error("store not set correctly")
		}
		if b.newRepo == nil {
			t.Error("newRepo should be set to default")
		}
	})

	t.Run("default workspace dir", func(t *testing.T) {
		b, err := NewBuilder(Options{
			MatrixStr: "arm64-darwin",
		})
		if err != nil {
			t.Fatalf("NewBuilder() error = %v", err)
		}
		if b.workspaceDir == "" {
			t.Error("workspaceDir should not be empty")
		}
		// Verify the default workspace directory was created
		if _, err := os.Stat(b.workspaceDir); err != nil {
			t.Errorf("default workspace dir not created: %v", err)
		}
		if !strings.Contains(b.workspaceDir, ".llar") {
			t.Errorf("workspace dir %q doesn't contain .llar", b.workspaceDir)
		}
	})
}

func TestBuild_TraceCapturesOnlyMainModule(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")
	b.trace = true

	oldCapture := captureOnBuildTrace
	defer func() {
		captureOnBuildTrace = oldCapture
	}()

	var calls int
	var gotOpts trace.CaptureOptions
	wantTrace := []trace.Record{{Argv: []string{"trace", "main"}}}
	wantEvents := []trace.Event{{Seq: 1, Kind: trace.EventExec, Argv: []string{"trace", "main"}}}
	captureOnBuildTrace = func(ctx context.Context, opts trace.CaptureOptions, run func() error) (trace.CaptureResult, error) {
		calls++
		gotOpts = opts
		if err := run(); err != nil {
			return trace.CaptureResult{}, err
		}
		return trace.CaptureResult{Records: wantTrace, Events: wantEvents}, nil
	}

	main := module.Version{Path: "test/depresult", Version: "1.0.0"}
	results, mods := loadAndBuild(t, b, store, main)

	if calls != 1 {
		t.Fatalf("captureOnBuildTrace call count = %d, want 1", calls)
	}

	root, ok := findResult(results, b, mods, "test/depresult")
	if !ok {
		t.Fatal("missing result for test/depresult")
	}
	if !reflect.DeepEqual(root.Trace, wantTrace) {
		t.Fatalf("root trace = %#v, want %#v", root.Trace, wantTrace)
	}
	if !reflect.DeepEqual(root.TraceEvents, wantEvents) {
		t.Fatalf("root trace events = %#v, want %#v", root.TraceEvents, wantEvents)
	}
	if !root.TraceDiagnostics.Trusted() {
		t.Fatalf("root trace diagnostics = %#v, want trusted", root.TraceDiagnostics)
	}
	if !root.ReplayReady {
		t.Fatal("root ReplayReady = false, want true")
	}
	if gotOpts.RootCwd == "" {
		t.Fatal("RootCwd should not be empty")
	}
	if len(gotOpts.KeepRoots) < 2 {
		t.Fatalf("KeepRoots = %#v, want source root plus install roots", gotOpts.KeepRoots)
	}
	if !strings.Contains(root.TraceScope.SourceRoot, filepath.Join(b.workspaceDir, ".trace-src")) {
		t.Fatalf("trace source root = %q, want under %q", root.TraceScope.SourceRoot, filepath.Join(b.workspaceDir, ".trace-src"))
	}
	if _, err := os.Stat(root.TraceScope.SourceRoot); err != nil {
		t.Fatalf("trace source root %q should exist: %v", root.TraceScope.SourceRoot, err)
	}

	dep, ok := findResult(results, b, mods, "test/liba")
	if !ok {
		t.Fatal("missing result for test/liba")
	}
	if len(dep.Trace) != 0 {
		t.Fatalf("dependency trace = %#v, want empty", dep.Trace)
	}
	if len(dep.TraceEvents) != 0 {
		t.Fatalf("dependency trace events = %#v, want empty", dep.TraceEvents)
	}
	if dep.ReplayReady {
		t.Fatal("dependency ReplayReady = true, want false")
	}
}

func TestBuild_TraceBypassesMainModuleCache(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")
	b.trace = true

	cache := &buildCache{}
	cache.set("1.0.0", "amd64-linux", &buildEntry{
		Metadata:  "-lPRECACHED",
		BuildTime: time.Now(),
	})
	if err := b.saveCache("test/liba", cache); err != nil {
		t.Fatalf("saveCache() failed: %v", err)
	}

	oldCapture := captureOnBuildTrace
	defer func() {
		captureOnBuildTrace = oldCapture
	}()
	captureOnBuildTrace = func(ctx context.Context, opts trace.CaptureOptions, run func() error) (trace.CaptureResult, error) {
		if err := run(); err != nil {
			return trace.CaptureResult{}, err
		}
		return trace.CaptureResult{
			Records: []trace.Record{{Argv: []string{"trace", "cache-bypass"}}},
			Events:  []trace.Event{{Seq: 1, Kind: trace.EventExec, Argv: []string{"trace", "cache-bypass"}}},
		}, nil
	}

	main := module.Version{Path: "test/liba", Version: "1.0.0"}
	results, _ := loadAndBuild(t, b, store, main)

	if results[0].Metadata != "-lA" {
		t.Fatalf("metadata = %q, want %q", results[0].Metadata, "-lA")
	}
	if len(results[0].Trace) != 1 {
		t.Fatalf("trace len = %d, want 1", len(results[0].Trace))
	}
	if len(results[0].TraceEvents) != 1 {
		t.Fatalf("trace events len = %d, want 1", len(results[0].TraceEvents))
	}
}

func TestBuild_TraceInfersNestedBuildRootFromTrace(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")
	b.trace = true

	oldCapture := captureOnBuildTrace
	defer func() {
		captureOnBuildTrace = oldCapture
	}()
	var gotOpts trace.CaptureOptions
	captureOnBuildTrace = func(ctx context.Context, opts trace.CaptureOptions, run func() error) (trace.CaptureResult, error) {
		gotOpts = opts
		if err := run(); err != nil {
			return trace.CaptureResult{}, err
		}
		nestedBuild := filepath.Join(opts.RootCwd, "expat", "_build")
		return trace.CaptureResult{
			Records: []trace.Record{{
				PID:       1,
				ParentPID: 0,
				Argv:      []string{"cmake", "-S", filepath.Join(opts.RootCwd, "expat"), "-B", nestedBuild},
				Cwd:       filepath.Join(opts.RootCwd, "expat"),
				Changes: []string{
					filepath.Join(nestedBuild, "CMakeCache.txt"),
					filepath.Join(nestedBuild, "libexpat.a"),
				},
			}},
		}, nil
	}

	main := module.Version{Path: "test/liba", Version: "1.0.0"}
	results, mods := loadAndBuild(t, b, store, main)

	root, ok := findResult(results, b, mods, "test/liba")
	if !ok {
		t.Fatal("missing result for test/liba")
	}
	wantBuildRoot := filepath.Join(gotOpts.RootCwd, "expat", "_build")
	if root.TraceScope.BuildRoot != wantBuildRoot {
		t.Fatalf("trace build root = %q, want %q", root.TraceScope.BuildRoot, wantBuildRoot)
	}
}

// ---------------------------------------------------------------------------
// Build error path tests
// ---------------------------------------------------------------------------

func TestBuild_EmptyTargets(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	results, err := b.Build(context.Background(), nil)
	if err != nil {
		t.Fatalf("Build(nil) error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestBuild_RepoCreationError(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	wantErr := errors.New("repo creation failed")
	b.newRepo = func(repoPath string) (vcs.Repo, error) {
		return nil, wantErr
	}

	main := module.Version{Path: "test/liba", Version: "1.0.0"}
	ctx := context.Background()
	mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}
	_, err = b.Build(ctx, mods)
	if err == nil {
		t.Fatal("Build() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "repo creation failed") {
		t.Errorf("error = %v, want it to contain %q", err, "repo creation failed")
	}
}

func TestBuild_SyncError(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	wantErr := errors.New("sync failed")
	b.newRepo = func(repoPath string) (vcs.Repo, error) {
		return &errorRepo{syncErr: wantErr}, nil
	}

	main := module.Version{Path: "test/liba", Version: "1.0.0"}
	ctx := context.Background()
	mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}
	_, err = b.Build(ctx, mods)
	if err == nil {
		t.Fatal("Build() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "sync failed") {
		t.Errorf("error = %v, want it to contain %q", err, "sync failed")
	}
}

// ---------------------------------------------------------------------------
// Build cache detail tests (not covered by e2e)
// ---------------------------------------------------------------------------

func TestBuild_PrePopulatedCache(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	// Pre-populate cache with a different metadata value
	cache := &buildCache{}
	cache.set("1.0.0", "amd64-linux", &buildEntry{
		Metadata:  "-lPRECACHED",
		BuildTime: time.Now(),
	})
	if err := b.saveCache("test/liba", cache); err != nil {
		t.Fatalf("saveCache() failed: %v", err)
	}

	main := module.Version{Path: "test/liba", Version: "1.0.0"}
	results, _ := loadAndBuild(t, b, store, main)

	// Should return pre-cached metadata, not the formula-defined "-lA"
	if results[0].Metadata != "-lPRECACHED" {
		t.Errorf("metadata = %q, want %q (from pre-populated cache)", results[0].Metadata, "-lPRECACHED")
	}
}

func TestBuild_CacheWrittenCorrectly(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/liba", Version: "1.0.0"}
	loadAndBuild(t, b, store, main)

	// Load the cache file and verify its content
	cache, err := b.loadCache("test/liba")
	if err != nil {
		t.Fatalf("loadCache() failed: %v", err)
	}
	entry, ok := cache.get("1.0.0", "amd64-linux")
	if !ok {
		t.Fatal("cache entry not found for 1.0.0-amd64-linux")
	}
	if entry.Metadata != "-lA" {
		t.Errorf("cached metadata = %q, want %q", entry.Metadata, "-lA")
	}
	if entry.BuildTime.IsZero() {
		t.Error("cache build time should not be zero")
	}

	// Verify it's valid JSON on disk
	cacheDir, _ := b.cacheDir("test/liba")
	data, err := os.ReadFile(filepath.Join(cacheDir, cacheFile))
	if err != nil {
		t.Fatalf("ReadFile() failed: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("cache file is not valid JSON: %v", err)
	}
}

func TestBuild_CacheAccumulatesMultipleVersions(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/liba", Version: "1.0.0"}
	loadAndBuild(t, b, store, main)

	// Manually add another version to the cache
	cache, _ := b.loadCache("test/liba")
	cache.set("2.0.0", "amd64-linux", &buildEntry{
		Metadata:  "-lA2",
		BuildTime: time.Now(),
	})
	b.saveCache("test/liba", cache)

	// Build again - should still hit cache for 1.0.0
	results, _ := loadAndBuild(t, b, store, main)
	if results[0].Metadata != "-lA" {
		t.Errorf("metadata = %q, want %q", results[0].Metadata, "-lA")
	}

	// Verify both entries exist
	cache, _ = b.loadCache("test/liba")
	if _, ok := cache.get("1.0.0", "amd64-linux"); !ok {
		t.Error("cache miss for 1.0.0")
	}
	if _, ok := cache.get("2.0.0", "amd64-linux"); !ok {
		t.Error("cache miss for 2.0.0")
	}
}

// ---------------------------------------------------------------------------
// Environment and workspace tests (not covered by e2e)
// ---------------------------------------------------------------------------

func TestBuild_EnvRestoration(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	const envKey = "LLAR_BUILD_TEST_ENV"
	os.Setenv(envKey, "original")
	defer os.Unsetenv(envKey)

	main := module.Version{Path: "test/liba", Version: "1.0.0"}
	loadAndBuild(t, b, store, main)

	if got := os.Getenv(envKey); got != "original" {
		t.Errorf("env %s = %q after Build, want %q (restored)", envKey, got, "original")
	}
}

func TestBuild_EnvRestoration_AfterError(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	const envKey = "LLAR_BUILD_TEST_ENV_ERR"
	os.Setenv(envKey, "before_error")
	defer os.Unsetenv(envKey)

	main := module.Version{Path: "test/errmod", Version: "1.0.0"}
	ctx := context.Background()
	mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}
	_, _ = b.Build(ctx, mods)

	// Environment should still be restored even after error
	if got := os.Getenv(envKey); got != "before_error" {
		t.Errorf("env %s = %q after failed Build, want %q", envKey, got, "before_error")
	}
}

func TestBuild_InstallDirConvention(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/liba", Version: "1.0.0"}
	loadAndBuild(t, b, store, main)

	installDir, _ := b.installDir("test/liba", "1.0.0")

	// Verify the path follows workspace/<escaped>@<version>-<matrix>
	rel, err := filepath.Rel(b.workspaceDir, installDir)
	if err != nil {
		t.Fatalf("installDir not under workspace: %v", err)
	}
	want := filepath.Join("test", "liba@1.0.0-amd64-linux")
	if rel != want {
		t.Errorf("installDir rel = %q, want %q", rel, want)
	}

	// Verify directory was created
	if _, err := os.Stat(installDir); err != nil {
		t.Errorf("installDir not created: %v", err)
	}
}

// ---------------------------------------------------------------------------
// constructBuildList additional tests
// ---------------------------------------------------------------------------

func TestConstructBuildList_DuplicatePaths(t *testing.T) {
	b := &Builder{}

	// Modules with same path at different versions (MVS should have resolved this,
	// but constructBuildList should handle it gracefully)
	C := mod("C", "1.0.0")
	B := mod("B", "1.0.0", C)
	A := mod("A", "1.0.0", B, C)
	got := b.constructBuildList([]*modules.Module{A, B, C})

	// Each path should appear exactly once
	seen := make(map[string]bool)
	for _, m := range got {
		if seen[m.Path] {
			t.Errorf("duplicate path in build list: %s", m.Path)
		}
		seen[m.Path] = true
	}
	if len(got) != 3 {
		t.Errorf("got %d modules, want 3", len(got))
	}
}

func TestConstructBuildList_DepNotInTargets(t *testing.T) {
	b := &Builder{}

	// B depends on C, but C is not in targets
	C := mod("C", "1.0.0")
	B := mod("B", "1.0.0", C)
	A := mod("A", "1.0.0", B)
	got := b.constructBuildList([]*modules.Module{A, B})
	// C should be skipped since it's not in targets
	if want := "B@1.0.0 A@1.0.0"; paths(got) != want {
		t.Errorf("got %q, want %q", paths(got), want)
	}
}

// ---------------------------------------------------------------------------
// resolveModTransitiveDeps additional tests
// ---------------------------------------------------------------------------

func TestResolveModTransitiveDeps_ModNotInTargets(t *testing.T) {
	b := &Builder{}

	// mod's deps reference a module not in targets
	D := mod("D", "1.0.0")
	C := mod("C", "1.0.0", D)
	B := mod("B", "1.0.0", C)
	A := mod("A", "1.0.0", B, C)
	targets := []*modules.Module{A, B, C} // D is NOT in targets

	got := b.resolveModTransitiveDeps(targets, B)
	// C is reachable, D is not in targets so skipped
	if want := "C@1.0.0"; versions(got) != want {
		t.Errorf("got %q, want %q", versions(got), want)
	}
}

func TestCollectTraceInputDigestsIncludesBuildOutputs(t *testing.T) {
	buildRoot := t.TempDir()
	generatedHeader := filepath.Join(buildRoot, "generated.h")
	objectFile := filepath.Join(buildRoot, "core.o")
	sourceFile := filepath.Join(buildRoot, "core.c")

	if err := os.WriteFile(sourceFile, []byte("int core(void) { return 0; }\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(source): %v", err)
	}
	if err := os.WriteFile(generatedHeader, []byte("#define FLAG 1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(generatedHeader): %v", err)
	}
	if err := os.WriteFile(objectFile, []byte("object-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile(objectFile): %v", err)
	}

	records := []trace.Record{
		{
			Cwd:     buildRoot,
			Argv:    []string{"generator"},
			Inputs:  []string{sourceFile},
			Changes: []string{generatedHeader},
		},
		{
			Cwd:     buildRoot,
			Argv:    []string{"cc", "-c", sourceFile, "-o", objectFile},
			Inputs:  []string{sourceFile, generatedHeader},
			Changes: []string{objectFile},
		},
	}

	got := collectTraceInputDigests(records, trace.Scope{BuildRoot: buildRoot})
	if got == nil {
		t.Fatal("collectTraceInputDigests() = nil, want digests")
	}
	for _, path := range []string{generatedHeader, objectFile} {
		if got[path] == "" {
			t.Fatalf("collectTraceInputDigests() missing digest for %q: %#v", path, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Mock types for error testing
// ---------------------------------------------------------------------------

// errorRepo implements vcs.Repo and returns configurable errors.
type errorRepo struct {
	syncErr error
}

func (e *errorRepo) Tags(ctx context.Context) ([]string, error) {
	return []string{"v1.0.0"}, nil
}

func (e *errorRepo) Latest(ctx context.Context) (string, error) {
	return "abc123", nil
}

func (e *errorRepo) At(ref, localDir string) fs.FS {
	return os.DirFS(".")
}

func (e *errorRepo) Sync(ctx context.Context, ref, path, destDir string) error {
	return e.syncErr
}
