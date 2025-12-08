package engine

import (
	"github.com/goplus/llar/internal/loader"
	"github.com/goplus/llar/internal/modload"
	"github.com/goplus/llar/pkgs/mod/module"
)

type buildContext struct {
	loader      loader.Loader
	tasks       []*Task
	comparators map[string]module.VersionComparator
}

func (c *buildContext) comparatorOf(mod module.Version) (module.VersionComparator, error) {
	if comp, ok := c.comparators[mod.ID]; ok {
		return comp, nil
	}
	comp, err := modload.LoadComparator(c.loader, mod)
	if err != nil {
		return nil, err
	}
	c.comparators[mod.ID] = comp
	return comp, nil
}

func (c *buildContext) push(task *Task) {

}
