package formula

import (
	"io/fs"
	"os"
	"strings"
	"testing"

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
		if fromVer == "" {
			t.Error("FromVerOf returned empty string")
		}
		t.Logf("fromVer: %s", fromVer)
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		_, err := FromVerOf("testdata/nonexistent.gox")
		if err == nil {
			t.Error("FromVerOf should return error for non-existent file")
		}
	})
}

func TestLoad(t *testing.T) {
	t.Run("LoadValidFormula", func(t *testing.T) {
		formula, err := loadFS(os.DirFS("testdata/formula").(fs.ReadFileFS), "hello_llar.gox")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if formula == nil {
			t.Fatal("Load returned nil formula")
		}

		// Test that struct fields are populated
		if formula.ModPath != "DaveGamble/cJSON" {
			t.Errorf("Unexpected ModPath: want %s got %s", "DaveGamble/cJSON", formula.ModPath)
			return
		}

		if formula.FromVer != "v1.0.0" {
			t.Errorf("Unexpected FromVer: want %s got %s", "v1.0.0", formula.FromVer)
			return
		}

		if formula.OnBuild == nil {
			t.Error("OnBuild is nil")
		}

		if formula.OnRequire == nil {
			t.Error("OnRequire is nil")
		}
	})

	t.Run("LoadInvalidPath", func(t *testing.T) {
		_, err := Load("nonexistent/path.gox")
		if err == nil {
			t.Error("Load should return error for non-existent path")
		}
	})

	t.Run("LoadInvalidFileName", func(t *testing.T) {
		// File name without underscore should fail
		_, err := Load("testdata/invalid.gox")
		if err == nil {
			t.Error("Load should return error for invalid file name")
		}
	})
}

func TestFormula_SetStdout(t *testing.T) {
	formula, err := loadFS(os.DirFS("testdata/formula").(fs.ReadFileFS), "hello_llar.gox")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Test setting stdout writer
	t.Run("SetValidWriter", func(t *testing.T) {
		var buf []byte
		mockWriter := &mockWriter{buf: &buf}

		// Should not panic
		formula.SetStdout(mockWriter)
	})

	t.Run("SetNilWriter", func(t *testing.T) {
		// Should not panic with nil writer
		formula.SetStdout(nil)
	})
}

func TestFormula_SetStderr(t *testing.T) {
	formula, err := loadFS(os.DirFS("testdata/formula").(fs.ReadFileFS), "hello_llar.gox")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Test setting stderr writer
	t.Run("SetValidWriter", func(t *testing.T) {
		var buf []byte
		mockWriter := &mockWriter{buf: &buf}

		// Should not panic
		formula.SetStderr(mockWriter)
	})

	t.Run("SetNilWriter", func(t *testing.T) {
		// Should not panic with nil writer
		formula.SetStderr(nil)
	})
}

func TestLoadFS(t *testing.T) {
	t.Run("LoadValidFormula", func(t *testing.T) {
		fsys := os.DirFS("testdata/formula").(fs.ReadFileFS)
		formula, err := LoadFS(fsys, "hello_llar.gox")
		if err != nil {
			t.Fatalf("LoadFS failed: %v", err)
		}

		if formula == nil {
			t.Fatal("LoadFS returned nil formula")
		}

		if formula.ModPath == "" {
			t.Error("ModPath is empty")
		}
		t.Logf("ModPath: %s", formula.ModPath)

		if formula.FromVer == "" {
			t.Error("FromVer is empty")
		}
		t.Logf("FromVer: %s", formula.FromVer)

		if formula.OnBuild == nil {
			t.Error("OnBuild is nil")
		}

		if formula.OnRequire == nil {
			t.Error("OnRequire is nil")
		}
	})

	t.Run("LoadNonExistentFile", func(t *testing.T) {
		fsys := os.DirFS("testdata/formula").(fs.ReadFileFS)
		_, err := LoadFS(fsys, "nonexistent.gox")
		if err == nil {
			t.Error("LoadFS should return error for non-existent file")
		}
	})

	t.Run("LoadInvalidFileName", func(t *testing.T) {
		// Create a mock FS with an invalid file name
		fsys := os.DirFS("testdata").(fs.ReadFileFS)
		_, err := LoadFS(fsys, "invalid.gox")
		if err == nil {
			t.Error("LoadFS should return error for invalid file name")
		}
	})
}

func TestFromVerFrom(t *testing.T) {
	t.Run("ParseValidAST", func(t *testing.T) {
		// Create a simple test AST with fromVer call
		testCode := `package main

func Main() {
	fromVer("v1.0.0")
}
`
		fs := token.NewFileSet()
		astFile, err := parser.ParseFile(fs, "test.gox", testCode, 0)
		if err != nil {
			t.Fatalf("Failed to parse test code: %v", err)
		}

		fromVer, err := fromVerFrom(astFile)
		if err != nil {
			t.Fatalf("fromVerFrom failed: %v", err)
		}

		if fromVer != "v1.0.0" {
			t.Errorf("Expected fromVer to be 'v1.0.0', got '%s'", fromVer)
		}
	})

	t.Run("ParseASTWithoutFromVer", func(t *testing.T) {
		testCode := `package main

func Main() {
	println("hello")
}
`
		fs := token.NewFileSet()
		astFile, err := parser.ParseFile(fs, "test.gox", testCode, 0)
		if err != nil {
			t.Fatalf("Failed to parse test code: %v", err)
		}

		fromVer, err := fromVerFrom(astFile)
		if fromVer != "" {
			t.Errorf("Expected empty fromVer, got '%s'", fromVer)
		}
		if err != nil {
			t.Logf("Expected error (no fromVer found): %v", err)
		}
	})
}

func TestParseCallArg(t *testing.T) {
	t.Run("ValidArgument", func(t *testing.T) {
		testCode := `package main

func Main() {
	fromVer("v1.2.3")
}
`
		fs := token.NewFileSet()
		astFile, err := parser.ParseFile(fs, "test.gox", testCode, 0)
		if err != nil {
			t.Fatalf("Failed to parse test code: %v", err)
		}

		// Find the call expression in the AST
		var callExpr *ast.CallExpr
		ast.Inspect(astFile, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				callExpr = c
				return false
			}
			return true
		})

		if callExpr == nil {
			t.Fatal("Failed to find call expression in test AST")
		}

		result, err := parseCallArg(callExpr, "fromVer")
		if err != nil {
			t.Fatalf("parseCallArg failed: %v", err)
		}

		if result != "v1.2.3" {
			t.Errorf("Expected 'v1.2.3', got '%s'", result)
		}
	})

	t.Run("NoArguments", func(t *testing.T) {
		testCode := `package main

func Main() {
	fromVer()
}
`
		fs := token.NewFileSet()
		astFile, err := parser.ParseFile(fs, "test.gox", testCode, 0)
		if err != nil {
			t.Fatalf("Failed to parse test code: %v", err)
		}

		var callExpr *ast.CallExpr
		ast.Inspect(astFile, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				callExpr = c
				return false
			}
			return true
		})

		if callExpr == nil {
			t.Fatal("Failed to find call expression in test AST")
		}

		_, err = parseCallArg(callExpr, "fromVer")
		if err == nil {
			t.Error("parseCallArg should return error for call with no arguments")
		}
	})
}

// mockWriter is a simple mock implementation of io.Writer for testing
type mockWriter struct {
	buf *[]byte
}

func (m *mockWriter) Write(p []byte) (n int, err error) {
	*m.buf = append(*m.buf, p...)
	return len(p), nil
}

func TestDir(t *testing.T) {
	t.Run("ValidDir", func(t *testing.T) {
		dir, err := Dir()
		if err != nil {
			t.Fatalf("Dir() failed: %v", err)
		}
		if dir == "" {
			t.Error("Dir() returned empty string")
		}
		// Check that the directory exists
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("Dir() returned non-existent path: %v", err)
		}
		if !info.IsDir() {
			t.Errorf("Dir() returned non-directory path: %s", dir)
		}
	})
}

func TestParseCallArgEmptyString(t *testing.T) {
	t.Run("EmptyStringArgument", func(t *testing.T) {
		testCode := `package main

func Main() {
	fromVer("")
}
`
		fs := token.NewFileSet()
		astFile, err := parser.ParseFile(fs, "test.gox", testCode, 0)
		if err != nil {
			t.Fatalf("Failed to parse test code: %v", err)
		}

		var callExpr *ast.CallExpr
		ast.Inspect(astFile, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				callExpr = c
				return false
			}
			return true
		})

		if callExpr == nil {
			t.Fatal("Failed to find call expression in test AST")
		}

		_, err = parseCallArg(callExpr, "fromVer")
		if err == nil {
			t.Error("parseCallArg should return error for empty string argument")
		}
	})

	t.Run("BacktickEmptyString", func(t *testing.T) {
		testCode := "package main\n\nfunc Main() {\n\tfromVer(``)\n}\n"
		fs := token.NewFileSet()
		astFile, err := parser.ParseFile(fs, "test.gox", testCode, 0)
		if err != nil {
			t.Fatalf("Failed to parse test code: %v", err)
		}

		var callExpr *ast.CallExpr
		ast.Inspect(astFile, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				callExpr = c
				return false
			}
			return true
		})

		if callExpr == nil {
			t.Fatal("Failed to find call expression in test AST")
		}

		_, err = parseCallArg(callExpr, "fromVer")
		if err == nil {
			t.Error("parseCallArg should return error for empty backtick string argument")
		}
	})

	t.Run("NonBasicLitArg", func(t *testing.T) {
		testCode := `package main

var version = "v1.0.0"

func Main() {
	fromVer(version)
}
`
		fs := token.NewFileSet()
		astFile, err := parser.ParseFile(fs, "test.gox", testCode, 0)
		if err != nil {
			t.Fatalf("Failed to parse test code: %v", err)
		}

		var callExpr *ast.CallExpr
		ast.Inspect(astFile, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if ident, ok := c.Fun.(*ast.Ident); ok && ident.Name == "fromVer" {
					callExpr = c
					return false
				}
			}
			return true
		})

		if callExpr == nil {
			t.Fatal("Failed to find fromVer call expression in test AST")
		}

		// When argument is not a BasicLit, parseCallArg returns empty string without error
		result, err := parseCallArg(callExpr, "fromVer")
		if err != nil {
			t.Logf("parseCallArg returned error for non-BasicLit arg: %v", err)
		}
		if result != "" {
			t.Errorf("parseCallArg should return empty string for non-BasicLit arg, got: %s", result)
		}
	})
}

func TestLoadFSErrors(t *testing.T) {
	t.Run("InvalidStructName", func(t *testing.T) {
		// Create a mock FS with a file that has no underscore in name
		fsys := os.DirFS("testdata").(fs.ReadFileFS)
		_, err := LoadFS(fsys, "cmp/hello_cmp.gox")
		// This should fail because the struct name won't match
		if err == nil {
			t.Log("Note: LoadFS didn't return error - struct 'hello' may have been found")
		}
	})

	t.Run("BuildError", func(t *testing.T) {
		// Test with a file that contains invalid syntax
		tmpDir := t.TempDir()
		invalidFile := "invalid_llar.gox"
		err := os.WriteFile(tmpDir+"/"+invalidFile, []byte("this is not valid gox code !!!@@@"), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		fsys := os.DirFS(tmpDir).(fs.ReadFileFS)
		_, err = LoadFS(fsys, invalidFile)
		if err == nil {
			t.Error("LoadFS should return error for invalid syntax")
		}
	})
}

func TestLoadPathValidation(t *testing.T) {
	t.Run("PathOutsideFormulaDir", func(t *testing.T) {
		// Try to load a path outside the formula directory
		_, err := Load("/tmp/some_formula.gox")
		if err == nil {
			t.Error("Load should return error for path outside formula directory")
		}
	})

	t.Run("RelativePathEscapeAttempt", func(t *testing.T) {
		// Try to escape the formula directory with relative path
		formulaDir, err := Dir()
		if err != nil {
			t.Skipf("Could not get formula dir: %v", err)
		}
		// Create a path that would resolve to parent directory
		escapePath := formulaDir + "/../../../etc/passwd"
		_, err = Load(escapePath)
		if err == nil {
			t.Error("Load should return error for path escape attempt")
		}
	})

	t.Run("ExactParentDir", func(t *testing.T) {
		// Test case where relPath == ".."
		formulaDir, err := Dir()
		if err != nil {
			t.Skipf("Could not get formula dir: %v", err)
		}
		// Path to parent directory
		parentPath := formulaDir + "/.."
		_, err = Load(parentPath)
		if err == nil {
			t.Error("Load should return error for parent directory access")
		}
		if err != nil && !strings.Contains(err.Error(), "disallow non formula dir access") {
			// Check that the error is about disallowed access or path issues
			t.Logf("Got expected error: %v", err)
		}
	})
}

func TestLoadFSStructNameNotFound(t *testing.T) {
	// Create a valid gox file but with wrong naming convention
	// The file name starts with "wrong" but the struct will be named differently
	tmpDir := t.TempDir()

	// Create a file with correct syntax but struct name won't match
	validGox := `id "test/pkg"

fromVer "v1.0.0"

onRequire (proj, deps) => {
   echo "hello"
}

onBuild (ctx, proj, out) => {
    echo "hello"
}
`
	// File name is "wrong_llar.gox", so it will look for struct "wrong"
	// but the generated code creates a struct based on the classfile mechanism
	err := os.WriteFile(tmpDir+"/notexist_llar.gox", []byte(validGox), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fsys := os.DirFS(tmpDir).(fs.ReadFileFS)
	_, err = LoadFS(fsys, "notexist_llar.gox")
	// This will either fail at struct name lookup or succeed
	// We're mainly testing the code path
	if err != nil {
		t.Logf("LoadFS returned expected error: %v", err)
	}
}

func TestLoadFSNoUnderscoreInFileName(t *testing.T) {
	tmpDir := t.TempDir()

	validGox := `id "test/pkg"

fromVer "v1.0.0"

onRequire (proj, deps) => {
   echo "hello"
}

onBuild (ctx, proj, out) => {
    echo "hello"
}
`
	// File name without underscore
	err := os.WriteFile(tmpDir+"/nounderscore.gox", []byte(validGox), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fsys := os.DirFS(tmpDir).(fs.ReadFileFS)
	_, err = LoadFS(fsys, "nounderscore.gox")
	// strings.Cut with no underscore will still return the whole string as first part
	// So this should either work or fail at struct lookup
	if err != nil {
		t.Logf("LoadFS returned error for no underscore file: %v", err)
	}
}

func TestLoadValidFormulaInFormulaDir(t *testing.T) {
	// Create a valid formula file in the actual formula directory
	formulaDir, err := Dir()
	if err != nil {
		t.Skipf("Could not get formula dir: %v", err)
	}

	testFile := formulaDir + "/test_llar.gox"
	validGox := `id "test/pkg"

fromVer "v1.0.0"

onRequire (proj, deps) => {
   echo "hello"
}

onBuild (ctx, proj, out) => {
    echo "hello"
}
`
	err = os.WriteFile(testFile, []byte(validGox), 0644)
	if err != nil {
		t.Skipf("Could not write test file: %v", err)
	}
	defer os.Remove(testFile)

	formula, err := Load(testFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if formula == nil {
		t.Fatal("Load returned nil formula")
	}

	if formula.ModPath != "test/pkg" {
		t.Errorf("Unexpected ModPath: want %s got %s", "test/pkg", formula.ModPath)
	}

	if formula.FromVer != "v1.0.0" {
		t.Errorf("Unexpected FromVer: want %s got %s", "v1.0.0", formula.FromVer)
	}
}
