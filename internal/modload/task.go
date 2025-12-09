package modload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/pkgs/mod/module"
	"github.com/goplus/llar/pkgs/mod/versions"
)

type task struct {
	tmpDir string
	mod    module.Version
	proj   *formula.Project
}

func newTask(mod module.Version) (*task, error) {
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("llar-build-%s-%s*", mod.ID, mod.Version))
	if err != nil {
		return nil, err
	}
	return &task{
		mod: mod,
		proj: &formula.Project{
			DirFS: os.DirFS(tempDir),
		},
	}, nil
}

func (t *task) prepareSource(ctx *formulaContext) error {
	f, err := ctx.formulaOf(t.mod)
	if err != nil {
		return err
	}
	return f.Sync(context.TODO(), t.tmpDir)
}

func (t *task) resolveDeps(ctx *formulaContext) ([]module.Version, error) {
	f, err := ctx.formulaOf(t.mod)
	if err != nil {
		return nil, err
	}
	var deps formula.ModuleDeps

	// onRequire is optional
	if f.OnRequire != nil {
		f.OnRequire(t.proj, &deps)
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
	versions, err := versions.Parse(filepath.Join(), nil)
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
