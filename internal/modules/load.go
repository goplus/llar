package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/goplus/llar/internal/formula"
	"github.com/goplus/llar/internal/formula/repo"
	"github.com/goplus/llar/internal/mvs"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/mod/module"
	"github.com/goplus/llar/mod/versions"

	classfile "github.com/goplus/llar/formula"
)

// Module represents a loaded module with its formula, filesystem, and resolved dependencies.
// The embedded Formula contains build instructions, while Deps contains pointers to
// all transitive dependencies in the build list.
type Module struct {
	*formula.Formula

	FS      fs.FS
	Path    string
	Version string

	Deps []*Module
}

// Options contains options for Load.
type Options struct {
	// Tidy, if true, computes minimal dependencies using mvs.Req
	// and updates the versions.json file.
	Tidy bool
	// FormulaStore is the store for downloading and caching formulas.
	FormulaStore *repo.Store
}

func latestVersion(modPath string, repo vcs.Repo, comparator func(v1, v2 module.Version) int) (version string, err error) {
	tags, err := repo.Tags(context.TODO())
	if err != nil {
		return "", err
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("failed to retrieve the latest version: no tags found")
	}
	max := slices.MaxFunc(tags, func(a, b string) int {
		return comparator(module.Version{modPath, a}, module.Version{modPath, b})
	})
	return max, nil
}

// Load loads all packages required by the main module and resolves
// their dependencies using the MVS algorithm. It returns modules for all
// packages in the computed build list.
func Load(ctx context.Context, main module.Version, opts Options) ([]*Module, error) {
	var moduleCache sync.Map

	moduleOf := func(modPath string) (*formulaModule, error) {
		if fm, ok := moduleCache.Load(modPath); ok {
			return fm.(*formulaModule), nil
		}
		// ModuleFS always fetches the formula from the latest commit.
		fs, err := opts.FormulaStore.ModuleFS(ctx, modPath)
		if err != nil {
			return nil, err
		}
		fm := newFormulaModule(fs, modPath)
		actual, _ := moduleCache.LoadOrStore(modPath, fm)
		return actual.(*formulaModule), nil
	}

	mainMod, err := moduleOf(main.Path)
	if err != nil {
		return nil, err
	}
	if main.Version == "" {
		cmp, err := mainMod.comparator()
		if err != nil {
			return nil, err
		}
		// TODO(MeteorsLiu): Support different code host sites
		latestRepo, err := vcs.NewRepo(fmt.Sprintf("github.com/%s", main.Path))
		if err != nil {
			return nil, err
		}
		latest, err := latestVersion(main.Path, latestRepo, cmp)
		if err != nil {
			return nil, err
		}
		main.Version = latest
	}
	mainFormula, err := mainMod.at(main.Version)
	if err != nil {
		return nil, err
	}
	mainDeps, err := resolveDeps(main, mainMod.fsys.(fs.ReadFileFS), mainFormula)
	if err != nil {
		return nil, err
	}
	cmp := func(p, v1, v2 string) int {
		// none is an internal version for MVS, which means the smallest
		if v1 == "none" && v2 != "none" {
			return -1
		} else if v1 != "none" && v2 == "none" {
			return +1
		} else if v1 == "none" && v2 == "none" {
			return 0
		}
		mod, err := moduleOf(p)
		if err != nil {
			// should not have errors
			panic(err)
		}
		compare, err := mod.comparator()
		if err != nil {
			// should not have errors
			panic(err)
		}
		return compare(module.Version{p, v1}, module.Version{p, v2})
	}
	onLoad := func(mod module.Version) ([]module.Version, error) {
		thisMod, err := moduleOf(mod.Path)
		if err != nil {
			return nil, err
		}
		f, err := thisMod.at(mod.Version)
		if err != nil {
			return nil, err
		}
		return resolveDeps(mod, thisMod.fsys.(fs.ReadFileFS), f)
	}

	reqs := &mvsReqs{
		roots: mainDeps,
		isMain: func(v module.Version) bool {
			return v.Path == main.Path && v.Version == main.Version
		},
		cmp:    cmp,
		onLoad: onLoad,
	}

	buildList, err := mvs.BuildList([]module.Version{main}, reqs)
	if err != nil {
		return nil, err
	}

	// Tidy: compute minimal dependencies and update versions.json
	if opts.Tidy {
		if err := tidy(main, mainMod.fsys.(fs.ReadFileFS), reqs); err != nil {
			return nil, err
		}
	}

	convertToModules := func(modList []module.Version) ([]*Module, error) {
		var modules []*Module

		for _, mod := range modList {
			thisMod, err := moduleOf(mod.Path)
			if err != nil {
				return nil, err
			}
			f, err := thisMod.at(mod.Version)
			if err != nil {
				return nil, err
			}
			module := &Module{
				Formula: f,
				FS:      thisMod.fsys,
				Path:    mod.Path,
				Version: mod.Version,
			}
			modules = append(modules, module)
		}

		return modules, nil
	}

	modules, err := convertToModules(buildList)
	if err != nil {
		return nil, err
	}

	// fill the deps
	for _, mod := range modules {
		var deps []*Module

		if mod.Path == main.Path && mod.Version == main.Version {
			deps = modules[1:]
		} else {
			reqs, err := mvs.Req(module.Version{mod.Path, mod.Version}, []string{}, reqs)
			if err != nil {
				return nil, err
			}
			deps, err = convertToModules(reqs)
			if err != nil {
				return nil, err
			}
		}
		mod.Deps = deps
	}

	return modules, nil
}

// tidy computes minimal dependencies using mvs.Req and updates versions.json.
func tidy(main module.Version, moduleFS fs.ReadFileFS, reqs *mvsReqs) error {
	minDeps, err := mvs.Req(main, []string{}, reqs)
	if err != nil {
		return err
	}
	content, err := moduleFS.ReadFile("versions.json")
	if err != nil {
		return err
	}
	v, err := versions.Parse("", content)
	if err != nil {
		return err
	}

	var newDeps []module.Version
	for _, dep := range minDeps {
		if dep.Path == main.Path {
			continue
		}
		newDeps = append(newDeps, module.Version{
			Path:    dep.Path,
			Version: dep.Version,
		})
	}

	v.Dependencies[main.Version] = newDeps

	data, err := json.MarshalIndent(v, "", "\t")
	if err != nil {
		return err
	}

	f, err := moduleFS.Open("versions.json")
	if err != nil {
		return err
	}
	defer f.Close()

	fileStat, err := f.Stat()
	if err != nil {
		return err
	}
	// NOTE(MeteorsLiu): fs.Fs dones't support `Write` method, so this is a very hack trick.
	// TODO(MeteorsLiu): consider remove this?
	return os.WriteFile(fileStat.Name(), data, 0644)
}

// resolveDeps resolves the dependencies for a formula.
// It first tries to get dependencies from the OnRequire callback,
// then falls back to parsing versions.json if no dependencies are found.
func resolveDeps(mod module.Version, modFS fs.ReadFileFS, frla *formula.Formula) ([]module.Version, error) {
	var deps classfile.ModuleDeps

	// TODO(MeteorsLiu): Support different code host sites.
	repo, err := vcs.NewRepo(fmt.Sprintf("github.com/%s", mod.Path))
	if err != nil {
		return nil, err
	}
	// onRequire is optional
	if frla.OnRequire != nil {
		// TODO(MeteorsLiu): Design source cache dir
		// In the most common case, onRequire only read one file like CMakelist.txt, etc.
		// So missing cache here is acceptable.
		tmpSourceDir, err := os.MkdirTemp("", fmt.Sprintf("source-%s-%s*", strings.ReplaceAll(mod.Path, "/", "-"), mod.Version))
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmpSourceDir)

		repoFS := repo.At(mod.Version, tmpSourceDir)
		proj := &classfile.Project{
			SourceFS: repoFS.(fs.ReadFileFS),
		}
		frla.OnRequire(proj, &deps)
	}

	content, err := modFS.ReadFile("versions.json")
	if err != nil {
		return nil, err
	}
	depTable, err := versions.Parse("", content)
	if err != nil {
		return nil, err
	}
	current := depTable.Dependencies[mod.Version]

	var vers []module.Version

	for _, dep := range deps.Deps() {
		if dep.Version == "" {
			// if a version of a dep input by onRequire is empty, try our best to resolve it.
			idx := slices.IndexFunc(current, func(depInTable module.Version) bool {
				return depInTable.Path == dep.Path
			})
			if idx < 0 {
				// It seems safe to drop deps here, because we resolve deps recursively and finally we will find that dep.
				continue
			}
			dep.Version = current[idx].Version
		}

		vers = append(vers, module.Version{
			Path:    dep.Path,
			Version: dep.Version,
		})
	}

	if len(vers) > 0 {
		return vers, nil
	}

	for _, dep := range current {
		if dep.Version != "" {
			vers = append(vers, module.Version{
				Path:    dep.Path,
				Version: dep.Version,
			})
		}
	}

	return vers, nil
}
