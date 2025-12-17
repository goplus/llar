package formula

import (
	"io/fs"
	"os"

	"github.com/goplus/llar/pkgs/mod/module"
)

// -----------------------------------------------------------------------------

// Project represents a project (module) being built.
type Project struct {
	Dir          string
	FormulaDir   string
	Deps         []module.Version
	BuildResults map[module.Version]*BuildResult
	Matrix       Matrix
}

// ReadFile reads the content of a file in the project.
func (p *Project) ReadFile(path string) ([]byte, error) {
	fs := os.DirFS(p.Dir).(fs.ReadFileFS)
	return fs.ReadFile(path)
}

// -----------------------------------------------------------------------------
