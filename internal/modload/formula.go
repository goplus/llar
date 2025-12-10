package modload

import (
	"context"
	"fmt"
	"go/ast"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/goplus/ixgo"
	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/env"
	"github.com/goplus/llar/internal/loader"
	"github.com/goplus/llar/internal/parser"
	repo "github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/pkgs/gnu"
	"github.com/goplus/llar/pkgs/mod/module"
)

const _defaultFormulaSuffix = "_llar.gox"

type Formula struct {
	module.Version

	vcs           repo.VCS
	refcnt        int
	remoteRepoUrl string

	Dir       string
	Proj      *formula.Project
	OnRequire func(proj *formula.Project, deps *formula.ModuleDeps)
	OnBuild   func(proj *formula.Project, out *formula.BuildResult)
}

func (f *Formula) markUse() {
	f.refcnt++
}

func (f *Formula) inUse() bool {
	return f.refcnt > 0
}

func (f *Formula) ref(ctx context.Context) (string, error) {
	refs, err := f.vcs.Tags(ctx, f.remoteRepoUrl)
	if err != nil {
		return "", err
	}
	ref, ok := matchGitRef(refs, f.Version.Version)
	if !ok {
		return "", fmt.Errorf("failed to resolve version: cannot find a ref from version: %s", f.Version.Version)
	}
	return ref, nil
}

func (f *Formula) Sync(ctx context.Context, dir string) error {
	ref, err := f.ref(ctx)
	if err != nil {
		return err
	}
	return f.vcs.Sync(ctx, f.remoteRepoUrl, ref, dir)
}

type formulaContext struct {
	ctx         *ixgo.Context
	loader      loader.Loader
	formulas    map[module.Version]*Formula
	comparators map[string]module.VersionComparator
}

func newFormulaContext() *formulaContext {
	ctx := ixgo.NewContext(ixgo.SupportMultipleInterp)
	return &formulaContext{
		ctx:         ctx,
		loader:      loader.NewFormulaLoader(ctx),
		formulas:    make(map[module.Version]*Formula),
		comparators: make(map[string]module.VersionComparator),
	}
}

func (m *formulaContext) comparatorOf(modId string) (module.VersionComparator, error) {
	if comp, ok := m.comparators[modId]; ok {
		return comp, nil
	}
	comp, err := loadComparator(m.loader, modId)
	if err != nil {
		return nil, err
	}
	m.comparators[modId] = comp
	return comp, nil
}

func (m *formulaContext) formulaOf(mod module.Version) (*Formula, error) {
	comparator, err := m.comparatorOf(mod.ID)
	if err != nil {
		return nil, err
	}
	maxFromVer, formulaPath, err := findMaxFromVer(mod, comparator)
	if err != nil {
		return nil, err
	}
	cacheKey := module.Version{ID: mod.ID, Version: maxFromVer}
	f, ok := m.formulas[cacheKey]
	if ok {
		return f, nil
	}
	formulaStruct, err := m.loader.Load(formulaPath)
	if err != nil {
		return nil, err
	}

	// TODO(MeteorsLiu): Support different VCS
	vcs := repo.NewGitVCS()
	remoteRepoUrl := fmt.Sprintf("https://github.com/%s", mod.ID)

	formulaDir, err := env.FormulaDir()
	if err != nil {
		return nil, err
	}
	f = &Formula{
		vcs:           vcs,
		remoteRepoUrl: remoteRepoUrl,
		Version:       mod,
		Dir:           filepath.Join(formulaDir, mod.ID),
		OnBuild:       formulaStruct.Value("fOnBuild").(func(*formula.Project, *formula.BuildResult)),
		OnRequire:     formulaStruct.Value("fOnRequire").(func(*formula.Project, *formula.ModuleDeps)),
	}
	m.formulas[cacheKey] = f
	return f, nil
}

func (m *formulaContext) gc() {
	for _, f := range m.formulas {
		if !f.inUse() && f.Proj != nil {
			tmpDir := reflect.ValueOf(f.Proj.DirFS).String()

			os.RemoveAll(tmpDir)
		}
	}
}

func parseLibraryName(modID string) string {
	_, name, ok := strings.Cut(modID, "/")
	if !ok {
		panic("invalid module id")
	}
	return name
}

func loadComparator(loader loader.Loader, modID string) (comparator module.VersionComparator, err error) {
	formulaDir, err := env.FormulaDir()
	if err != nil {
		return nil, err
	}
	moduleDir := filepath.Join(formulaDir, modID)

	var cmpFormulaPath string
	cmpFormulas, _ := filepath.Glob(filepath.Join(moduleDir, "*_cmp.gox"))

	if len(cmpFormulas) > 0 {
		cmpFormulaPath = cmpFormulas[0]
	}

	if cmpFormulaPath == "" {
		return func(v1, v2 module.Version) int {
			return gnu.Compare(v1.Version, v2.Version)
		}, nil
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
		fromVerMod := module.Version{mod.ID, fromVer}

		// skip if not fromVer <= mod.
		if compare(fromVerMod, mod) > 0 {
			return nil
		}
		// fromVer > maxFromVer
		if maxFromVer == "" || compare(fromVerMod, module.Version{mod.ID, maxFromVer}) > 0 {
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

func matchGitRef(refs []string, version string) (ref string, ok bool) {
	for _, r := range refs {
		if strings.HasSuffix(r, version) {
			return r, true
		}
	}
	return "", false
}
