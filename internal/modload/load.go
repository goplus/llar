package modload

import (
	"context"
	"fmt"
	"strings"

	"github.com/goplus/ixgo"
	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/loader"
	"github.com/goplus/llar/pkgs/mod/module"
)

type ModLoader struct {
	ctx         *ixgo.Context
	loader      loader.Loader
	formulas    map[module.Version]*Formula
	comparators map[string]module.VersionComparator
}

func NewModLoader() *ModLoader {
	ctx := ixgo.NewContext(ixgo.SupportMultipleInterp)
	return &ModLoader{
		ctx:         ctx,
		loader:      loader.NewFormulaLoader(ctx),
		formulas:    make(map[module.Version]*Formula),
		comparators: make(map[string]module.VersionComparator),
	}
}

func (m *ModLoader) comparatorOf(mod module.Version) (module.VersionComparator, error) {
	if comp, ok := m.comparators[mod.ID]; ok {
		return comp, nil
	}
	comp, err := loadComparator(m.loader, mod)
	if err != nil {
		return nil, err
	}
	m.comparators[mod.ID] = comp
	return comp, nil
}

func (m *ModLoader) formulaOf(mod module.Version) (*Formula, error) {
	comparator, err := m.comparatorOf(mod)
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
	f = &Formula{
		Version:   mod,
		Dir:       formulaPath,
		OnBuild:   formulaStruct.Value("fOnBuild").(func(*formula.Project, *formula.BuildResult)),
		OnRequire: formulaStruct.Value("fOnRequire").(func(*formula.Project, *formula.ModuleDeps)),
	}
	m.formulas[cacheKey] = f
	return f, nil
}

func (m *ModLoader) prepareSource(mod module.Version) error {
	refs, err := m.vcs.Tags(context.TODO(), m.remoteRepoOf(mod))
	if err != nil {
		return err
	}
	ref, ok := matchRef(refs, mod.Version)
	if !ok {
		return fmt.Errorf("failed to resolve version: cannot find a ref from version: %s", mod.Version)
	}
	err = vcs.Sync(context.TODO(), m.remoteRepoOf(mod), ref, m.tempDir)
	if err != nil {
		return err
	}
}

func (m *ModLoader) collectDeps(mod module.Version, proj *formula.Project) ([]module.Version, error) {
	f, err := m.formulaOf(mod)
	if err != nil {
		return nil, err
	}
	var deps formula.ModuleDeps

	// onRequire is optional
	if f.OnRequire != nil {
		f.OnRequire(proj, &deps)
	}

	if len(deps.Deps) > 0 {
		return
	}

}

func (m *ModLoader) LoadPackages(main module.Version) ([]*Formula, error) {
	proj := &formula.Project{}

}

func matchRef(refs []string, version string) (ref string, ok bool) {
	for _, r := range refs {
		if strings.HasSuffix(r, version) {
			return r, true
		}
	}
	return "", false
}
