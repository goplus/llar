package modules

import (
	"fmt"
	"go/token"
	"io/fs"
	"strings"

	"github.com/goplus/ixgo/xgobuild"
	"github.com/goplus/llar/internal/formula"
	"github.com/goplus/llar/mod/module"
	"github.com/goplus/llar/x/gnu"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
)

const defaultFormulaSuffix = "_llar.gox"

// moduleSource manages formula repositories and provides access to individual modules.
// It handles synchronization of remote formulas and caches formulaModule instances.
type moduleSource struct {
	fsys    fs.FS
	syncFn  func(modPath string) error
	modules map[string]*formulaModule
}

// newModuleSource creates a new moduleSource with the given filesystem and sync function.
// The syncFn is called to synchronize formulas from remote before accessing a module.
func newModuleSource(fsys fs.FS, syncFn func(modPath string) error) *moduleSource {
	return &moduleSource{
		fsys:    fsys,
		syncFn:  syncFn,
		modules: make(map[string]*formulaModule),
	}
}

// module returns the formulaModule for the given module path.
// It synchronizes the module from remote if needed and caches the result.
func (s *moduleSource) module(modPath string) (*formulaModule, error) {
	if m, ok := s.modules[modPath]; ok {
		return m, nil
	}

	if s.syncFn != nil {
		if err := s.syncFn(modPath); err != nil {
			return nil, err
		}
	}

	m := newFormulaModule(s.fsys, modPath)
	s.modules[modPath] = m
	return m, nil
}

// formulaModule represents a single module's formula collection.
// It provides access to the module's version comparator and formulas.
type formulaModule struct {
	fsys     fs.FS
	modPath  string
	cmp      func(v1, v2 module.Version) int
	formulas map[string]*formula.Formula
}

// newFormulaModule creates a new formulaModule for the given module path.
func newFormulaModule(fsys fs.FS, modPath string) *formulaModule {
	return &formulaModule{
		fsys:     fsys,
		modPath:  modPath,
		formulas: make(map[string]*formula.Formula),
	}
}

// comparator returns the version comparator for this module.
// It loads the comparator lazily and caches the result.
func (m *formulaModule) comparator() (func(v1, v2 module.Version) int, error) {
	if m.cmp != nil {
		return m.cmp, nil
	}

	modDir, err := module.EscapePath(m.modPath)
	if err != nil {
		return nil, err
	}

	cmp, err := loadComparatorFS(m.fsys.(fs.ReadFileFS), modDir)
	if err != nil {
		cmp = func(v1, v2 module.Version) int {
			return gnu.Compare(v1.Version, v2.Version)
		}
	}

	m.cmp = cmp
	return m.cmp, nil
}

// at returns the formula for the specified version.
// It finds the appropriate formula based on version matching and caches the result.
func (m *formulaModule) at(version string) (*formula.Formula, error) {
	cmp, err := m.comparator()
	if err != nil {
		return nil, err
	}

	mod := module.Version{Path: m.modPath, Version: version}
	fromVer, formulaPath, err := m.findMaxFromVer(mod, cmp)
	if err != nil {
		return nil, err
	}

	if f, ok := m.formulas[fromVer]; ok {
		return f, nil
	}

	f, err := formula.LoadFS(m.fsys.(fs.ReadFileFS), formulaPath)
	if err != nil {
		return nil, err
	}

	m.formulas[fromVer] = f
	return f, nil
}

// findMaxFromVer finds the formula file with the highest fromVer that is <= the target version.
func (m *formulaModule) findMaxFromVer(mod module.Version, compare func(v1, v2 module.Version) int) (maxFromVer, formulaPath string, err error) {
	modDir, err := module.EscapePath(mod.Path)
	if err != nil {
		return "", "", err
	}

	err = fs.WalkDir(m.fsys, modDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(path, defaultFormulaSuffix) {
			return nil
		}

		fromVer, err := fromVerOf(m.fsys.(fs.ReadFileFS), path)
		if err != nil {
			return err
		}
		fromVerMod := module.Version{Path: mod.Path, Version: fromVer}

		if compare(fromVerMod, mod) > 0 {
			return nil
		}
		if maxFromVer == "" || compare(fromVerMod, module.Version{Path: mod.Path, Version: maxFromVer}) > 0 {
			maxFromVer = fromVer
			formulaPath = path
		}
		return nil
	})

	if err != nil {
		return "", "", err
	}

	if formulaPath == "" {
		return "", "", fmt.Errorf("no formula found for %s", mod.Path)
	}

	return maxFromVer, formulaPath, nil
}

// fromVerOf extracts the fromVer value from a formula file by parsing its AST.
func fromVerOf(fsys fs.ReadFileFS, formulaPath string) (string, error) {
	content, err := fsys.ReadFile(formulaPath)
	if err != nil {
		return "", err
	}

	fset := token.NewFileSet()
	astFile, err := parser.ParseEntry(fset, formulaPath, content, parser.Config{
		ClassKind: xgobuild.ClassKind,
	})
	if err != nil {
		return "", err
	}
	return fromVerFrom(astFile)
}

// fromVerFrom extracts the fromVer value from a formula AST.
func fromVerFrom(formulaAST *ast.File) (string, error) {
	var fromVer string
	var err error

	ast.Inspect(formulaAST, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if fn, ok := c.Fun.(*ast.Ident); ok && fn.Name == "fromVer" {
			fromVer, err = parseCallArg(c, fn.Name)
			return false
		}
		return true
	})

	return fromVer, err
}

// parseCallArg extracts the first string argument from a function call expression.
func parseCallArg(c *ast.CallExpr, fnName string) (string, error) {
	if len(c.Args) == 0 {
		return "", fmt.Errorf("failed to parse %s from AST: no argument", fnName)
	}

	if arg, ok := c.Args[0].(*ast.BasicLit); ok {
		result := strings.Trim(strings.Trim(arg.Value, `"`), "`")
		if result == "" {
			return "", fmt.Errorf("failed to parse %s from AST: empty argument", fnName)
		}
		return result, nil
	}

	return "", nil
}
