package build

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goplus/llar/internal/modules"
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
