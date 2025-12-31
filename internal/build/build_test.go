package build

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/env"
	"github.com/goplus/llar/internal/modules"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/pkgs/mod/module"
)

func init() {
	formulaDir, err := env.FormulaDir()
	if err != nil {
		panic(err)
	}
	os.RemoveAll(formulaDir)

	if err = os.CopyFS(formulaDir, os.DirFS("testdata")); err != nil {
		panic(err)
	}
}

func TestBuildZlib(t *testing.T) {
	testdataDir, _ := filepath.Abs("testdata")
	mockRepo := newMockRepo(testdataDir)

	// Create mock repo factory for modules.Load
	mockFormulaRepo := newMockRepo(testdataDir)

	t.Run("zlib", func(t *testing.T) {
		ctx := context.TODO()
		mainModule := module.Version{ID: "github.com/madler/zlib", Version: "1.2.11"}

		// Load packages using modload with mock formula repo
		modules, err := modules.Load(ctx, mainModule, modules.Options{
			FormulaRepo: mockFormulaRepo,
		})
		if err != nil {
			t.Fatal(err)
			return
		}

		matrix := formula.Matrix{
			Require: map[string][]string{
				"os":   {"linux"},
				"arch": {"amd64"},
			},
		}

		// Use mock repo factory for source downloads
		newRepo := func(repoPath string) (vcs.Repo, error) {
			return mockRepo, nil
		}

		err = NewBuilder(newRepo).Build(ctx, mainModule, modules, matrix)
		if err != nil {
			t.Fatal(err)
			return
		}
		dir, err := env.FormulaDir()
		if err != nil {
			t.Fatal(err)
			return
		}
		outDir := filepath.Join(dir, "github.com/madler/zlib", ".build", "1.2.11", "amd64-linux")

		if _, err := os.Stat(filepath.Join(outDir, "lib")); os.IsNotExist(err) {
			t.Errorf("output dir not found")
			return
		}

		ret, err := exec.Command("nm", "-g", filepath.Join(outDir, "lib", "libz.a")).CombinedOutput()
		if err != nil {
			t.Fatal(string(ret))
			return
		}
		if !strings.Contains(string(ret), "compress") {
			t.Fatalf("unexpeceted: want symbol compress")
		}
	})
	t.Run("libpng", func(t *testing.T) {
		ctx := context.TODO()
		mainModule := module.Version{ID: "github.com/pnggroup/libpng", Version: "v1.6.53"}

		// Load packages using modload with mock formula repo
		modules, err := modules.Load(ctx, mainModule, modules.Options{
			FormulaRepo: mockFormulaRepo,
		})
		if err != nil {
			t.Fatal(err)
			return
		}

		matrix := formula.Matrix{
			Require: map[string][]string{
				"os":   {"linux"},
				"arch": {"amd64"},
			},
		}

		// Use mock repo factory for source downloads
		newRepo := func(repoPath string) (vcs.Repo, error) {
			return mockRepo, nil
		}

		err = NewBuilder(newRepo).Build(ctx, mainModule, modules, matrix)
		if err != nil {
			t.Fatal(err)
			return
		}
		dir, err := env.FormulaDir()
		if err != nil {
			t.Fatal(err)
			return
		}
		outDir := filepath.Join(dir, "github.com/pnggroup/libpng", ".build", "v1.6.53", "amd64-linux")

		if _, err := os.Stat(filepath.Join(outDir, "lib")); os.IsNotExist(err) {
			t.Errorf("output dir not found")
			return
		}

		ret, err := exec.Command("nm", "-g", filepath.Join(outDir, "lib", "libpng.a")).CombinedOutput()
		if err != nil {
			t.Fatal(string(ret))
			return
		}
		if !strings.Contains(string(ret), "png_free") {
			t.Fatalf("unexpeceted: want symbol png_free")
		}
	})

}
