package modload

import (
	"fmt"
	"os"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/pkgs/mod/module"
)

type task struct {
	tmpDir string
	proj   *formula.Project
}

func newTask(mod module.Version) (*task, error) {
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("llar-build-%s-%s*", mod.ID, mod.Version))
	if err != nil {
		return nil, err
	}
	return &task{
		proj: &formula.Project{},
	}
}
