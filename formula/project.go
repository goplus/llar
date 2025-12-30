package formula

import (
	"io/fs"

	"github.com/goplus/llar/pkgs/mod/module"
)

// -----------------------------------------------------------------------------

// Project represents a project (module) being built.
type Project struct {
	FileFS       fs.ReadFileFS
	Deps         []module.Version
	BuildResults map[module.Version]*BuildResult
	Matrix       Matrix
}

// ReadFile reads the content of a file in the project.
func (p *Project) ReadFile(path string) ([]byte, error) {
	return p.FileFS.ReadFile(path)
}

// -----------------------------------------------------------------------------
