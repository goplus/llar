package modules

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	classfile "github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/formula"
	"github.com/goplus/llar/pkgs/mod/module"
	"github.com/goplus/llar/pkgs/mod/versions"
)

func newProject(ctx context.Context, mod module.Version, f *formula.Formula) (*classfile.Project, error) {
	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("llar-build-%s-%s-*", strings.ReplaceAll(mod.ID, "/", "-"), mod.Version))
	if err != nil {
		return nil, err
	}
	f.Sync(ctx, tmpDir)
}

// resolveDeps resolves the dependencies for a formula.
// It first tries to get dependencies from the OnRequire callback,
// then falls back to parsing versions.json if no dependencies are found.
func resolveDeps(ctx context.Context, f *Formula) ([]module.Version, error) {
	if err := initProj(ctx, f); err != nil {
		return nil, err
	}

	var deps formula.ModuleDeps

	// onRequire is optional
	if f.OnRequire != nil {
		f.OnRequire(f.Proj, &deps)
	}

	depTable, err := versions.Parse(filepath.Join(f.Dir, "versions.json"), nil)
	if err != nil {
		return nil, err
	}
	current := depTable.Dependencies[f.Version.Version]

	var vers []module.Version

	for _, dep := range deps.Deps {
		if dep.Version == "" {
			// if a version of a dep input by onRequire is empty, try our best to resolve it.
			idx := slices.IndexFunc[[]versions.Dependency](current, func(depInTable versions.Dependency) bool {
				return depInTable.ModuleID == dep.ModuleID
			})
			if idx < 0 {
				// It seems safe to drop deps here, because we resolve deps recursively and finally we will find that dep.
				continue
			}
			dep.Version = current[idx].Version
		}

		vers = append(vers, module.Version{
			ID:      dep.ModuleID,
			Version: dep.Version,
		})
	}

	if len(vers) > 0 {
		return vers, nil
	}

	for _, dep := range current {
		if dep.Version != "" {
			vers = append(vers, module.Version{
				ID:      dep.ModuleID,
				Version: dep.Version,
			})
		}
	}

	return vers, nil
}
