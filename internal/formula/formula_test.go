package formula

import (
	"io/fs"
	"os"
	"testing"

	formulapkg "github.com/goplus/llar/formula"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
)

func TestFromVerOf(t *testing.T) {
	t.Run("ValidFormula", func(t *testing.T) {
		fromVer, err := FromVerOf("testdata/formula/hello_llar.gox")
		if err != nil {
			t.Fatalf("FromVerOf failed: %v", err)
		}
		if fromVer != "v1.0.0" {
			t.Errorf("Expected fromVer 'v1.0.0', got '%s'", fromVer)
		}
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		_, err := FromVerOf("testdata/nonexistent.gox")
		if err == nil {
			t.Error("FromVerOf should return error for non-existent file")
		}
	})
}

func TestLoadFS(t *testing.T) {
	t.Run("ValidFormula", func(t *testing.T) {
		fsys := os.DirFS("testdata/formula").(fs.ReadFileFS)
		f, err := LoadFS(fsys, "hello_llar.gox")
		if err != nil {
			t.Fatalf("LoadFS failed: %v", err)
		}
		// Verify metadata
		if f.ModPath != "DaveGamble/cJSON" {
			t.Errorf("Unexpected ModPath: want %s got %s", "DaveGamble/cJSON", f.ModPath)
		}
		if f.FromVer != "v1.0.0" {
			t.Errorf("Unexpected FromVer: want %s got %s", "v1.0.0", f.FromVer)
		}
		if f.OnBuild == nil {
			t.Error("OnBuild is nil")
		}
		if f.OnRequire == nil {
			t.Error("OnRequire is nil")
		}

		// Functional test: verify callbacks can be invoked without panic
		f.OnRequire(&formulapkg.Project{}, &formulapkg.ModuleDeps{})
		f.OnBuild(&formulapkg.Context{}, &formulapkg.Project{}, &formulapkg.BuildResult{})
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		fsys := os.DirFS("testdata/formula").(fs.ReadFileFS)
		_, err := LoadFS(fsys, "nonexistent.gox")
		if err == nil {
			t.Error("LoadFS should return error for non-existent file")
		}
	})

	t.Run("InvalidSyntax", func(t *testing.T) {
		tmpDir := t.TempDir()
		os.WriteFile(tmpDir+"/invalid_llar.gox", []byte("this is not valid gox code !!!@@@"), 0644)
		fsys := os.DirFS(tmpDir).(fs.ReadFileFS)
		_, err := LoadFS(fsys, "invalid_llar.gox")
		if err == nil {
			t.Error("LoadFS should return error for invalid syntax")
		}
	})
}

func TestLoad(t *testing.T) {
	t.Run("PathOutsideFormulaDir", func(t *testing.T) {
		_, err := Load("/tmp/some_formula.gox")
		if err == nil {
			t.Error("Load should return error for path outside formula directory")
		}
	})

	t.Run("PathTraversal", func(t *testing.T) {
		formulaDir, err := Dir()
		if err != nil {
			t.Skipf("Could not get formula dir: %v", err)
		}

		testCases := []string{
			formulaDir + "/..",                   // exact ".."
			formulaDir + "/../foo",               // ../foo
			formulaDir + "/../../../etc/passwd",  // deep traversal
			formulaDir + "/foo/../../../etc",     // embedded traversal
			"/tmp/evil.gox",                      // completely outside
		}

		for _, path := range testCases {
			_, err := Load(path)
			if err == nil {
				t.Errorf("Load(%q) should return error for path traversal", path)
			}
		}
	})

	t.Run("ValidFormulaInFormulaDir", func(t *testing.T) {
		formulaDir, err := Dir()
		if err != nil {
			t.Skipf("Could not get formula dir: %v", err)
		}
		testFile := formulaDir + "/test_llar.gox"
		validGox := `id "test/pkg"
fromVer "v1.0.0"
onRequire (proj, deps) => { echo "hello" }
onBuild (ctx, proj, out) => { echo "hello" }
`
		if err := os.WriteFile(testFile, []byte(validGox), 0644); err != nil {
			t.Skipf("Could not write test file: %v", err)
		}
		defer os.Remove(testFile)

		formula, err := Load(testFile)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if formula.ModPath != "test/pkg" {
			t.Errorf("Unexpected ModPath: want %s got %s", "test/pkg", formula.ModPath)
		}
	})
}

func TestDir(t *testing.T) {
	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir() failed: %v", err)
	}
	if dir == "" {
		t.Error("Dir() returned empty string")
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Errorf("Dir() returned non-existent path: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("Dir() returned non-directory path: %s", dir)
	}
}

func TestFormula_SetStdout(t *testing.T) {
	formula, err := loadFS(os.DirFS("testdata/formula").(fs.ReadFileFS), "hello_llar.gox")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	var buf []byte
	formula.SetStdout(&mockWriter{buf: &buf})
	formula.SetStdout(nil)
}

func TestFormula_SetStderr(t *testing.T) {
	formula, err := loadFS(os.DirFS("testdata/formula").(fs.ReadFileFS), "hello_llar.gox")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	var buf []byte
	formula.SetStderr(&mockWriter{buf: &buf})
	formula.SetStderr(nil)
}

func TestFromVerFrom(t *testing.T) {
	t.Run("WithFromVer", func(t *testing.T) {
		testCode := `package main
func Main() { fromVer("v1.0.0") }
`
		fs := token.NewFileSet()
		astFile, _ := parser.ParseFile(fs, "test.gox", testCode, 0)
		fromVer, err := fromVerFrom(astFile)
		if err != nil {
			t.Fatalf("fromVerFrom failed: %v", err)
		}
		if fromVer != "v1.0.0" {
			t.Errorf("Expected 'v1.0.0', got '%s'", fromVer)
		}
	})

	t.Run("WithoutFromVer", func(t *testing.T) {
		testCode := `package main
func Main() { println("hello") }
`
		fs := token.NewFileSet()
		astFile, _ := parser.ParseFile(fs, "test.gox", testCode, 0)
		fromVer, _ := fromVerFrom(astFile)
		if fromVer != "" {
			t.Errorf("Expected empty fromVer, got '%s'", fromVer)
		}
	})
}

func TestParseCallArg(t *testing.T) {
	findCallExpr := func(code string, fnName string) *ast.CallExpr {
		fs := token.NewFileSet()
		astFile, _ := parser.ParseFile(fs, "test.gox", code, 0)
		var callExpr *ast.CallExpr
		ast.Inspect(astFile, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if ident, ok := c.Fun.(*ast.Ident); ok && ident.Name == fnName {
					callExpr = c
					return false
				}
			}
			return true
		})
		return callExpr
	}

	t.Run("ValidArgument", func(t *testing.T) {
		callExpr := findCallExpr(`package main; func Main() { fromVer("v1.2.3") }`, "fromVer")
		result, err := parseCallArg(callExpr, "fromVer")
		if err != nil {
			t.Fatalf("parseCallArg failed: %v", err)
		}
		if result != "v1.2.3" {
			t.Errorf("Expected 'v1.2.3', got '%s'", result)
		}
	})

	t.Run("NoArguments", func(t *testing.T) {
		callExpr := findCallExpr(`package main; func Main() { fromVer() }`, "fromVer")
		_, err := parseCallArg(callExpr, "fromVer")
		if err == nil {
			t.Error("parseCallArg should return error for no arguments")
		}
	})

	t.Run("EmptyString", func(t *testing.T) {
		callExpr := findCallExpr(`package main; func Main() { fromVer("") }`, "fromVer")
		_, err := parseCallArg(callExpr, "fromVer")
		if err == nil {
			t.Error("parseCallArg should return error for empty string")
		}
	})

	t.Run("BacktickEmptyString", func(t *testing.T) {
		callExpr := findCallExpr("package main; func Main() { fromVer(``) }", "fromVer")
		_, err := parseCallArg(callExpr, "fromVer")
		if err == nil {
			t.Error("parseCallArg should return error for empty backtick string")
		}
	})

	t.Run("NonBasicLitArg", func(t *testing.T) {
		callExpr := findCallExpr(`package main; var v = "x"; func Main() { fromVer(v) }`, "fromVer")
		result, _ := parseCallArg(callExpr, "fromVer")
		if result != "" {
			t.Errorf("Expected empty string for non-BasicLit arg, got '%s'", result)
		}
	})
}

type mockWriter struct {
	buf *[]byte
}

func (m *mockWriter) Write(p []byte) (n int, err error) {
	*m.buf = append(*m.buf, p...)
	return len(p), nil
}
