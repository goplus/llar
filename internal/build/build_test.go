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
	"github.com/goplus/llar/pkgs/mod/module"
)

func init() {
	formulaDir, err := env.FormulaDir()
	if err != nil {
		panic(err)
	}
	os.RemoveAll(formulaDir)

	os.CopyFS(formulaDir, os.DirFS("testdata"))
}

func TestBuildZlib(t *testing.T) {
	testdataDir, _ := filepath.Abs("testdata")

	// Mock formula repo (formulas are in testdata)
	mockFormulaRepo := newMockRepo(testdataDir)

	t.Run("zlib", func(t *testing.T) {
		ctx := context.TODO()
		mainModule := module.Version{ID: "madler/zlib", Version: "v1.2.11"}

		// Load packages with mock formula repo
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

		// Use real vcs.NewRepo for source code
		_, err = NewBuilder(matrix).Build(ctx, mainModule, modules)
		if err != nil {
			t.Fatal(err)
			return
		}
		dir, err := env.FormulaDir()
		if err != nil {
			t.Fatal(err)
			return
		}
		outDir := filepath.Join(dir, "madler/zlib", ".build", "v1.2.11", "amd64-linux")

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
		mainModule := module.Version{ID: "pnggroup/libpng", Version: "v1.6.53"}

		// Load packages with mock formula repo
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

		// Use real vcs.NewRepo for source code
		_, err = NewBuilder(matrix).Build(ctx, mainModule, modules)
		if err != nil {
			t.Fatal(err)
			return
		}
		dir, err := env.FormulaDir()
		if err != nil {
			t.Fatal(err)
			return
		}
		outDir := filepath.Join(dir, "pnggroup/libpng", ".build", "v1.6.53", "amd64-linux")

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
