package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goplus/llar/internal/modules"
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

// TestE2E_ContextMatrix verifies that ctx.currentMatrix() returns the
// builder's matrix string inside the formula's onBuild callback.
func TestE2E_ContextMatrix(t *testing.T) {
	store := setupTestStore(t)

	tests := []struct {
		matrix string
	}{
		{"amd64-linux"},
		{"arm64-darwin"},
		{"x86_64-linux|zlibON"},
	}
	for _, tt := range tests {
		t.Run(tt.matrix, func(t *testing.T) {
			b := setupBuilder(t, store, tt.matrix)
			main := module.Version{Path: "test/ctxcheck", Version: "1.0.0"}
			results, _ := loadAndBuild(t, b, store, main)

			if results[0].Metadata != tt.matrix {
				t.Errorf("metadata = %q, want %q", results[0].Metadata, tt.matrix)
			}
		})
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

// TestE2E_DiamondBuildOrder verifies that in a diamond dependency graph,
// shared leaves are built before modules that depend on them.
func TestE2E_DiamondBuildOrder(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/diamond", Version: "1.0.0"}
	ctx := context.Background()
	mods, err := modules.Load(ctx, main, modules.Options{FormulaStore: store})
	if err != nil {
		t.Fatalf("modules.Load() failed: %v", err)
	}

	buildOrder := b.constructBuildList(mods)
	var orderPaths []string
	for _, m := range buildOrder {
		orderPaths = append(orderPaths, m.Path)
	}
	t.Logf("diamond build order: %v", orderPaths)

	// liba must be built before both libb and diamond
	indexOf := func(path string) int {
		for i, p := range orderPaths {
			if p == path {
				return i
			}
		}
		return -1
	}
	libaIdx := indexOf("test/liba")
	libbIdx := indexOf("test/libb")
	diamondIdx := indexOf("test/diamond")

	if libaIdx < 0 || libbIdx < 0 || diamondIdx < 0 {
		t.Fatalf("missing modules in build order: liba=%d libb=%d diamond=%d",
			libaIdx, libbIdx, diamondIdx)
	}
	if libaIdx >= libbIdx {
		t.Errorf("liba@%d should be built before libb@%d", libaIdx, libbIdx)
	}
	if libaIdx >= diamondIdx {
		t.Errorf("liba@%d should be built before diamond@%d", libaIdx, diamondIdx)
	}
	if libbIdx >= diamondIdx {
		t.Errorf("libb@%d should be built before diamond@%d", libbIdx, diamondIdx)
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

// TestE2E_TransitiveChainWithDeps verifies the full transitive chain:
// libc -> libb -> liba, ensuring all modules are loaded, ordered, and
// built correctly through the real formula pipeline.
func TestE2E_TransitiveChainWithDeps(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")

	main := module.Version{Path: "test/libc", Version: "1.0.0"}
	results, mods := loadAndBuild(t, b, store, main)

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	// Verify all install directories were created
	for _, mod := range mods {
		installDir, _ := b.installDir(mod.Path, mod.Version)
		if _, err := os.Stat(installDir); err != nil {
			t.Errorf("installDir not created for %s: %v", mod.Path, err)
		}
	}

	// Verify all caches were written
	for _, mod := range mods {
		cache, err := b.loadCache(mod.Path)
		if err != nil {
			t.Errorf("cache not written for %s: %v", mod.Path, err)
			continue
		}
		if _, ok := cache.get(mod.Version, "amd64-linux"); !ok {
			t.Errorf("cache entry missing for %s@%s", mod.Path, mod.Version)
		}
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

// TestE2E_SharedWorkspaceDifferentModules verifies that building different
// modules in the same workspace doesn't interfere with each other.
func TestE2E_SharedWorkspaceDifferentModules(t *testing.T) {
	store := setupTestStore(t)
	wsDir := t.TempDir()

	mods := []struct {
		path     string
		wantMeta string
	}{
		{"test/liba", "-lA"},
		{"test/libb", "-lB"},
		{"test/readcfg", "-lreadcfg"},
	}

	for _, tc := range mods {
		b := setupBuilder(t, store, "amd64-linux")
		b.workspaceDir = wsDir

		main := module.Version{Path: tc.path, Version: "1.0.0"}
		results, allMods := loadAndBuild(t, b, store, main)

		r, ok := findResult(results, b, allMods, tc.path)
		if !ok {
			t.Errorf("missing result for %s", tc.path)
			continue
		}
		got := strings.TrimSpace(r.Metadata)
		if got != tc.wantMeta {
			t.Errorf("%s: metadata = %q, want %q", tc.path, got, tc.wantMeta)
		}
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
