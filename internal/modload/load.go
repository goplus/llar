package modload

import (
	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/pkgs/mod/module"
)

type ModLoader struct {
	ctx *formulaContext
}

func NewModLoader() *ModLoader {
	return &ModLoader{ctx: newFormulaContext()}
}

func (m *ModLoader) collectDeps(mod module.Version) ([]module.Version, error) {
	f, err := m.ctx.formulaOf(mod)
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
