package formula

import (
	"io/fs"

	"github.com/goplus/llar/pkgs/mod/module"
)

// -----------------------------------------------------------------------------

// Project represents a project (module) being built.
type Project struct {
	DirFS        fs.FS
	Deps         []module.Version
	BuildResults map[module.Version]*BuildResult
	Matrix       Matrix
}

// ReadFile reads the content of a file in the project.
func (p *Project) ReadFile(path string) ([]byte, error) {
	fs := p.DirFS.(fs.ReadFileFS)
	return fs.ReadFile(path)
}

// -----------------------------------------------------------------------------
