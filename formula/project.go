package formula

import (
	"io/fs"

	"github.com/goplus/llar/pkgs/mod/module"
)

// -----------------------------------------------------------------------------

// Project represents a project (module) being built.
type Project struct {
	Deps   []module.Version
	FileFS fs.ReadFileFS
}

type Context struct {
	Matrix       Matrix
	SourceDir    string
	BuildResults map[module.Version]BuildResult
}

// ReadFile reads the content of a file in the project.
func (p *Project) ReadFile(path string) ([]byte, error) {
	return p.FileFS.ReadFile(path)
}

// -----------------------------------------------------------------------------
