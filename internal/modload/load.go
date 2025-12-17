package modload

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/goplus/llar/internal/mvs"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/pkgs/mod/module"
	"github.com/goplus/llar/pkgs/mod/versions"
)

// PackageOpts contains options for LoadPackages.
type PackageOpts struct {
	// Tidy, if true, computes minimal dependencies using mvs.Req
	// and updates the versions.json file.
	Tidy bool
}

func latestVersion(modID string, comparator module.VersionComparator) (version string, err error) {
	// TODO(MeteorsLiu): Support different VCS
	vcs := vcs.NewGitVCS()
	remoteRepoUrl := fmt.Sprintf("https://github.com/%s", modID)

	tags, err := vcs.Tags(context.TODO(), remoteRepoUrl)
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
func LoadPackages(ctx context.Context, main module.Version, opts PackageOpts) ([]*Formula, error) {
	formulaContext := newFormulaContext()
	defer formulaContext.gc()

	if main.Version == "" {
		cmp, err := formulaContext.comparatorOf(main.ID)
		if err != nil {
			return nil, err
		}
		latest, err := latestVersion(main.ID, cmp)
		if err != nil {
			return nil, err
		}
		main.Version = latest
	}
	mainFormula, err := formulaContext.formulaOf(main)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	mainDeps, err := resolveDeps(ctx, mainFormula)
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
		compare, err := formulaContext.comparatorOf(p)
		if err != nil {
			panic(err)
		}
		return compare(module.Version{p, v1}, module.Version{p, v2})
	}

	graph := mvs.NewGraph(cmp, []module.Version{main})

	graph.Require(main, mainDeps)

	onLoad := func(mod module.Version) ([]module.Version, error) {
		f, err := formulaContext.formulaOf(mod)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()

		deps, err := resolveDeps(ctx, f)
		if err != nil {
			return nil, err
		}

		graph.Require(mod, deps)
		return deps, nil
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
		if err := tidy(main, mainFormula.Dir, reqs); err != nil {
			return nil, err
		}
	}

	var formulas []*Formula

	for _, mod := range buildList {
		f, err := formulaContext.formulaOf(mod)
		if err != nil {
			return nil, err
		}
		f.markUse()
		// fill the dep list
		if mod == main {
			f.Proj.Deps = buildList[1:]
		} else {
			f.Proj.Deps, _ = graph.RequiredBy(mod)
		}
		formulas = append(formulas, f)
	}

	return formulas, nil
}

// tidy computes minimal dependencies using mvs.Req and updates versions.json.
func tidy(main module.Version, formulaDir string, reqs *mvsReqs) error {
	minDeps, err := mvs.Req(main, []string{}, reqs)
	if err != nil {
		return err
	}

	versionsFile := filepath.Join(formulaDir, "versions.json")
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
