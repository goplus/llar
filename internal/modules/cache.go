package modules

import (
	"fmt"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/goplus/ixgo/xgobuild"
	"github.com/goplus/llar/internal/formula"
	"github.com/goplus/llar/mod/module"
	"github.com/goplus/llar/x/gnu"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
)

const _defaultFormulaSuffix = "_llar.gox"

// classfileCache manages formula loading and caching.
// It maintains a cache of loaded formulas and version comparators.
type classfileCache struct {
	formulas          map[module.Version]*formula.Formula
	comparators       map[string]func(v1, v2 module.Version) int
	searchPaths       []string // formula search paths (first match wins)
	loadRemoteFormula func(modPath string) error
}

func newClassfileCache(localDir string, loadRemoteFormula func(modPath string) error) *classfileCache {
	if localDir == "" {
		localDir = "."
	}

	return &classfileCache{
		loadRemoteFormula: loadRemoteFormula,
		searchPaths:       []string{localDir},
		formulas:          make(map[module.Version]*formula.Formula),
		comparators:       make(map[string]func(v1, v2 module.Version) int),
	}
}

// comparatorOf returns a version comparator for the specified module.
// It caches comparators to avoid reloading them.
func (m *classfileCache) comparatorOf(modPath string) (func(v1, v2 module.Version) int, error) {
	if comp, ok := m.comparators[modPath]; ok {
		return comp, nil
	}
	if m.loadRemoteFormula != nil {
		if err := m.loadRemoteFormula(modPath); err != nil {
			return nil, err
		}
	}
	moduleDir, err := moduleDirOf(modPath)
	if err != nil {
		return nil, err
	}
	seachPaths := append([]string{moduleDir}, m.searchPaths...)

	var comp func(v1 module.Version, v2 module.Version) int

	for _, searchPath := range seachPaths {
		comp, err = loadComparator(searchPath)
		if err == nil {
			break
		}
	}
	if comp == nil {
		comp = func(v1, v2 module.Version) int {
			return gnu.Compare(v1.Version, v2.Version)
		}
	}
	m.comparators[modPath] = comp
	return comp, nil
}

// formulaOf returns the formula for the specified module version.
// It finds the appropriate formula file based on version and caches the result.
func (m *classfileCache) formulaOf(mod module.Version) (*formula.Formula, error) {
	comparator, err := m.comparatorOf(mod.Path)
	if err != nil {
		return nil, err
	}
	maxFromVer, formulaPath, err := m.findMaxFromVer(mod, comparator)
	if err != nil {
		return nil, err
	}
	cacheKey := module.Version{Path: mod.Path, Version: maxFromVer}
	f, ok := m.formulas[cacheKey]
	if ok {
		return f, nil
	}
	f, err = formula.Load(formulaPath)
	if err != nil {
		return nil, err
	}
	m.formulas[cacheKey] = f
	return f, nil
}

// findMaxFromVer finds the formula file with the highest FromVer that is <= the target version.
// It searches through all searchPaths in order, returning the first match.
func (m *classfileCache) findMaxFromVer(mod module.Version, compare func(v1, v2 module.Version) int) (maxFromVer, formulaPath string, err error) {
	moduleDir, err := moduleDirOf(mod.Path)
	if err != nil {
		return "", "", err
	}
	seachPaths := append([]string{moduleDir}, m.searchPaths...)

	for _, seachPath := range seachPaths {
		// Skip if directory doesn't exist
		if _, statErr := os.Stat(seachPath); os.IsNotExist(statErr) {
			continue
		}

		err = filepath.WalkDir(seachPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !strings.HasSuffix(path, _defaultFormulaSuffix) {
				return nil
			}
			fromVer, err := fromVerOf(path)
			if err != nil {
				return err
			}
			fromVerMod := module.Version{mod.Path, fromVer}

			if compare(fromVerMod, mod) > 0 {
				return nil
			}
			if maxFromVer == "" || compare(fromVerMod, module.Version{mod.Path, maxFromVer}) > 0 {
				maxFromVer = fromVer
				formulaPath = path
			}
			return nil
		})

		if err != nil {
			return "", "", err
		}

		// Found in this search path, return immediately
		if formulaPath != "" {
			return maxFromVer, formulaPath, nil
		}
	}

	return "", "", fmt.Errorf("failed to load formula: no formula found for %s", mod.Path)
}

// fromVerOf extracts the FromVer value from a LLAR formula file by parsing its AST.
// It searches for the fromVer() call in the formula file and returns its argument.
func fromVerOf(formulaPath string) (fromVer string, err error) {
	fs := token.NewFileSet()
	astFile, err := parser.ParseEntry(fs, formulaPath, nil, parser.Config{
		ClassKind: xgobuild.ClassKind,
	})
	if err != nil {
		return "", err
	}
	return fromVerFrom(astFile)
}

// fromVerFrom extracts the FromVer value from a formula AST by finding the FromVer() call.
func fromVerFrom(formulaAST *ast.File) (fromVer string, err error) {
	ast.Inspect(formulaAST, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := c.Fun.(type) {
		case *ast.Ident:
			switch fn.Name {
			case "fromVer":
				fromVer, err = parseCallArg(c, fn.Name)

				return false
			}
		}
		return true
	})
	return
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
			return "", fmt.Errorf("failed to parse %s from AST: no argument", fnName)
		}
	}
	return argResult, nil
}
