package modules

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	classfile "github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/formula"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/pkgs/mod/module"
	"github.com/goplus/llar/pkgs/mod/versions"
)

// resolveDeps resolves the dependencies for a formula.
// It first tries to get dependencies from the OnRequire callback,
// then falls back to parsing versions.json if no dependencies are found.
func resolveDeps(ctx context.Context, mainMod module.Version, mainFormula *formula.Formula) ([]module.Version, error) {
	var deps classfile.ModuleDeps

	// TODO(MeteorsLiu): Support different code host sites.
	repo, err := vcs.NewRepo(fmt.Sprintf("github.com/%s", mainMod.Path))
	if err != nil {
		return nil, err
	}

	moduleDir, err := moduleDirOf(mainMod.Path)
	if err != nil {
		return nil, err
	}
	sourceCacheDir, err := sourceCacheDirOf(mainMod)
	if err != nil {
		return nil, err
	}
	repoFS := repo.At(mainMod.Version, sourceCacheDir)
	proj := &classfile.Project{
		FileFS: repoFS.(fs.ReadFileFS),
	}
	// onRequire is optional
	if mainFormula.OnRequire != nil {
		mainFormula.OnRequire(proj, &deps)
	}

	depTable, err := versions.Parse(filepath.Join(moduleDir, "versions.json"), nil)
	if err != nil {
		return nil, err
	}
	current := depTable.Dependencies[mainMod.Version]

	var vers []module.Version

	for _, dep := range deps.Deps {
		if dep.Version == "" {
			// if a version of a dep input by onRequire is empty, try our best to resolve it.
			idx := slices.IndexFunc[[]versions.Dependency](current, func(depInTable versions.Dependency) bool {
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
