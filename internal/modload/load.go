package modload

import (
	"context"
	"time"

	"github.com/goplus/llar/internal/mvs"
	"github.com/goplus/llar/pkgs/mod/module"
)

// LoadPackages loads all packages required by the main module and resolves
// their dependencies using the MVS algorithm. It returns formulas for all
// modules in the computed build list.
func LoadPackages(ctx context.Context, main module.Version) ([]*Formula, error) {
	formulaContext := newFormulaContext()
	defer formulaContext.gc()

	onLoad := func(mod module.Version) ([]module.Version, error) {
		f, err := formulaContext.formulaOf(mod)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()

		return resolveDeps(ctx, f)
	}

	f, err := formulaContext.formulaOf(main)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	mainDeps, err := resolveDeps(ctx, f)
	if err != nil {
		return nil, err
	}
	reqs := &mvsReqs{
		roots: mainDeps,
		isMain: func(v module.Version) bool {
			return v.ID == main.ID && v.Version == main.Version
		},
		cmp: func(p, v1, v2 string) int {
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
		},
		onLoad: onLoad,
	}

	buildList, err := mvs.BuildList([]module.Version{main}, reqs)
	if err != nil {
		return nil, err
	}

	var formulas []*Formula

	for _, mod := range buildList {
		f, err := formulaContext.formulaOf(mod)
		if err != nil {
			return nil, err
		}
		f.markUse()
		formulas = append(formulas, f)
	}

	return formulas, nil
}
