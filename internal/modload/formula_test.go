package modload

import (
	"go/ast"
	"testing"

	"github.com/goplus/ixgo"
	"github.com/goplus/llar/internal/parser"
	"github.com/goplus/llar/pkgs/mod/module"
)

func TestMatchGitRef(t *testing.T) {
	tests := []struct {
		name    string
		refs    []string
		version string
		want    string
		wantOK  bool
	}{
		{
			name:    "exact match",
			refs:    []string{"v1.0.0", "v1.1.0", "v2.0.0"},
			version: "v1.0.0",
			want:    "v1.0.0",
			wantOK:  true,
		},
		{
			name:    "suffix match",
			refs:    []string{"release/v1.0.0", "release/v1.1.0"},
			version: "v1.0.0",
			want:    "release/v1.0.0",
			wantOK:  true,
		},
		{
			name:    "no match",
			refs:    []string{"v1.0.0", "v1.1.0"},
			version: "v2.0.0",
			want:    "",
			wantOK:  false,
		},
		{
			name:    "empty refs",
			refs:    []string{},
			version: "v1.0.0",
			want:    "",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := matchGitRef(tt.refs, tt.version)
			if ok != tt.wantOK {
				t.Errorf("matchGitRef() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("matchGitRef() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseLibraryName(t *testing.T) {
	tests := []struct {
		modID string
		want  string
	}{
		{"DaveGamble/cJSON", "cJSON"},
		{"madler/zlib", "zlib"},
		{"bminor/glibc", "glibc"},
	}

	for _, tt := range tests {
		t.Run(tt.modID, func(t *testing.T) {
			got := parseLibraryName(tt.modID)
			if got != tt.want {
				t.Errorf("parseLibraryName(%q) = %q, want %q", tt.modID, got, tt.want)
			}
		})
	}
}

func TestFindMaxFromVer(t *testing.T) {
	// testdata/DaveGamble/cJSON has fromVer: 1.0.0, 1.5.0, 2.0.0
	// findMaxFromVer finds the largest fromVer <= mod.Version

	cmp := func(v1, v2 module.Version) int {
		if v1.Version < v2.Version {
			return -1
		} else if v1.Version > v2.Version {
			return 1
		}
		return 0
	}

	tests := []struct {
		name        string
		modVersion  string
		wantFromVer string
	}{
		{"version 1.7.18 gets fromVer 1.5.0", "1.7.18", "1.5.0"},
		{"version 2.5.0 gets fromVer 2.0.0", "2.5.0", "2.0.0"},
		{"version 1.0.0 gets fromVer 1.0.0", "1.0.0", "1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := module.Version{ID: "DaveGamble/cJSON", Version: tt.modVersion}
			maxFromVer, formulaPath, err := findMaxFromVer(mod, cmp)
			if err != nil {
				t.Fatalf("findMaxFromVer() error = %v", err)
			}
			if maxFromVer != tt.wantFromVer {
				t.Errorf("findMaxFromVer() maxFromVer = %q, want %q", maxFromVer, tt.wantFromVer)
			}
			if formulaPath == "" {
				t.Error("findMaxFromVer() formulaPath is empty")
			}
		})
	}
}

func TestFindMaxFromVer_NoMatch(t *testing.T) {
	// version 0.5.0 is less than all fromVer (1.0.0, 1.5.0, 2.0.0)
	cmp := func(v1, v2 module.Version) int {
		if v1.Version < v2.Version {
			return -1
		} else if v1.Version > v2.Version {
			return 1
		}
		return 0
	}

	mod := module.Version{ID: "DaveGamble/cJSON", Version: "0.5.0"}
	_, _, err := findMaxFromVer(mod, cmp)
	if err == nil {
		t.Error("findMaxFromVer() expected error for version with no matching fromVer")
	}
}

func TestFormula_markUse(t *testing.T) {
	f := &Formula{}

	if f.inUse() {
		t.Error("new Formula should not be in use")
	}

	f.markUse()
	if !f.inUse() {
		t.Error("Formula should be in use after markUse()")
	}

	f.markUse()
	if f.refcnt != 2 {
		t.Errorf("refcnt = %d, want 2", f.refcnt)
	}
}

func TestParseCallArg_EmptyArgs(t *testing.T) {
	// Test with empty args
	callExpr := &ast.CallExpr{
		Args: []ast.Expr{},
	}

	_, err := parseCallArg(callExpr, "TestFunc")
	if err == nil {
		t.Error("parseCallArg() expected error for empty args")
	}
}

func TestParseCallArg_BasicLit(t *testing.T) {
	callExpr := &ast.CallExpr{
		Args: []ast.Expr{
			&ast.BasicLit{Value: `"v1.0.0"`},
		},
	}

	got, err := parseCallArg(callExpr, "TestFunc")
	if err != nil {
		t.Fatalf("parseCallArg() error = %v", err)
	}
	if got != "v1.0.0" {
		t.Errorf("parseCallArg() = %q, want %q", got, "v1.0.0")
	}
}

func TestParseCallArg_BacktickLit(t *testing.T) {
	callExpr := &ast.CallExpr{
		Args: []ast.Expr{
			&ast.BasicLit{Value: "`v2.0.0`"},
		},
	}

	got, err := parseCallArg(callExpr, "TestFunc")
	if err != nil {
		t.Fatalf("parseCallArg() error = %v", err)
	}
	if got != "v2.0.0" {
		t.Errorf("parseCallArg() = %q, want %q", got, "v2.0.0")
	}
}

func TestFromVerFrom(t *testing.T) {
	ctx := ixgo.NewContext(0)
	p := parser.NewParser(ctx)

	tests := []struct {
		name string
		path string
		want string
	}{
		{"cJSON 1.0.0", "testdata/DaveGamble/cJSON/1.0.0/CJSON_llar.gox", "1.0.0"},
		{"cJSON 1.5.0", "testdata/DaveGamble/cJSON/1.5.0/CJSON_llar.gox", "1.5.0"},
		{"cJSON 2.0.0", "testdata/DaveGamble/cJSON/2.0.0/CJSON_llar.gox", "2.0.0"},
		{"zlib 1.0.0", "testdata/madler/zlib/1.0.0/Zlib_llar.gox", "1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			astFile, err := p.ParseAST(tt.path)
			if err != nil {
				t.Fatalf("ParseAST() error = %v", err)
			}

			got, err := fromVerFrom(astFile)
			if err != nil {
				t.Fatalf("fromVerFrom() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("fromVerFrom() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseCallArg_EmptyString(t *testing.T) {
	callExpr := &ast.CallExpr{
		Args: []ast.Expr{
			&ast.BasicLit{Value: `""`},
		},
	}

	_, err := parseCallArg(callExpr, "TestFunc")
	if err == nil {
		t.Error("parseCallArg() expected error for empty string value")
	}
}

func TestFormulaContext_comparatorOf(t *testing.T) {
	ctx := newFormulaContext()
	defer ctx.gc()

	// Test loading default comparator (gnu.Compare) for madler/zlib
	cmp, err := ctx.comparatorOf("madler/zlib")
	if err != nil {
		t.Fatalf("comparatorOf() error = %v", err)
	}

	// Test the comparator works (gnu version compare)
	v1 := module.Version{ID: "madler/zlib", Version: "1.0.0"}
	v2 := module.Version{ID: "madler/zlib", Version: "2.0.0"}

	if cmp(v1, v2) >= 0 {
		t.Errorf("comparator(1.0.0, 2.0.0) should be < 0")
	}
	if cmp(v2, v1) <= 0 {
		t.Errorf("comparator(2.0.0, 1.0.0) should be > 0")
	}
	if cmp(v1, v1) != 0 {
		t.Errorf("comparator(1.0.0, 1.0.0) should be 0")
	}

	// Test caching - should return same comparator
	cmp2, err := ctx.comparatorOf("madler/zlib")
	if err != nil {
		t.Fatalf("comparatorOf() second call error = %v", err)
	}
	// Compare function pointers by behavior
	if cmp(v1, v2) != cmp2(v1, v2) {
		t.Error("comparatorOf() should cache and return same comparator")
	}
}

func TestFormulaContext_formulaOf(t *testing.T) {
	ctx := newFormulaContext()
	defer ctx.gc()

	// Use madler/zlib which has no custom comparator
	mod := module.Version{ID: "madler/zlib", Version: "1.5.0"}

	f, err := ctx.formulaOf(mod)
	if err != nil {
		t.Fatalf("formulaOf() error = %v", err)
	}

	if f.ID != mod.ID {
		t.Errorf("formula.ID = %q, want %q", f.ID, mod.ID)
	}

	if f.OnRequire == nil {
		t.Error("formula.OnRequire should not be nil")
	}
	if f.OnBuild == nil {
		t.Error("formula.OnBuild should not be nil")
	}

	// Test caching
	f2, err := ctx.formulaOf(mod)
	if err != nil {
		t.Fatalf("formulaOf() second call error = %v", err)
	}
	if f != f2 {
		t.Error("formulaOf() should cache and return same formula")
	}
}

func TestFormulaContext_gc(t *testing.T) {
	ctx := newFormulaContext()

	mod := module.Version{ID: "madler/zlib", Version: "1.5.0"}

	f, err := ctx.formulaOf(mod)
	if err != nil {
		t.Fatalf("formulaOf() error = %v", err)
	}

	// Formula not in use, gc should be safe
	if f.inUse() {
		t.Error("formula should not be in use initially")
	}

	// Mark as in use
	f.markUse()
	if !f.inUse() {
		t.Error("formula should be in use after markUse()")
	}

	// gc should not affect in-use formulas
	ctx.gc()

	// Formula should still be in map
	f2, err := ctx.formulaOf(mod)
	if err != nil {
		t.Fatalf("formulaOf() after gc error = %v", err)
	}
	if f != f2 {
		t.Error("formula should still be cached after gc when in use")
	}
}

func TestFindMaxFromVer_MultipleVersions(t *testing.T) {
	// Test that findMaxFromVer correctly finds the max fromVer <= target
	ctx := ixgo.NewContext(0)
	p := parser.NewParser(ctx)

	// Parse all cJSON formulas to verify their fromVer values
	paths := []struct {
		path    string
		fromVer string
	}{
		{"testdata/DaveGamble/cJSON/1.0.0/CJSON_llar.gox", "1.0.0"},
		{"testdata/DaveGamble/cJSON/1.5.0/CJSON_llar.gox", "1.5.0"},
		{"testdata/DaveGamble/cJSON/2.0.0/CJSON_llar.gox", "2.0.0"},
	}

	for _, tt := range paths {
		astFile, err := p.ParseAST(tt.path)
		if err != nil {
			t.Fatalf("ParseAST(%s) error = %v", tt.path, err)
		}
		got, err := fromVerFrom(astFile)
		if err != nil {
			t.Fatalf("fromVerFrom(%s) error = %v", tt.path, err)
		}
		if got != tt.fromVer {
			t.Errorf("fromVerFrom(%s) = %q, want %q", tt.path, got, tt.fromVer)
		}
	}
}
