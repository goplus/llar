package modules

import (
	"errors"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/goplus/ixgo/xgobuild"
	"github.com/goplus/llar/mod/module"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
)

func TestNewModuleSource(t *testing.T) {
	fsys := os.DirFS("testdata")
	source := newModuleSource(fsys, nil)

	if source == nil {
		t.Fatal("newModuleSource returned nil")
	}
	if source.fsys != fsys {
		t.Error("fsys not set correctly")
	}
	if source.modules == nil {
		t.Error("modules map is nil")
	}
}

func TestModuleSource_Module(t *testing.T) {
	fsys := os.DirFS("testdata")
	source := newModuleSource(fsys, nil)

	// First call should create new formulaModule
	mod := source.module("DaveGamble/cJSON")
	if mod == nil {
		t.Fatal("module() returned nil")
	}
	if mod.modPath != "DaveGamble/cJSON" {
		t.Errorf("modPath = %q, want %q", mod.modPath, "DaveGamble/cJSON")
	}

	// Second call should return cached formulaModule
	mod2 := source.module("DaveGamble/cJSON")
	if mod != mod2 {
		t.Error("module() did not return cached instance")
	}
}

func TestModuleSource_ModuleWithSyncFn(t *testing.T) {
	fsys := os.DirFS("testdata")
	syncCalled := false
	syncFn := func(modPath string) error {
		syncCalled = true
		if modPath != "DaveGamble/cJSON" {
			t.Errorf("syncFn called with %q, want %q", modPath, "DaveGamble/cJSON")
		}
		return nil
	}

	source := newModuleSource(fsys, syncFn)
	mod := source.module("DaveGamble/cJSON")

	if !syncCalled {
		t.Error("syncFn was not called")
	}

	// Verify no error stored
	_, err := mod.comparator()
	if err != nil {
		t.Fatalf("comparator() failed: %v", err)
	}

	// Second call should use cache, not call syncFn again
	syncCalled = false
	_ = source.module("DaveGamble/cJSON")
	if syncCalled {
		t.Error("syncFn should not be called for cached module")
	}
}

func TestModuleSource_ModuleSyncError(t *testing.T) {
	fsys := os.DirFS("testdata")
	expectedErr := errors.New("sync failed")
	syncFn := func(modPath string) error {
		return expectedErr
	}

	source := newModuleSource(fsys, syncFn)
	mod := source.module("DaveGamble/cJSON")

	// Error should be deferred to comparator() or at()
	_, err := mod.comparator()
	if err != expectedErr {
		t.Errorf("comparator() error = %v, want %v", err, expectedErr)
	}

	_, err = mod.at("1.0.0")
	if err != expectedErr {
		t.Errorf("at() error = %v, want %v", err, expectedErr)
	}
}

func TestModuleSource_SyncWritePermissionDenied(t *testing.T) {
	// Create a read-only directory to simulate permission denied
	tmpDir := t.TempDir()
	readonlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.MkdirAll(readonlyDir, 0555); err != nil {
		t.Fatalf("failed to create readonly dir: %v", err)
	}

	fsys := os.DirFS(tmpDir)
	syncFn := func(modPath string) error {
		// Try to create a file in the readonly directory
		targetPath := filepath.Join(readonlyDir, "test.txt")
		_, err := os.Create(targetPath)
		return err
	}

	source := newModuleSource(fsys, syncFn)
	mod := source.module("test/module")

	_, err := mod.comparator()
	if err == nil {
		t.Error("comparator() should fail when sync has permission error")
	}
	if !os.IsPermission(err) {
		t.Errorf("expected permission error, got: %v", err)
	}
}

func TestModuleSource_SyncModuleNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	fsys := os.DirFS(tmpDir)

	syncFn := func(modPath string) error {
		// Simulate module not found in remote repository
		return fs.ErrNotExist
	}

	source := newModuleSource(fsys, syncFn)
	mod := source.module("nonexistent/module")

	_, err := mod.at("1.0.0")
	if err == nil {
		t.Error("at() should fail when module not found")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected fs.ErrNotExist, got: %v", err)
	}
}

func TestNewFormulaModule(t *testing.T) {
	fsys := os.DirFS("testdata")
	mod := newFormulaModule(fsys, "DaveGamble/cJSON")

	if mod == nil {
		t.Fatal("newFormulaModule returned nil")
	}
	if mod.modPath != "DaveGamble/cJSON" {
		t.Errorf("modPath = %q, want %q", mod.modPath, "DaveGamble/cJSON")
	}
	if mod.formulas == nil {
		t.Error("formulas map is nil")
	}
	if mod.cmp != nil {
		t.Error("cmp should be nil initially")
	}
}

func TestFormulaModule_Comparator(t *testing.T) {
	fsys := os.DirFS("testdata")
	mod := newFormulaModule(fsys, "DaveGamble/cJSON")

	// First call should load comparator
	cmp, err := mod.comparator()
	if err != nil {
		t.Fatalf("comparator() failed: %v", err)
	}
	if cmp == nil {
		t.Fatal("comparator() returned nil")
	}

	// Test comparator works correctly
	v1 := module.Version{Path: "DaveGamble/cJSON", Version: "1.0"}
	v2 := module.Version{Path: "DaveGamble/cJSON", Version: "2.0"}
	if result := cmp(v1, v2); result >= 0 {
		t.Errorf("cmp(1.0, 2.0) = %d, want < 0", result)
	}
	if result := cmp(v2, v1); result <= 0 {
		t.Errorf("cmp(2.0, 1.0) = %d, want > 0", result)
	}
	if result := cmp(v1, v1); result != 0 {
		t.Errorf("cmp(1.0, 1.0) = %d, want 0", result)
	}

	// Second call should return cached comparator
	cmp2, err := mod.comparator()
	if err != nil {
		t.Fatalf("second comparator() failed: %v", err)
	}
	// Verify caching by checking the same function produces same results
	if cmp(v1, v2) != cmp2(v1, v2) {
		t.Error("cached comparator produces different results")
	}
}

func TestFormulaModule_ComparatorDefaultFallback(t *testing.T) {
	fsys := os.DirFS("testdata")
	// madler/zlib has no comparator file, should use default
	mod := newFormulaModule(fsys, "madler/zlib")

	cmp, err := mod.comparator()
	if err != nil {
		t.Fatalf("comparator() failed: %v", err)
	}
	if cmp == nil {
		t.Fatal("comparator() returned nil")
	}

	// Default comparator should work with gnu version comparison
	v1 := module.Version{Path: "madler/zlib", Version: "1.0.0"}
	v2 := module.Version{Path: "madler/zlib", Version: "2.0.0"}
	if result := cmp(v1, v2); result >= 0 {
		t.Errorf("default cmp(1.0.0, 2.0.0) = %d, want < 0", result)
	}
}

func TestFormulaModule_At(t *testing.T) {
	fsys := os.DirFS("testdata")
	mod := newFormulaModule(fsys, "DaveGamble/cJSON")

	// Test getting formula for version 1.7.18 (should match fromVer 1.5.0)
	f, err := mod.at("1.7.18")
	if err != nil {
		t.Fatalf("at() failed: %v", err)
	}
	if f == nil {
		t.Fatal("at() returned nil")
	}
	if f.FromVer != "1.5.0" {
		t.Errorf("FromVer = %q, want %q", f.FromVer, "1.5.0")
	}

	// Test caching
	f2, err := mod.at("1.7.18")
	if err != nil {
		t.Fatalf("second at() failed: %v", err)
	}
	if f != f2 {
		t.Error("at() did not return cached formula")
	}
}

func TestFormulaModule_AtVersionMatching(t *testing.T) {
	fsys := os.DirFS("testdata")
	mod := newFormulaModule(fsys, "DaveGamble/cJSON")

	tests := []struct {
		version     string
		wantFromVer string
	}{
		{"1.0.0", "1.0.0"},
		{"1.2.0", "1.0.0"},
		{"1.5.0", "1.5.0"},
		{"1.7.18", "1.5.0"},
		{"2.0.0", "2.0.0"},
		{"2.5.0", "2.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			f, err := mod.at(tt.version)
			if err != nil {
				t.Fatalf("at(%q) failed: %v", tt.version, err)
			}
			if f.FromVer != tt.wantFromVer {
				t.Errorf("at(%q).FromVer = %q, want %q", tt.version, f.FromVer, tt.wantFromVer)
			}
		})
	}
}

func TestFormulaModule_AtNoFormula(t *testing.T) {
	fsys := os.DirFS("testdata")
	mod := newFormulaModule(fsys, "DaveGamble/cJSON")

	// Version lower than all fromVer should fail
	_, err := mod.at("0.5.0")
	if err == nil {
		t.Error("at() should fail for version lower than all fromVer")
	}
}

func TestFormulaModule_AtNonexistentModule(t *testing.T) {
	fsys := os.DirFS("testdata")
	mod := newFormulaModule(fsys, "nonexistent/module")

	_, err := mod.at("1.0.0")
	if err == nil {
		t.Error("at() should fail for nonexistent module")
	}
}

func TestFormulaModule_FindMaxFromVer(t *testing.T) {
	fsys := os.DirFS("testdata")
	mod := newFormulaModule(fsys, "DaveGamble/cJSON")

	cmp, _ := mod.comparator()
	target := module.Version{Path: "DaveGamble/cJSON", Version: "1.7.18"}

	fromVer, path, err := mod.findMaxFromVer(target, cmp)
	if err != nil {
		t.Fatalf("findMaxFromVer() failed: %v", err)
	}
	if fromVer != "1.5.0" {
		t.Errorf("fromVer = %q, want %q", fromVer, "1.5.0")
	}
	if path == "" {
		t.Error("path is empty")
	}
}

func TestFromVerOf(t *testing.T) {
	fsys := os.DirFS("testdata").(fs.ReadFileFS)

	tests := []struct {
		name        string
		path        string
		wantFromVer string
		wantErr     bool
	}{
		{
			name:        "cJSON 1.0.0",
			path:        "DaveGamble/cJSON/1.0.0/CJSON_llar.gox",
			wantFromVer: "1.0.0",
		},
		{
			name:        "cJSON 1.5.0",
			path:        "DaveGamble/cJSON/1.5.0/CJSON_llar.gox",
			wantFromVer: "1.5.0",
		},
		{
			name:        "cJSON 2.0.0",
			path:        "DaveGamble/cJSON/2.0.0/CJSON_llar.gox",
			wantFromVer: "2.0.0",
		},
		{
			name:    "nonexistent file",
			path:    "nonexistent/formula_llar.gox",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fromVerOf(fsys, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("fromVerOf() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantFromVer {
				t.Errorf("fromVerOf() = %q, want %q", got, tt.wantFromVer)
			}
		})
	}
}

func TestFromVerFrom(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		wantFromVer string
		wantErr     bool
	}{
		{
			name: "valid fromVer call",
			source: `
id "test/pkg"
fromVer "1.2.3"
`,
			wantFromVer: "1.2.3",
		},
		{
			name:        "fromVer with backticks",
			source:      "id `test/pkg`\nfromVer `2.0.0`\n",
			wantFromVer: "2.0.0",
		},
		{
			name: "no fromVer call",
			source: `
id "test/pkg"
onBuild (ctx, proj, out) => {
    echo "hello"
}
`,
			wantFromVer: "",
		},
		{
			name:        "empty source",
			source:      "",
			wantFromVer: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			astFile, err := parser.ParseEntry(fset, "test_llar.gox", []byte(tt.source), parser.Config{
				ClassKind: xgobuild.ClassKind,
			})
			if err != nil {
				t.Fatalf("failed to parse source: %v", err)
			}

			got, err := fromVerFrom(astFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("fromVerFrom() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantFromVer {
				t.Errorf("fromVerFrom() = %q, want %q", got, tt.wantFromVer)
			}
		})
	}
}

func TestParseCallArg(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		fnName  string
		want    string
		wantErr bool
	}{
		{
			name:   "string literal with double quotes",
			source: `fromVer "1.0.0"`,
			fnName: "fromVer",
			want:   "1.0.0",
		},
		{
			name:   "string literal with backticks",
			source: "fromVer `2.0.0`",
			fnName: "fromVer",
			want:   "2.0.0",
		},
		{
			name:    "empty argument",
			source:  `fromVer ""`,
			fnName:  "fromVer",
			want:    "",
			wantErr: true,
		},
		{
			name:   "id function call",
			source: `id "test/pkg"`,
			fnName: "id",
			want:   "test/pkg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			astFile, err := parser.ParseEntry(fset, "test_llar.gox", []byte(tt.source), parser.Config{
				ClassKind: xgobuild.ClassKind,
			})
			if err != nil {
				t.Fatalf("failed to parse source: %v", err)
			}

			var callExpr *ast.CallExpr
			ast.Inspect(astFile, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if ident, ok := c.Fun.(*ast.Ident); ok && ident.Name == tt.fnName {
						callExpr = c
						return false
					}
				}
				return true
			})

			if callExpr == nil {
				t.Fatalf("failed to find %s call in AST", tt.fnName)
			}

			got, err := parseCallArg(callExpr, tt.fnName)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseCallArg() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseCallArg() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseCallArg_NoArgument(t *testing.T) {
	callExpr := &ast.CallExpr{
		Fun:  &ast.Ident{Name: "testFunc"},
		Args: []ast.Expr{},
	}

	_, err := parseCallArg(callExpr, "testFunc")
	if err == nil {
		t.Error("parseCallArg() expected error for no arguments")
	}
}

func TestParseCallArg_NonStringArg(t *testing.T) {
	callExpr := &ast.CallExpr{
		Fun: &ast.Ident{Name: "testFunc"},
		Args: []ast.Expr{
			&ast.Ident{Name: "someVariable"},
		},
	}

	_, err := parseCallArg(callExpr, "testFunc")
	if err == nil {
		t.Error("parseCallArg() expected error for non-string argument")
	}
}

func TestIntegration_ModuleSourceToFormula(t *testing.T) {
	fsys := os.DirFS("testdata")
	source := newModuleSource(fsys, nil)

	// Get formula via chained call
	f, err := source.module("DaveGamble/cJSON").at("1.7.18")
	if err != nil {
		t.Fatalf("at() failed: %v", err)
	}

	// Verify formula
	if f.ModPath != "DaveGamble/cJSON" {
		t.Errorf("ModPath = %q, want %q", f.ModPath, "DaveGamble/cJSON")
	}
	if f.FromVer != "1.5.0" {
		t.Errorf("FromVer = %q, want %q", f.FromVer, "1.5.0")
	}
}

func TestIntegration_MultipleModules(t *testing.T) {
	fsys := os.DirFS("testdata")
	source := newModuleSource(fsys, nil)

	modules := []struct {
		path    string
		version string
	}{
		{"DaveGamble/cJSON", "1.7.18"},
		{"madler/zlib", "1.5.0"},
	}

	for _, m := range modules {
		f, err := source.module(m.path).at(m.version)
		if err != nil {
			t.Errorf("at(%q) for %q failed: %v", m.version, m.path, err)
			continue
		}

		if f == nil {
			t.Errorf("at(%q) for %q returned nil", m.version, m.path)
		}
	}
}
