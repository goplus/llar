package modload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/pkgs/mod/module"
	"github.com/goplus/llar/pkgs/mod/versions"
)

// initProj initializes the project directory for a formula.
// It creates a temporary directory and syncs the source code from remote repository.
func initProj(ctx context.Context, f *Formula) error {
	if f.Proj != nil {
		return nil
	}
	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("llar-build-%s-%s-*", strings.ReplaceAll(f.Version.ID, "/", "-"), f.Version.Version))
	if err != nil {
		return err
	}
	os.RemoveAll(tmpDir)

	f.Proj = &formula.Project{
		DirFS: os.DirFS(tmpDir),
	}
	return f.Sync(ctx, tmpDir)
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

	var vers []module.Version

	for _, dep := range deps.Deps {
		if dep.Version != "" {
			vers = append(vers, module.Version{
				ID:      dep.ModuleID,
				Version: dep.Version,
			})
		}
	}

	if len(vers) > 0 {
		return vers, nil
	}

	// fallback
	versions, err := versions.Parse(filepath.Join(f.Dir, "versions.json"), nil)
	if err != nil {
		return nil, err
	}

	for _, dep := range versions.Dependencies[f.Version.Version] {
		if dep.Version != "" {
			vers = append(vers, module.Version{
				ID:      dep.ModuleID,
				Version: dep.Version,
			})
		}
	}

	return vers, nil
}
