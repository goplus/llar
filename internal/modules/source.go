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
// Any sync error is stored and returned by subsequent calls to comparator() or at().
func (s *moduleSource) module(modPath string) *formulaModule {
	if m, ok := s.modules[modPath]; ok {
		return m
	}

	m := newFormulaModule(s.fsys, modPath)

	if s.syncFn != nil {
		m.err = s.syncFn(modPath)
	}

	s.modules[modPath] = m
	return m
}

// formulaModule represents a single module's formula collection.
// It provides access to the module's version comparator and formulas.
type formulaModule struct {
	fsys     fs.FS
	modPath  string
	err      error // stores sync error, returned by comparator() or at()
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
	if m.err != nil {
		return nil, m.err
	}

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
	if m.err != nil {
		return nil, m.err
	}

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
	var argResult string
	switch arg := c.Args[0].(type) {
	case *ast.BasicLit:
		argResult = strings.Trim(strings.Trim(arg.Value, `"`), "`")
		if argResult == "" {
			return "", fmt.Errorf("failed to parse %s from AST: empty argument", fnName)
		}
	default:
		return "", fmt.Errorf("failed to parse %s from AST: argument is not a string literal", fnName)
	}
	return argResult, nil
}
