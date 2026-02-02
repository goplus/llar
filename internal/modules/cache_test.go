package modules

import (
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/goplus/ixgo/xgobuild"
	"github.com/goplus/llar/internal/formula"
	"github.com/goplus/llar/mod/module"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
)

func TestNewClassfileCache(t *testing.T) {
	tests := []struct {
		name           string
		localDir       string
		wantSearchPath string
	}{
		{
			name:           "with specified local dir",
			localDir:       "/custom/path",
			wantSearchPath: "/custom/path",
		},
		{
			name:           "with empty local dir defaults to current directory",
			localDir:       "",
			wantSearchPath: ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := newClassfileCache(tt.localDir, nil)
			if cache == nil {
				t.Fatal("newClassfileCache returned nil")
			}
			if cache.formulas == nil {
				t.Error("formulas map is nil")
			}
			if cache.comparators == nil {
				t.Error("comparators map is nil")
			}
			if len(cache.searchPaths) != 1 || cache.searchPaths[0] != tt.wantSearchPath {
				t.Errorf("searchPaths = %v, want [%s]", cache.searchPaths, tt.wantSearchPath)
			}
		})
	}
}

func TestFindMaxFromVer_NoDirectory(t *testing.T) {
	cache := newClassfileCache("/nonexistent/path", nil)
	mod := module.Version{Path: "test/pkg", Version: "1.0.0"}

	compare := func(v1, v2 module.Version) int {
		if v1.Version < v2.Version {
			return -1
		} else if v1.Version > v2.Version {
			return 1
		}
		return 0
	}

	_, _, err := cache.findMaxFromVer(mod, compare)
	if err == nil {
		t.Error("expected error when no formula found, got nil")
	}
}

func TestFindMaxFromVer_WithTestdata(t *testing.T) {
	// Use testdata directory as the search path
	testdataDir := "testdata"
	cache := newClassfileCache(testdataDir, nil)

	// Test with DaveGamble/cJSON which has multiple versions
	mod := module.Version{Path: "github.com/DaveGamble/cJSON", Version: "1.7.18"}

	compare := func(v1, v2 module.Version) int {
		if v1.Version < v2.Version {
			return -1
		} else if v1.Version > v2.Version {
			return 1
		}
		return 0
	}

	maxVer, formulaPath, err := cache.findMaxFromVer(mod, compare)
	if err != nil {
		t.Fatalf("findMaxFromVer failed: %v", err)
	}

	// Should find version 1.5.0 (highest version <= 1.7.18)
	if maxVer != "1.5.0" {
		t.Errorf("maxFromVer = %q, want %q", maxVer, "1.5.0")
	}

	if !filepath.IsAbs(formulaPath) {
		formulaPath, _ = filepath.Abs(formulaPath)
	}
	if _, err := os.Stat(formulaPath); os.IsNotExist(err) {
		t.Errorf("formula path does not exist: %s", formulaPath)
	}
}

func TestClassfileCache_ComparatorOf_WithMock(t *testing.T) {
	// Create mock repo pointing to testdata
	testdataDir, _ := filepath.Abs("testdata")

	// Use a temp dir for formula download destination
	tempDir := t.TempDir()
	cache := newClassfileCache(tempDir, nil)

	// Pre-populate the temp dir with testdata (simulating lazyDownloadFormula)
	// Copy DaveGamble/cJSON to temp dir
	srcDir := filepath.Join(testdataDir, "DaveGamble", "cJSON")
	destDir := filepath.Join(tempDir, "DaveGamble", "cJSON")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}
	if err := os.CopyFS(destDir, os.DirFS(srcDir)); err != nil {
		t.Fatalf("failed to copy testdata: %v", err)
	}

	modPath := "github.com/DaveGamble/cJSON"

	// This should use the custom comparator from CJSON_cmp.gox
	comp, err := cache.comparatorOf(modPath)
	if err != nil {
		t.Skipf("comparatorOf failed (env not configured): %v", err)
	}

	// Test the comparator works
	v1 := module.Version{Path: modPath, Version: "1.0"}
	v2 := module.Version{Path: modPath, Version: "2.0"}

	if result := comp(v1, v2); result >= 0 {
		t.Errorf("comp(1.0, 2.0) = %d, want < 0", result)
	}
	if result := comp(v2, v1); result <= 0 {
		t.Errorf("comp(2.0, 1.0) = %d, want > 0", result)
	}
}

func TestClassfileCache_ComparatorOf_Caching(t *testing.T) {
	testdataDir, _ := filepath.Abs("testdata")
	tempDir := t.TempDir()
	cache := newClassfileCache(tempDir, nil)

	// Pre-populate with zlib (uses default comparator)
	srcDir := filepath.Join(testdataDir, "madler", "zlib")
	destDir := filepath.Join(tempDir, "madler", "zlib")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}
	if err := os.CopyFS(destDir, os.DirFS(srcDir)); err != nil {
		t.Fatalf("failed to copy testdata: %v", err)
	}

	modPath := "github.com/madler/zlib"

	comp1, err := cache.comparatorOf(modPath)
	if err != nil {
		t.Skipf("comparatorOf failed: %v", err)
	}

	// Second call should return cached comparator
	comp2, err := cache.comparatorOf(modPath)
	if err != nil {
		t.Fatalf("second comparatorOf failed: %v", err)
	}

	// Both should produce same results
	v1 := module.Version{Path: modPath, Version: "1.0"}
	v2 := module.Version{Path: modPath, Version: "2.0"}

	if comp1(v1, v2) != comp2(v1, v2) {
		t.Error("cached comparator produces different results")
	}
}

func TestDefaultFormulaSuffix(t *testing.T) {
	if _defaultFormulaSuffix != "_llar.gox" {
		t.Errorf("_defaultFormulaSuffix = %q, want %q", _defaultFormulaSuffix, "_llar.gox")
	}
}

func TestFromVerOf(t *testing.T) {
	tests := []struct {
		name        string
		formulaPath string
		wantFromVer string
		wantErr     bool
	}{
		{
			name:        "valid formula with fromVer 1.5.0",
			formulaPath: "testdata/DaveGamble/cJSON/1.5.0/CJSON_llar.gox",
			wantFromVer: "1.5.0",
			wantErr:     false,
		},
		{
			name:        "valid formula with fromVer 1.0.0",
			formulaPath: "testdata/DaveGamble/cJSON/1.0.0/CJSON_llar.gox",
			wantFromVer: "1.0.0",
			wantErr:     false,
		},
		{
			name:        "valid formula with fromVer 2.0.0",
			formulaPath: "testdata/DaveGamble/cJSON/2.0.0/CJSON_llar.gox",
			wantFromVer: "2.0.0",
			wantErr:     false,
		},
		{
			name:        "nonexistent file",
			formulaPath: "testdata/nonexistent/formula_llar.gox",
			wantFromVer: "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fromVerOf(tt.formulaPath)
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
			wantErr:     false,
		},
		{
			name: "fromVer with backticks",
			source: "id `test/pkg`\nfromVer `2.0.0`\n",
			wantFromVer: "2.0.0",
			wantErr:     false,
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
			wantErr:     false,
		},
		{
			name:        "empty source",
			source:      "",
			wantFromVer: "",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := token.NewFileSet()
			astFile, err := parser.ParseEntry(fs, "test_llar.gox", []byte(tt.source), parser.Config{
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
			name:    "string literal with double quotes",
			source:  `fromVer "1.0.0"`,
			fnName:  "fromVer",
			want:    "1.0.0",
			wantErr: false,
		},
		{
			name:    "string literal with backticks",
			source:  "fromVer `2.0.0`",
			fnName:  "fromVer",
			want:    "2.0.0",
			wantErr: false,
		},
		{
			name:    "empty argument",
			source:  `fromVer ""`,
			fnName:  "fromVer",
			want:    "",
			wantErr: true,
		},
		{
			name:    "id function call",
			source:  `id "github.com/test/pkg"`,
			fnName:  "id",
			want:    "github.com/test/pkg",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := token.NewFileSet()
			// Parse as a simple expression statement
			astFile, err := parser.ParseEntry(fs, "test_llar.gox", []byte(tt.source), parser.Config{
				ClassKind: xgobuild.ClassKind,
			})
			if err != nil {
				t.Fatalf("failed to parse source: %v", err)
			}

			// Find the call expression in the AST
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
	// Test case where function has no arguments - manually construct the AST
	callExpr := &ast.CallExpr{
		Fun:  &ast.Ident{Name: "testFunc"},
		Args: []ast.Expr{},
	}

	_, err := parseCallArg(callExpr, "testFunc")
	if err == nil {
		t.Error("parseCallArg() expected error for no arguments, got nil")
	}
	if err != nil && err.Error() != "failed to parse testFunc from AST: no argument" {
		t.Errorf("parseCallArg() error = %q, want %q", err.Error(), "failed to parse testFunc from AST: no argument")
	}
}

func TestParseCallArg_NonStringArg(t *testing.T) {
	// Test case where argument is not a string literal (e.g., identifier)
	callExpr := &ast.CallExpr{
		Fun: &ast.Ident{Name: "testFunc"},
		Args: []ast.Expr{
			&ast.Ident{Name: "someVariable"},
		},
	}

	got, err := parseCallArg(callExpr, "testFunc")
	if err != nil {
		t.Errorf("parseCallArg() unexpected error = %v", err)
	}
	// When arg is not a BasicLit, it returns empty string without error
	if got != "" {
		t.Errorf("parseCallArg() = %q, want empty string for non-BasicLit arg", got)
	}
}

func TestFormulaOf_Success(t *testing.T) {
	// Setup: copy testdata to formula.Dir()
	formulaDir, err := setupTestFormulaDir(t)
	if err != nil {
		t.Fatalf("failed to setup formula dir: %v", err)
	}

	cache := newClassfileCache(formulaDir, nil)
	mod := module.Version{Path: "github.com/DaveGamble/cJSON", Version: "1.7.18"}

	f, err := cache.formulaOf(mod)
	if err != nil {
		t.Fatalf("formulaOf failed: %v", err)
	}
	if f == nil {
		t.Error("formulaOf returned nil formula")
	}
}

func TestFormulaOf_Caching(t *testing.T) {
	// Setup: copy testdata to formula.Dir()
	formulaDir, err := setupTestFormulaDir(t)
	if err != nil {
		t.Fatalf("failed to setup formula dir: %v", err)
	}

	cache := newClassfileCache(formulaDir, nil)
	mod := module.Version{Path: "github.com/DaveGamble/cJSON", Version: "1.7.18"}

	// First call - loads from file
	f1, err := cache.formulaOf(mod)
	if err != nil {
		t.Fatalf("first formulaOf failed: %v", err)
	}

	// Second call - should return cached formula
	f2, err := cache.formulaOf(mod)
	if err != nil {
		t.Fatalf("second formulaOf failed: %v", err)
	}

	// Both should return the same cached formula instance
	if f1 != f2 {
		t.Error("formulaOf did not return cached formula on second call")
	}
}

func TestFormulaOf_ComparatorError(t *testing.T) {
	// Use a cache with a loadRemoteFormula that always fails
	cache := newClassfileCache("/nonexistent", func(modPath string) error {
		return fmt.Errorf("mock remote load error")
	})

	mod := module.Version{Path: "github.com/nonexistent/pkg", Version: "1.0.0"}

	_, err := cache.formulaOf(mod)
	if err == nil {
		t.Error("formulaOf should fail when comparatorOf fails")
	}
}

func TestFormulaOf_FindMaxFromVerError(t *testing.T) {
	// Create cache with empty search path (no formulas)
	tempDir := t.TempDir()
	cache := newClassfileCache(tempDir, nil)

	mod := module.Version{Path: "github.com/nonexistent/pkg", Version: "1.0.0"}

	_, err := cache.formulaOf(mod)
	if err == nil {
		t.Error("formulaOf should fail when no formula found")
	}
}

func TestFormulaOf_LoadError(t *testing.T) {
	// formula.Load() requires files to be under formula.Dir()
	// Create a valid formula syntax file in formula.Dir() but with invalid classfile structure
	formulaDir, err := formula.Dir()
	if err != nil {
		t.Fatalf("failed to get formula dir: %v", err)
	}

	// Create formula structure under formula.Dir()
	destDir := filepath.Join(formulaDir, "invalid", "pkg", "1.0.0")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	t.Cleanup(func() {
		os.RemoveAll(filepath.Join(formulaDir, "invalid"))
	})

	// Create a formula file with valid DSL syntax but that will fail during Load
	// (e.g., missing required classfile setup or invalid formula structure)
	// Using a file that parses but fails at ixgo build stage
	formulaContent := `id "invalid/pkg"
fromVer "1.0.0"
onBuild (ctx, proj, out) => {
    undefinedFunction()
}
`
	if err := os.WriteFile(filepath.Join(destDir, "Invalid_llar.gox"), []byte(formulaContent), 0644); err != nil {
		t.Fatalf("failed to write formula: %v", err)
	}

	cache := newClassfileCache(formulaDir, nil)
	mod := module.Version{Path: "github.com/invalid/pkg", Version: "1.0.0"}

	_, err = cache.formulaOf(mod)
	if err == nil {
		t.Error("formulaOf should fail when formula.Load fails")
	}
}

func TestFormulaOf_DifferentVersionsSameFormula(t *testing.T) {
	// Setup: copy testdata to formula.Dir()
	formulaDir, err := setupTestFormulaDir(t)
	if err != nil {
		t.Fatalf("failed to setup formula dir: %v", err)
	}

	cache := newClassfileCache(formulaDir, nil)

	// Both versions should use the same formula (1.5.0) since it's the max <= requested
	mod1 := module.Version{Path: "github.com/DaveGamble/cJSON", Version: "1.7.18"}
	mod2 := module.Version{Path: "github.com/DaveGamble/cJSON", Version: "1.6.0"}

	f1, err := cache.formulaOf(mod1)
	if err != nil {
		t.Fatalf("formulaOf(1.7.18) failed: %v", err)
	}

	f2, err := cache.formulaOf(mod2)
	if err != nil {
		t.Fatalf("formulaOf(1.6.0) failed: %v", err)
	}

	// Both should return the same cached formula (fromVer 1.5.0)
	if f1 != f2 {
		t.Error("different versions mapping to same fromVer should return same cached formula")
	}
}

// setupTestFormulaDir copies testdata to formula.Dir() for testing
func setupTestFormulaDir(t *testing.T) (string, error) {
	t.Helper()

	formulaDir, err := formula.Dir()
	if err != nil {
		return "", err
	}

	testdataDir, err := filepath.Abs("testdata")
	if err != nil {
		return "", err
	}

	// Copy DaveGamble/cJSON to formula dir
	srcDir := filepath.Join(testdataDir, "DaveGamble", "cJSON")
	destDir := filepath.Join(formulaDir, "DaveGamble", "cJSON")

	// Clean up any existing test data
	os.RemoveAll(destDir)

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}
	if err := os.CopyFS(destDir, os.DirFS(srcDir)); err != nil {
		return "", err
	}

	// Register cleanup
	t.Cleanup(func() {
		os.RemoveAll(destDir)
	})

	return formulaDir, nil
}
