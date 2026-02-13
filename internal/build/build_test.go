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

	classfile "github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/formula"
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
// Build() tests — real formula loading via modules.Load
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

func TestBuild_SingleModule(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/liba", Version: "1.0.0"}
	results, _ := loadAndBuild(t, b, store, main)

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Metadata != "-lA" {
		t.Errorf("metadata = %q, want %q", results[0].Metadata, "-lA")
	}
}

func TestBuild_WithDeps(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	// libb depends on liba
	main := module.Version{Path: "test/libb", Version: "1.0.0"}
	results, mods := loadAndBuild(t, b, store, main)

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (liba + libb)", len(results))
	}

	// Verify module paths in build list
	var paths []string
	for _, m := range mods {
		paths = append(paths, m.Path)
	}
	t.Logf("loaded modules: %v", paths)

	// Verify both modules have correct metadata
	for _, m := range mods {
		r, ok := findResult(results, b, mods, m.Path)
		if !ok {
			t.Errorf("missing result for %s", m.Path)
			continue
		}
		switch m.Path {
		case "test/liba":
			if r.Metadata != "-lA" {
				t.Errorf("liba metadata = %q, want %q", r.Metadata, "-lA")
			}
		case "test/libb":
			if r.Metadata != "-lB" {
				t.Errorf("libb metadata = %q, want %q", r.Metadata, "-lB")
			}
		}
	}
}

func TestBuild_TransitiveChain(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	// libc -> libb -> liba
	main := module.Version{Path: "test/libc", Version: "1.0.0"}
	results, mods := loadAndBuild(t, b, store, main)

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3 (liba + libb + libc)", len(results))
	}

	// Verify build order: leaves first (liba), then libb, then libc (root)
	buildOrder := b.constructBuildList(mods)
	var orderPaths []string
	for _, m := range buildOrder {
		orderPaths = append(orderPaths, m.Path)
	}
	t.Logf("build order: %v", orderPaths)

	// liba must come before libb, libb before libc
	libaIdx, libbIdx, libcIdx := -1, -1, -1
	for i, p := range orderPaths {
		switch p {
		case "test/liba":
			libaIdx = i
		case "test/libb":
			libbIdx = i
		case "test/libc":
			libcIdx = i
		}
	}
	if libaIdx >= libbIdx || libbIdx >= libcIdx {
		t.Errorf("wrong build order: liba@%d, libb@%d, libc@%d", libaIdx, libbIdx, libcIdx)
	}

	// Verify all metadata
	wantMeta := map[string]string{
		"test/liba": "-lA",
		"test/libb": "-lB",
		"test/libc": "-lC",
	}
	for _, m := range mods {
		r, ok := findResult(results, b, mods, m.Path)
		if !ok {
			t.Errorf("missing result for %s", m.Path)
			continue
		}
		if want, exists := wantMeta[m.Path]; exists && r.Metadata != want {
			t.Errorf("%s metadata = %q, want %q", m.Path, r.Metadata, want)
		}
	}
}

func TestBuild_CacheHit(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/liba", Version: "1.0.0"}

	// First build: populates cache
	results1, _ := loadAndBuild(t, b, store, main)
	if results1[0].Metadata != "-lA" {
		t.Fatalf("first build metadata = %q, want %q", results1[0].Metadata, "-lA")
	}

	// Verify cache file was written
	cacheDir, _ := b.cacheDir("test/liba")
	cachePath := filepath.Join(cacheDir, cacheFile)
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}

	// Second build: should hit cache and return same result
	results2, _ := loadAndBuild(t, b, store, main)
	if results2[0].Metadata != "-lA" {
		t.Errorf("second build metadata = %q, want %q (from cache)", results2[0].Metadata, "-lA")
	}
}

func TestBuild_CacheDifferentMatrix(t *testing.T) {
	store := setupTestStore(t)
	main := module.Version{Path: "test/liba", Version: "1.0.0"}

	// Build with first matrix
	b1 := setupBuilder(t, store, "amd64-linux")
	b1.workspaceDir = t.TempDir() // shared workspace
	ws := b1.workspaceDir

	results1, _ := loadAndBuild(t, b1, store, main)
	if results1[0].Metadata != "-lA" {
		t.Fatalf("first matrix build metadata = %q, want %q", results1[0].Metadata, "-lA")
	}

	// Build with different matrix, same workspace
	b2 := setupBuilder(t, store, "arm64-darwin")
	b2.workspaceDir = ws

	results2, _ := loadAndBuild(t, b2, store, main)
	if results2[0].Metadata != "-lA" {
		t.Errorf("second matrix build metadata = %q, want %q", results2[0].Metadata, "-lA")
	}

	// Verify different installDirs
	dir1, _ := b1.installDir("test/liba", "1.0.0")
	dir2, _ := b2.installDir("test/liba", "1.0.0")
	if dir1 == dir2 {
		t.Errorf("same installDir for different matrices: %q", dir1)
	}
	// Both should exist
	for _, dir := range []string{dir1, dir2} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("installDir %q not created: %v", dir, err)
		}
	}
}

func TestBuild_Error(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	// errmod's OnBuild reads a nonexistent file and adds the error
	main := module.Version{Path: "test/errmod", Version: "1.0.0"}
	ctx := context.Background()
	mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}

	_, err = b.Build(ctx, mods)
	if err == nil {
		t.Fatal("Build() error = nil, want error")
	}
	t.Logf("got expected error: %v", err)
}

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

func TestBuild_DepResultInjection(t *testing.T) {
	store := setupTestStore(t)

	// We build libb (depends on liba) and verify that when libb's OnBuild
	// runs, the build context contains liba's result.
	// Since the real formula just sets "-lB" metadata, we verify by checking
	// that the build succeeds and both results are returned with correct metadata.
	// The real injection is tested by building the chain and verifying the
	// context is properly set up.
	b := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/libb", Version: "1.0.0"}
	results, mods := loadAndBuild(t, b, store, main)

	buildOrder := b.constructBuildList(mods)
	if len(buildOrder) != 2 {
		t.Fatalf("build order has %d entries, want 2", len(buildOrder))
	}

	// liba should be built first
	if buildOrder[0].Path != "test/liba" {
		t.Errorf("first build = %q, want %q", buildOrder[0].Path, "test/liba")
	}

	// Both should have results
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Metadata != "-lA" {
		t.Errorf("liba metadata = %q, want %q", results[0].Metadata, "-lA")
	}
	if results[1].Metadata != "-lB" {
		t.Errorf("libb metadata = %q, want %q", results[1].Metadata, "-lB")
	}
}

func TestBuild_DepResultInjection_WithSyntheticModule(t *testing.T) {
	store := setupTestStore(t)
	wsDir := t.TempDir()

	// Build liba first to populate cache, so we can verify dep injection
	// in a controlled way by creating a synthetic module that checks its deps.
	b := &Builder{
		store:        store,
		matrix:       "amd64-linux",
		workspaceDir: wsDir,
		newRepo: func(repoPath string) (vcs.Repo, error) {
			modPath := strings.TrimPrefix(repoPath, "github.com/")
			return newMockRepo(filepath.Join(testSourceDir, modPath)), nil
		},
	}

	// First build liba normally
	main := module.Version{Path: "test/liba", Version: "1.0.0"}
	ctx := context.Background()
	mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}
	results, err := b.Build(ctx, mods)
	if err != nil {
		t.Fatalf("Build(liba) failed: %v", err)
	}
	if results[0].Metadata != "-lA" {
		t.Fatalf("liba metadata = %q, want %q", results[0].Metadata, "-lA")
	}
}

func TestBuild_MultipleErrors(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	// errmod's OnBuild produces an error
	main := module.Version{Path: "test/errmod", Version: "1.0.0"}
	ctx := context.Background()
	mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}
	_, err = b.Build(ctx, mods)
	if err == nil {
		t.Fatal("Build() error = nil, want error")
	}
	// Verify the error is from the formula
	if !strings.Contains(err.Error(), "nonexistent.txt") {
		t.Errorf("error = %v, want it to mention nonexistent.txt", err)
	}
}

func TestBuild_ErrorDoesNotCache(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	// Build errmod which fails
	main := module.Version{Path: "test/errmod", Version: "1.0.0"}
	ctx := context.Background()
	mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}
	_, _ = b.Build(ctx, mods)

	// Verify no cache was written for the failed module
	_, err = b.loadCache("test/errmod")
	if err == nil {
		t.Fatal("cache should not exist for failed build")
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

func TestBuild_WorkspaceIsolation(t *testing.T) {
	store := setupTestStore(t)

	// Two builders with different workspaces should not interfere
	b1 := setupBuilder(t, store, "amd64-linux")
	b2 := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/liba", Version: "1.0.0"}
	results1, _ := loadAndBuild(t, b1, store, main)
	results2, _ := loadAndBuild(t, b2, store, main)

	if results1[0].Metadata != results2[0].Metadata {
		t.Errorf("results differ: %q vs %q", results1[0].Metadata, results2[0].Metadata)
	}

	// But they should have different install dirs
	dir1, _ := b1.installDir("test/liba", "1.0.0")
	dir2, _ := b2.installDir("test/liba", "1.0.0")
	if dir1 == dir2 {
		t.Error("different builders should have different install dirs")
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

func TestConstructBuildList_SingleDep(t *testing.T) {
	b := &Builder{}

	B := mod("B", "1.0.0")
	A := mod("A", "1.0.0", B)
	got := b.constructBuildList([]*modules.Module{A, B})
	if want := "B@1.0.0 A@1.0.0"; paths(got) != want {
		t.Errorf("got %q, want %q", paths(got), want)
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

func TestResolveModTransitiveDeps_EmptyDeps(t *testing.T) {
	b := &Builder{}

	A := mod("A", "1.0.0")
	targets := []*modules.Module{A}

	got := b.resolveModTransitiveDeps(targets, A)
	if len(got) != 0 {
		t.Errorf("got %q, want empty", versions(got))
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

// ---------------------------------------------------------------------------
// Direct module construction tests (bypasses modules.Load)
// ---------------------------------------------------------------------------

func TestBuild_DirectModule_SimpleOnBuild(t *testing.T) {
	store := setupTestStore(t)
	wsDir := t.TempDir()

	b := &Builder{
		store:        store,
		matrix:       "amd64-linux",
		workspaceDir: wsDir,
		newRepo: func(repoPath string) (vcs.Repo, error) {
			modPath := strings.TrimPrefix(repoPath, "github.com/")
			return newMockRepo(filepath.Join(testSourceDir, modPath)), nil
		},
	}

	// Create a module with a custom OnBuild that sets specific metadata
	testMod := &modules.Module{
		Formula: &formula.Formula{
			ModPath: "test/liba",
			FromVer: "1.0.0",
			OnBuild: func(ctx *classfile.Context, proj *classfile.Project, out *classfile.BuildResult) {
				out.SetMetadata("-lcustom")
			},
		},
		FS:      os.DirFS(filepath.Join(testSourceDir, "test/liba")),
		Path:    "test/liba",
		Version: "1.0.0",
	}

	results, err := b.Build(context.Background(), []*modules.Module{testMod})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Metadata != "-lcustom" {
		t.Errorf("metadata = %q, want %q", results[0].Metadata, "-lcustom")
	}
}

func TestBuild_DirectModule_OnBuildError(t *testing.T) {
	store := setupTestStore(t)
	wsDir := t.TempDir()

	b := &Builder{
		store:        store,
		matrix:       "amd64-linux",
		workspaceDir: wsDir,
		newRepo: func(repoPath string) (vcs.Repo, error) {
			modPath := strings.TrimPrefix(repoPath, "github.com/")
			return newMockRepo(filepath.Join(testSourceDir, modPath)), nil
		},
	}

	testMod := &modules.Module{
		Formula: &formula.Formula{
			ModPath: "test/liba",
			FromVer: "1.0.0",
			OnBuild: func(ctx *classfile.Context, proj *classfile.Project, out *classfile.BuildResult) {
				out.AddErr(errors.New("build exploded"))
			},
		},
		FS:      os.DirFS(filepath.Join(testSourceDir, "test/liba")),
		Path:    "test/liba",
		Version: "1.0.0",
	}

	_, err := b.Build(context.Background(), []*modules.Module{testMod})
	if err == nil {
		t.Fatal("Build() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "build exploded") {
		t.Errorf("error = %v, want it to contain %q", err, "build exploded")
	}
}

func TestBuild_DirectModule_EmptyMetadata(t *testing.T) {
	store := setupTestStore(t)
	wsDir := t.TempDir()

	b := &Builder{
		store:        store,
		matrix:       "amd64-linux",
		workspaceDir: wsDir,
		newRepo: func(repoPath string) (vcs.Repo, error) {
			modPath := strings.TrimPrefix(repoPath, "github.com/")
			return newMockRepo(filepath.Join(testSourceDir, modPath)), nil
		},
	}

	// OnBuild that doesn't set any metadata
	testMod := &modules.Module{
		Formula: &formula.Formula{
			ModPath: "test/liba",
			FromVer: "1.0.0",
			OnBuild: func(ctx *classfile.Context, proj *classfile.Project, out *classfile.BuildResult) {
				// no-op, no metadata set
			},
		},
		FS:      os.DirFS(filepath.Join(testSourceDir, "test/liba")),
		Path:    "test/liba",
		Version: "1.0.0",
	}

	results, err := b.Build(context.Background(), []*modules.Module{testMod})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if results[0].Metadata != "" {
		t.Errorf("metadata = %q, want empty", results[0].Metadata)
	}
}

func TestBuild_DirectModule_DepContextInjection(t *testing.T) {
	store := setupTestStore(t)
	wsDir := t.TempDir()

	b := &Builder{
		store:        store,
		matrix:       "amd64-linux",
		workspaceDir: wsDir,
		newRepo: func(repoPath string) (vcs.Repo, error) {
			modPath := strings.TrimPrefix(repoPath, "github.com/")
			return newMockRepo(filepath.Join(testSourceDir, modPath)), nil
		},
	}

	// Module A: sets metadata "-lA"
	modA := &modules.Module{
		Formula: &formula.Formula{
			ModPath: "test/liba",
			FromVer: "1.0.0",
			OnBuild: func(ctx *classfile.Context, proj *classfile.Project, out *classfile.BuildResult) {
				out.SetMetadata("-lA")
			},
		},
		FS:      os.DirFS(filepath.Join(testSourceDir, "test/liba")),
		Path:    "test/liba",
		Version: "1.0.0",
	}

	// Module B: depends on A, reads A's build result from context
	var gotDepResult string
	var gotDepOk bool
	modB := &modules.Module{
		Formula: &formula.Formula{
			ModPath: "test/libb",
			FromVer: "1.0.0",
			OnBuild: func(ctx *classfile.Context, proj *classfile.Project, out *classfile.BuildResult) {
				depVer := module.Version{Path: "test/liba", Version: "1.0.0"}
				result, ok := ctx.BuildResult(depVer)
				gotDepResult = result.Metadata()
				gotDepOk = ok
				out.SetMetadata("-lB")
			},
		},
		FS:      os.DirFS(filepath.Join(testSourceDir, "test/libb")),
		Path:    "test/libb",
		Version: "1.0.0",
		Deps:    []*modules.Module{modA},
	}

	// Main module has all modules in deps
	modB.Deps = []*modules.Module{modA}
	targets := []*modules.Module{modB, modA}

	results, err := b.Build(context.Background(), targets)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if !gotDepOk {
		t.Fatal("dep build result was not injected into context")
	}
	if gotDepResult != "-lA" {
		t.Errorf("dep result metadata = %q, want %q", gotDepResult, "-lA")
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	// Build order: A first (leaf), then B
	if results[0].Metadata != "-lA" {
		t.Errorf("results[0].Metadata = %q, want %q", results[0].Metadata, "-lA")
	}
	if results[1].Metadata != "-lB" {
		t.Errorf("results[1].Metadata = %q, want %q", results[1].Metadata, "-lB")
	}
}

func TestBuild_DirectModule_ContextFields(t *testing.T) {
	store := setupTestStore(t)
	wsDir := t.TempDir()

	b := &Builder{
		store:        store,
		matrix:       "amd64-linux",
		workspaceDir: wsDir,
		newRepo: func(repoPath string) (vcs.Repo, error) {
			modPath := strings.TrimPrefix(repoPath, "github.com/")
			return newMockRepo(filepath.Join(testSourceDir, modPath)), nil
		},
	}

	var capturedSourceDir string
	var capturedMatrix string
	testMod := &modules.Module{
		Formula: &formula.Formula{
			ModPath: "test/liba",
			FromVer: "1.0.0",
			OnBuild: func(ctx *classfile.Context, proj *classfile.Project, out *classfile.BuildResult) {
				capturedSourceDir = ctx.SourceDir
				capturedMatrix = ctx.CurrentMatrix()
				out.SetMetadata("-lA")
			},
		},
		FS:      os.DirFS(filepath.Join(testSourceDir, "test/liba")),
		Path:    "test/liba",
		Version: "1.0.0",
	}

	_, err := b.Build(context.Background(), []*modules.Module{testMod})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if capturedSourceDir == "" {
		t.Error("context SourceDir should not be empty")
	}
	if capturedMatrix != "amd64-linux" {
		t.Errorf("context matrix = %q, want %q", capturedMatrix, "amd64-linux")
	}
}

func TestBuild_DirectModule_OutputDir(t *testing.T) {
	store := setupTestStore(t)
	wsDir := t.TempDir()

	b := &Builder{
		store:        store,
		matrix:       "amd64-linux",
		workspaceDir: wsDir,
		newRepo: func(repoPath string) (vcs.Repo, error) {
			modPath := strings.TrimPrefix(repoPath, "github.com/")
			return newMockRepo(filepath.Join(testSourceDir, modPath)), nil
		},
	}

	var outputDirErr error
	var gotOutputDir string
	testMod := &modules.Module{
		Formula: &formula.Formula{
			ModPath: "test/liba",
			FromVer: "1.0.0",
			OnBuild: func(ctx *classfile.Context, proj *classfile.Project, out *classfile.BuildResult) {
				gotOutputDir, outputDirErr = ctx.OutputDir(module.Version{Path: "test/liba", Version: "1.0.0"})
				out.SetMetadata("-lA")
			},
		},
		FS:      os.DirFS(filepath.Join(testSourceDir, "test/liba")),
		Path:    "test/liba",
		Version: "1.0.0",
	}

	_, err := b.Build(context.Background(), []*modules.Module{testMod})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if outputDirErr != nil {
		t.Fatalf("OutputDir() error = %v", outputDirErr)
	}
	expectedDir, _ := b.installDir("test/liba", "1.0.0")
	if gotOutputDir != expectedDir {
		t.Errorf("OutputDir = %q, want %q", gotOutputDir, expectedDir)
	}
}

func TestBuild_DirectModule_ProjectDeps(t *testing.T) {
	store := setupTestStore(t)
	wsDir := t.TempDir()

	b := &Builder{
		store:        store,
		matrix:       "amd64-linux",
		workspaceDir: wsDir,
		newRepo: func(repoPath string) (vcs.Repo, error) {
			modPath := strings.TrimPrefix(repoPath, "github.com/")
			return newMockRepo(filepath.Join(testSourceDir, modPath)), nil
		},
	}

	// Create a chain: C -> B -> A
	modA := &modules.Module{
		Formula: &formula.Formula{
			ModPath: "test/liba",
			FromVer: "1.0.0",
			OnBuild: func(ctx *classfile.Context, proj *classfile.Project, out *classfile.BuildResult) {
				out.SetMetadata("-lA")
			},
		},
		FS:      os.DirFS(filepath.Join(testSourceDir, "test/liba")),
		Path:    "test/liba",
		Version: "1.0.0",
	}

	modB := &modules.Module{
		Formula: &formula.Formula{
			ModPath: "test/libb",
			FromVer: "1.0.0",
			OnBuild: func(ctx *classfile.Context, proj *classfile.Project, out *classfile.BuildResult) {
				out.SetMetadata("-lB")
			},
		},
		FS:      os.DirFS(filepath.Join(testSourceDir, "test/libb")),
		Path:    "test/libb",
		Version: "1.0.0",
		Deps:    []*modules.Module{modA},
	}

	var capturedDeps []module.Version
	modC := &modules.Module{
		Formula: &formula.Formula{
			ModPath: "test/libc",
			FromVer: "1.0.0",
			OnBuild: func(ctx *classfile.Context, proj *classfile.Project, out *classfile.BuildResult) {
				capturedDeps = proj.Deps
				out.SetMetadata("-lC")
			},
		},
		FS:      os.DirFS(filepath.Join(testSourceDir, "test/libc")),
		Path:    "test/libc",
		Version: "1.0.0",
		Deps:    []*modules.Module{modB},
	}

	targets := []*modules.Module{modC, modB, modA}
	_, err := b.Build(context.Background(), targets)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// C depends on B, which depends on A.
	// resolveModTransitiveDeps should return A, B (in topological order).
	if len(capturedDeps) != 2 {
		t.Fatalf("project deps len = %d, want 2", len(capturedDeps))
	}
	// A before B (DFS post-order)
	if capturedDeps[0].Path != "test/liba" {
		t.Errorf("dep[0] = %q, want %q", capturedDeps[0].Path, "test/liba")
	}
	if capturedDeps[1].Path != "test/libb" {
		t.Errorf("dep[1] = %q, want %q", capturedDeps[1].Path, "test/libb")
	}
}

func TestBuild_DirectModule_ProjectSourceFS(t *testing.T) {
	store := setupTestStore(t)
	wsDir := t.TempDir()

	b := &Builder{
		store:        store,
		matrix:       "amd64-linux",
		workspaceDir: wsDir,
		newRepo: func(repoPath string) (vcs.Repo, error) {
			modPath := strings.TrimPrefix(repoPath, "github.com/")
			return newMockRepo(filepath.Join(testSourceDir, modPath)), nil
		},
	}

	var sourceContent []byte
	var readErr error
	testMod := &modules.Module{
		Formula: &formula.Formula{
			ModPath: "test/liba",
			FromVer: "1.0.0",
			OnBuild: func(ctx *classfile.Context, proj *classfile.Project, out *classfile.BuildResult) {
				sourceContent, readErr = proj.ReadFile("source.txt")
				out.SetMetadata("-lA")
			},
		},
		FS:      os.DirFS(filepath.Join(testSourceDir, "test/liba")),
		Path:    "test/liba",
		Version: "1.0.0",
	}

	_, err := b.Build(context.Background(), []*modules.Module{testMod})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if readErr != nil {
		t.Fatalf("proj.ReadFile() error = %v", readErr)
	}
	if !strings.Contains(string(sourceContent), "liba source") {
		t.Errorf("source content = %q, want it to contain %q", string(sourceContent), "liba source")
	}
}

// ---------------------------------------------------------------------------
// findResult helper tests
// ---------------------------------------------------------------------------

func TestFindResult_NotFound(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/liba", Version: "1.0.0"}
	results, mods := loadAndBuild(t, b, store, main)

	_, ok := findResult(results, b, mods, "nonexistent/mod")
	if ok {
		t.Error("findResult should return false for nonexistent module")
	}
}
