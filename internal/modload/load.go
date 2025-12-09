package modload

import (
	"github.com/goplus/llar/pkgs/mod/module"
)

type ModLoader struct {
	ctx *formulaContext
}

func NewModLoader() *ModLoader {
	return &ModLoader{ctx: newFormulaContext()}
}

func (m *ModLoader) LoadPackages(main module.Version) ([]*Formula, error) {
	tasks := make(map[module.Version]*task)

	createTask := func(mod module.Version) (*task, error) {
		if t, ok := tasks[mod]; ok {
			return t, nil
		}
		t, err := newTask(mod)
		if err != nil {
			return nil, err
		}
		tasks[mod] = t
		return t, nil
	}
	onLoad := func(mod module.Version) ([]module.Version, error) {
		t, err := createTask(main)
		if err != nil {
			return nil, err
		}
		deps, err := t.resolveDeps(m.ctx)
		if err != nil {
			return nil, err
		}
		return deps, nil
	}

	t, err := createTask(main)
	if err != nil {
		return nil, err
	}
	mainDeps, err := t.resolveDeps(m.ctx)
	if err != nil {
		return nil, err
	}
	reqs := &mvsReqs{
		roots: mainDeps,
		isMain: func(v module.Version) bool {
			return v.ID == main.ID && v.Version == main.Version
		},
		cmp: func(p, v1, v2 string) int {
			m.ctx.comparatorOf()
		},
	}
}
