package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/goplus/llar/internal/formula"
	"github.com/goplus/llar/internal/mvs"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/pkgs/mod/module"
	"github.com/goplus/llar/pkgs/mod/versions"
)

type Module struct {
	*formula.Formula

	ID      string
	Dir     string
	Version string

	Deps []*Module
}

// Options contains options for LoadPackages.
type Options struct {
	// Tidy, if true, computes minimal dependencies using mvs.Req
	// and updates the versions.json file.
	Tidy bool
	// LocalDir specifies the local directory to fallback when formula
	// is not found in FormulaDir. If empty, defaults to current directory.
	LocalDir string
	// FormulaRepo is the vcs.Repo for downloading formulas.
	FormulaRepo vcs.Repo
}

func latestVersion(modID string, comparator func(v1, v2 module.Version) int) (version string, err error) {
	// TODO(MeteorsLiu): Support different code host sites
	repo, err := vcs.NewRepo(fmt.Sprintf("https://github.com/%s", modID))
	if err != nil {
		return "", err
	}

	tags, err := repo.Tags(context.TODO())
	if err != nil {
		return "", err
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("failed to retrieve the latest version: no tags found")
	}
	slices.SortFunc(tags, func(a, b string) int {
		// we want the max heap
		return -comparator(module.Version{modID, a}, module.Version{modID, b})
	})

	return tags[0], nil
}

// LoadPackages loads all packages required by the main module and resolves
// their dependencies using the MVS algorithm. It returns formulas for all
// modules in the computed build list.
func Load(ctx context.Context, main module.Version, opts Options) ([]*Module, error) {
	cache := newClassfileCache(opts.FormulaRepo, opts.LocalDir)

	if main.Version == "" {
		cmp, err := cache.comparatorOf(main.ID)
		if err != nil {
			return nil, err
		}
		latest, err := latestVersion(main.ID, cmp)
		if err != nil {
			return nil, err
		}
		main.Version = latest
	}
	mainFormula, err := cache.formulaOf(main)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	mainDeps, err := resolveDeps(ctx, main, mainFormula)
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
		compare, err := cache.comparatorOf(p)
		if err != nil {
			panic(err)
		}
		return compare(module.Version{p, v1}, module.Version{p, v2})
	}
	onLoad := func(mod module.Version) ([]module.Version, error) {
		f, err := cache.formulaOf(mod)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()

		return resolveDeps(ctx, mod, f)
	}

	reqs := &mvsReqs{
		roots: mainDeps,
		isMain: func(v module.Version) bool {
			return v.ID == main.ID && v.Version == main.Version
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
		if err := tidy(main, reqs); err != nil {
			return nil, err
		}
	}

	modCache := make(map[module.Version]*Module)

	convertToModules := func(modList []module.Version) ([]*Module, error) {
		var modules []*Module

		for _, mod := range modList {
			if cacheMod, ok := modCache[mod]; ok {
				modules = append(modules, cacheMod)
				continue
			}
			f, err := cache.formulaOf(mod)
			if err != nil {
				return nil, err
			}
			// TODO(MeteorsLiu): Support custom module dir
			moduleDir, err := moduleDirOf(mod.ID)
			if err != nil {
				return nil, err
			}
			module := &Module{
				Formula: f,
				ID:      mod.ID,
				Dir:     moduleDir,
				Version: mod.Version,
			}
			modCache[mod] = module
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

		if mod.ID == main.ID && mod.Version == main.Version {
			deps = modules[1:]
		} else {
			reqs, err := mvs.Req(module.Version{mod.ID, mod.Version}, []string{}, reqs)
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
func tidy(main module.Version, reqs *mvsReqs) error {
	minDeps, err := mvs.Req(main, []string{}, reqs)
	if err != nil {
		return err
	}
	moduleDir, err := moduleDirOf(main.ID)
	if err != nil {
		return err
	}
	versionsFile := filepath.Join(moduleDir, "versions.json")
	v, err := versions.Parse(versionsFile, nil)
	if err != nil {
		return err
	}

	var newDeps []versions.Dependency
	for _, dep := range minDeps {
		if dep.ID == main.ID {
			continue
		}
		newDeps = append(newDeps, versions.Dependency{
			ModuleID: dep.ID,
			Version:  dep.Version,
		})
	}

	v.Dependencies[main.Version] = newDeps

	data, err := json.MarshalIndent(v, "", "\t")
	if err != nil {
		return err
	}

	return os.WriteFile(versionsFile, data, 0644)
}
