package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goplus/llar/internal/formula/repo"
	"github.com/goplus/llar/internal/modules"
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
func setupTestStore(t *testing.T) *repo.Store {
	t.Helper()
	storeDir := t.TempDir()
	if err := os.CopyFS(storeDir, os.DirFS(testFormulaDir)); err != nil {
		t.Fatalf("failed to copy testdata: %v", err)
	}
	return repo.New(storeDir, newMockRepo(storeDir))
}

// setupBuilder creates a Builder wired with a test Store and mock source repos.
func setupBuilder(t *testing.T, store *repo.Store, matrix string) *Builder {
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
func loadAndBuild(t *testing.T, b *Builder, store *repo.Store, main module.Version) ([]Result, []*modules.Module) {
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
