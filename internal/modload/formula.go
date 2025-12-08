package modload

import (
	"fmt"
	"go/ast"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/goplus/ixgo"
	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/env"
	"github.com/goplus/llar/internal/loader"
	"github.com/goplus/llar/internal/parser"
	"github.com/goplus/llar/pkgs/gnu"
	"github.com/goplus/llar/pkgs/mod/module"
)

const _defaultFormulaSuffix = "_llar.gox"

type Formula struct {
	module.Version

	Dir       string
	OnRequire func(proj *formula.Project, deps *formula.ModuleDeps)
	OnBuild   func(proj *formula.Project, out *formula.BuildResult)
}

func parseLibraryName(mod module.Version) string {
	_, name, ok := strings.Cut(mod.ID, "/")
	if !ok {
		panic("invalid module id")
	}
	return name
}

func loadComparator(loader loader.Loader, mod module.Version) (comparator module.VersionComparator, err error) {
	formulaDir, err := env.FormulaDir()
	if err != nil {
		return nil, err
	}
	moduleDir := filepath.Join(formulaDir, mod.ID)

	cmpFormulaPath := filepath.Join(moduleDir, fmt.Sprintf("%s_cmp.gox", parseLibraryName(mod)))

	if _, err := os.Stat(cmpFormulaPath); os.IsNotExist(err) {
		cmpFormulas, _ := filepath.Glob(filepath.Join(moduleDir, "*_cmp.gox"))

		if len(cmpFormulas) == 0 {
			cmpFormulaPath = ""
		} else {
			cmpFormulaPath = cmpFormulas[0]
		}
	}

	if cmpFormulaPath == "" {
		return gnu.Compare, nil
	}

	cmpStruct, err := loader.Load(cmpFormulaPath)
	if err != nil {
		return nil, err
	}
	return cmpStruct.Value("fCompareVer").(module.VersionComparator), nil
}

func findMaxFromVer(mod module.Version, compare module.VersionComparator) (maxFromVer, formulaPath string, err error) {
	formulaDir, err := env.FormulaDir()
	if err != nil {
		return "", "", err
	}
	moduleDir := filepath.Join(formulaDir, mod.ID)

	ctx := ixgo.NewContext(0)

	parser := parser.NewParser(ctx)

	err = filepath.WalkDir(moduleDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(path, _defaultFormulaSuffix) {
			// skip non-suffix
			return nil
		}
		formulaAST, err := parser.ParseAST(path)
		if err != nil {
			return err
		}
		fromVer, err := fromVerFrom(formulaAST)
		if err != nil {
			return err
		}
		if maxFromVer == "" {
			maxFromVer = fromVer
			formulaPath = path
			return nil
		}
		// a > b
		if compare(fromVer, maxFromVer) > 0 {
			maxFromVer = fromVer
			formulaPath = path
		}
		return nil
	})

	if err != nil {
		return "", "", err
	}

	if formulaPath == "" {
		return "", "", fmt.Errorf("failed to load formula: no formula found")
	}

	return maxFromVer, formulaPath, nil
}

func fromVerFrom(formulaAST *ast.File) (fromVer string, err error) {
	ast.Inspect(formulaAST, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := c.Fun.(type) {
		case *ast.SelectorExpr:
			switch fn.Sel.Name {
			case "FromVer":
				fromVer, err = parseCallArg(c, fn.Sel.Name)

				return false
			}
		}
		return true
	})
	return
}

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
