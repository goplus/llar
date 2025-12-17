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
	t.Run("zlib", func(t *testing.T) {
		err := NewBuilder().Build(context.TODO(), "madler/zlib", "1.2.11", formula.Matrix{
			Require: map[string][]string{
				"os":   []string{"linux"},
				"arch": []string{"amd64"},
			},
		})
		if err != nil {
			t.Fatal(err)
			return
		}
		dir, err := env.FormulaDir()
		if err != nil {
			t.Fatal(err)
			return
		}
		outDir := filepath.Join(dir, "madler/zlib", "build", "1.2.11", "amd64-linux")

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
		err := NewBuilder().Build(context.TODO(), "pnggroup/libpng", "v1.6.53", formula.Matrix{
			Require: map[string][]string{
				"os":   []string{"linux"},
				"arch": []string{"amd64"},
			},
		})
		if err != nil {
			t.Fatal(err)
			return
		}
		dir, err := env.FormulaDir()
		if err != nil {
			t.Fatal(err)
			return
		}
		outDir := filepath.Join(dir, "pnggroup/libpng", "build", "v1.6.53", "amd64-linux")

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

}
