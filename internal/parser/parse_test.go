package parser

import (
	"go/ast"
	"testing"

	"github.com/goplus/ixgo"
)

func TestParser_ParseAST(t *testing.T) {
	ctx := ixgo.NewContext(ixgo.SupportMultipleInterp)
	p := NewParser(ctx)

	astFile, err := p.ParseAST("testdata/hello_llar.gox")
	if err != nil {
		t.Fatalf("ParseAST failed: %v", err)
	}

	if astFile == nil {
		t.Fatal("expected non-nil ast.File")
	}

	// Check package name
	if astFile.Name == nil || astFile.Name.Name != "main" {
		t.Errorf("expected package name 'main', got %v", astFile.Name)
	}

	// Check that we have declarations
	if len(astFile.Decls) == 0 {
		t.Error("expected at least one declaration")
	}

	// Look for expected function declarations (onRequire, onBuild)
	foundFuncs := make(map[string]bool)
	for _, decl := range astFile.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			foundFuncs[fn.Name.Name] = true
		}
	}

	if !foundFuncs["main"] {
		t.Error("expected to find main function")
	}
}
