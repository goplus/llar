package formula

import (
	"io/fs"
	"os"
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
