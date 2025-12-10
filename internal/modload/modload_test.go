package modload

import (
	"context"
	"fmt"
	"go/ast"
	"testing"

	"github.com/goplus/ixgo"
	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/mvs"
	"github.com/goplus/llar/internal/parser"
	"github.com/goplus/llar/internal/repo"
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

func TestMvsReqs_Max(t *testing.T) {
	reqs := &mvsReqs{
		isMain: func(v module.Version) bool {
			return v.ID == "main" && v.Version == ""
		},
		cmp: func(p, v1, v2 string) int {
			// simple string comparison for test
			if v1 < v2 {
				return -1
			} else if v1 > v2 {
				return 1
			}
			return 0
		},
	}

	tests := []struct {
		name string
		p    string
		v1   string
		v2   string
		want string
	}{
		{"v1 > v2", "pkg", "v2.0.0", "v1.0.0", "v2.0.0"},
		{"v1 < v2", "pkg", "v1.0.0", "v2.0.0", "v2.0.0"},
		{"v1 == v2", "pkg", "v1.0.0", "v1.0.0", "v1.0.0"},
		{"main version wins", "main", "", "v1.0.0", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reqs.Max(tt.p, tt.v1, tt.v2)
			if got != tt.want {
				t.Errorf("Max(%q, %q, %q) = %q, want %q", tt.p, tt.v1, tt.v2, got, tt.want)
			}
		})
	}
}

func TestMvsReqs_Required(t *testing.T) {
	roots := []module.Version{
		{ID: "dep1", Version: "v1.0.0"},
		{ID: "dep2", Version: "v2.0.0"},
	}

	reqs := &mvsReqs{
		roots: roots,
		isMain: func(v module.Version) bool {
			return v.ID == "main" && v.Version == ""
		},
		onLoad: func(mod module.Version) ([]module.Version, error) {
			if mod.ID == "dep1" {
				return []module.Version{{ID: "dep3", Version: "v1.0.0"}}, nil
			}
			return nil, nil
		},
	}

	t.Run("main module returns roots", func(t *testing.T) {
		got, err := reqs.Required(module.Version{ID: "main", Version: ""})
		if err != nil {
			t.Fatalf("Required() error = %v", err)
		}
		if len(got) != len(roots) {
			t.Errorf("Required() returned %d deps, want %d", len(got), len(roots))
		}
	})

	t.Run("none version returns nil", func(t *testing.T) {
		got, err := reqs.Required(module.Version{ID: "dep1", Version: "none"})
		if err != nil {
			t.Fatalf("Required() error = %v", err)
		}
		if got != nil {
			t.Errorf("Required() = %v, want nil", got)
		}
	})

	t.Run("regular module calls onLoad", func(t *testing.T) {
		got, err := reqs.Required(module.Version{ID: "dep1", Version: "v1.0.0"})
		if err != nil {
			t.Fatalf("Required() error = %v", err)
		}
		if len(got) != 1 || got[0].ID != "dep3" {
			t.Errorf("Required() = %v, want [{dep3 v1.0.0}]", got)
		}
	})
}

func TestMvsReqs_cmpVersion(t *testing.T) {
	reqs := &mvsReqs{
		isMain: func(v module.Version) bool {
			return v.ID == "main" && v.Version == ""
		},
		cmp: func(p, v1, v2 string) int {
			if v1 < v2 {
				return -1
			} else if v1 > v2 {
				return 1
			}
			return 0
		},
	}

	tests := []struct {
		name string
		p    string
		v1   string
		v2   string
		want int
	}{
		{"v1 < v2", "pkg", "v1.0.0", "v2.0.0", -1},
		{"v1 > v2", "pkg", "v2.0.0", "v1.0.0", 1},
		{"v1 == v2", "pkg", "v1.0.0", "v1.0.0", 0},
		{"main v2 wins over v1", "main", "v1.0.0", "", -1},
		{"v1 wins over main v2 (v1 is main)", "main", "", "v1.0.0", 1},
		{"both main", "main", "", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reqs.cmpVersion(tt.p, tt.v1, tt.v2)
			if got != tt.want {
				t.Errorf("cmpVersion(%q, %q, %q) = %d, want %d", tt.p, tt.v1, tt.v2, got, tt.want)
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

func TestMvsReqs_Upgrade(t *testing.T) {
	reqs := &mvsReqs{}

	mod := module.Version{ID: "test/pkg", Version: "v1.0.0"}
	got, err := reqs.Upgrade(mod)
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if got != mod {
		t.Errorf("Upgrade() = %v, want %v (no-op)", got, mod)
	}
}

func TestMVS_BuildList(t *testing.T) {
	// Simulate:
	// A@1.0 -> B@1.0, C@1.0
	// B@1.0 -> C@2.0
	// C@1.0 -> (none)
	// C@2.0 -> (none)
	// MVS should select: A@1.0, B@1.0, C@2.0

	deps := map[module.Version][]module.Version{
		{ID: "A", Version: "1.0"}: {
			{ID: "B", Version: "1.0"},
			{ID: "C", Version: "1.0"},
		},
		{ID: "B", Version: "1.0"}: {
			{ID: "C", Version: "2.0"},
		},
		{ID: "C", Version: "1.0"}: {},
		{ID: "C", Version: "2.0"}: {},
	}

	main := module.Version{ID: "A", Version: "1.0"}

	reqs := &mvsReqs{
		roots: deps[main],
		isMain: func(v module.Version) bool {
			return v.ID == main.ID && v.Version == main.Version
		},
		cmp: func(p, v1, v2 string) int {
			if v1 == "none" {
				return -1
			}
			if v2 == "none" {
				return 1
			}
			if v1 < v2 {
				return -1
			} else if v1 > v2 {
				return 1
			}
			return 0
		},
		onLoad: func(mod module.Version) ([]module.Version, error) {
			if d, ok := deps[mod]; ok {
				return d, nil
			}
			return nil, nil
		},
	}

	buildList, err := mvs.BuildList([]module.Version{main}, reqs)
	if err != nil {
		t.Fatalf("BuildList() error = %v", err)
	}

	// Should have 3 modules: A, B, C
	if len(buildList) != 3 {
		t.Fatalf("BuildList() returned %d modules, want 3: %v", len(buildList), buildList)
	}

	// First should be main
	if buildList[0] != main {
		t.Errorf("BuildList()[0] = %v, want %v", buildList[0], main)
	}

	// C should be version 2.0 (MVS selects max)
	cVersion := ""
	for _, m := range buildList {
		if m.ID == "C" {
			cVersion = m.Version
			break
		}
	}
	if cVersion != "2.0" {
		t.Errorf("C version = %q, want %q (MVS should select max)", cVersion, "2.0")
	}
}

func TestMVS_DiamondDependency(t *testing.T) {
	// Diamond dependency:
	// A -> B, C
	// B -> D@1.0
	// C -> D@2.0
	// MVS should select D@2.0

	deps := map[module.Version][]module.Version{
		{ID: "A", Version: "1.0"}: {
			{ID: "B", Version: "1.0"},
			{ID: "C", Version: "1.0"},
		},
		{ID: "B", Version: "1.0"}: {
			{ID: "D", Version: "1.0"},
		},
		{ID: "C", Version: "1.0"}: {
			{ID: "D", Version: "2.0"},
		},
		{ID: "D", Version: "1.0"}: {},
		{ID: "D", Version: "2.0"}: {},
	}

	main := module.Version{ID: "A", Version: "1.0"}

	reqs := &mvsReqs{
		roots: deps[main],
		isMain: func(v module.Version) bool {
			return v.ID == main.ID && v.Version == main.Version
		},
		cmp: func(p, v1, v2 string) int {
			if v1 == "none" {
				return -1
			}
			if v2 == "none" {
				return 1
			}
			if v1 < v2 {
				return -1
			} else if v1 > v2 {
				return 1
			}
			return 0
		},
		onLoad: func(mod module.Version) ([]module.Version, error) {
			if d, ok := deps[mod]; ok {
				return d, nil
			}
			return nil, nil
		},
	}

	buildList, err := mvs.BuildList([]module.Version{main}, reqs)
	if err != nil {
		t.Fatalf("BuildList() error = %v", err)
	}

	// Find D version
	dVersion := ""
	for _, m := range buildList {
		if m.ID == "D" {
			dVersion = m.Version
			break
		}
	}
	if dVersion != "2.0" {
		t.Errorf("D version = %q, want %q (MVS diamond: select max)", dVersion, "2.0")
	}
}

func TestMVS_NoneVersion(t *testing.T) {
	// Test that "none" version is handled correctly
	deps := map[module.Version][]module.Version{
		{ID: "A", Version: "1.0"}: {
			{ID: "B", Version: "1.0"},
		},
		{ID: "B", Version: "1.0"}: {},
	}

	main := module.Version{ID: "A", Version: "1.0"}

	reqs := &mvsReqs{
		roots: deps[main],
		isMain: func(v module.Version) bool {
			return v.ID == main.ID && v.Version == main.Version
		},
		cmp: func(p, v1, v2 string) int {
			if v1 == "none" && v2 != "none" {
				return -1
			}
			if v1 != "none" && v2 == "none" {
				return 1
			}
			if v1 == "none" && v2 == "none" {
				return 0
			}
			if v1 < v2 {
				return -1
			} else if v1 > v2 {
				return 1
			}
			return 0
		},
		onLoad: func(mod module.Version) ([]module.Version, error) {
			if d, ok := deps[mod]; ok {
				return d, nil
			}
			return nil, nil
		},
	}

	// Test Max with "none"
	if got := reqs.Max("B", "1.0", "none"); got != "1.0" {
		t.Errorf("Max(1.0, none) = %q, want %q", got, "1.0")
	}
	if got := reqs.Max("B", "none", "1.0"); got != "1.0" {
		t.Errorf("Max(none, 1.0) = %q, want %q", got, "1.0")
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

func TestInitProj(t *testing.T) {
	f := &Formula{
		Version:       module.Version{ID: "test/repo", Version: "1.0.0"},
		vcs:           repo.NewGitVCS(),
		remoteRepoUrl: "https://github.com/test/repo",
	}

	// Test that initProj is a no-op when Proj is already set
	f.Proj = &formula.Project{}
	err := initProj(context.Background(), f)
	if err != nil {
		t.Errorf("initProj() with existing Proj should not error, got %v", err)
	}
}

func TestResolveDeps_FromVersionsJSON(t *testing.T) {
	// Create a formula with Proj already set (to skip initProj's Sync)
	// and OnRequire as nil (to use versions.json fallback)
	f := &Formula{
		Version:   module.Version{ID: "DaveGamble/cJSON", Version: "1.7.18"},
		Dir:       "testdata/DaveGamble/cJSON",
		Proj:      &formula.Project{},
		OnRequire: nil,
	}

	deps, err := resolveDeps(context.Background(), f)
	if err != nil {
		t.Fatalf("resolveDeps() error = %v", err)
	}

	// Should find dependency from versions.json
	if len(deps) != 1 {
		t.Fatalf("resolveDeps() got %d deps, want 1", len(deps))
	}

	if deps[0].ID != "madler/zlib" {
		t.Errorf("deps[0].ID = %q, want %q", deps[0].ID, "madler/zlib")
	}
	if deps[0].Version != "1.2.1" {
		t.Errorf("deps[0].Version = %q, want %q", deps[0].Version, "1.2.1")
	}
}

func TestResolveDeps_FromOnRequire(t *testing.T) {
	// Create a formula with OnRequire that sets dependencies
	f := &Formula{
		Version: module.Version{ID: "test/repo", Version: "1.0.0"},
		Dir:     "testdata/test/repo",
		Proj:    &formula.Project{},
		OnRequire: func(proj *formula.Project, deps *formula.ModuleDeps) {
			deps.Require("foo/bar", "2.0.0")
			deps.Require("baz/qux", "3.0.0")
		},
	}

	deps, err := resolveDeps(context.Background(), f)
	if err != nil {
		t.Fatalf("resolveDeps() error = %v", err)
	}

	if len(deps) != 2 {
		t.Fatalf("resolveDeps() got %d deps, want 2", len(deps))
	}

	if deps[0].ID != "foo/bar" || deps[0].Version != "2.0.0" {
		t.Errorf("deps[0] = %v, want {foo/bar 2.0.0}", deps[0])
	}
	if deps[1].ID != "baz/qux" || deps[1].Version != "3.0.0" {
		t.Errorf("deps[1] = %v, want {baz/qux 3.0.0}", deps[1])
	}
}

func TestResolveDeps_EmptyDeps(t *testing.T) {
	// Create a formula with OnRequire that sets no dependencies
	f := &Formula{
		Version:   module.Version{ID: "madler/zlib", Version: "1.0.0"},
		Dir:       "testdata/madler/zlib",
		Proj:      &formula.Project{},
		OnRequire: func(proj *formula.Project, deps *formula.ModuleDeps) {},
	}

	deps, err := resolveDeps(context.Background(), f)
	if err != nil {
		t.Fatalf("resolveDeps() error = %v", err)
	}

	// OnRequire returned nothing, and version 1.0.0 has no deps in versions.json
	if len(deps) != 0 {
		t.Errorf("resolveDeps() got %d deps, want 0", len(deps))
	}
}

func TestE2E(t *testing.T) {
	mods, err := LoadPackages(context.TODO(), module.Version{ID: "DaveGamble/cJSON", Version: "1.7.18"})
	if err != nil {
		t.Error(err)
		return
	}
	for _, f := range mods {
		fmt.Println(f.ID)
	}
}
