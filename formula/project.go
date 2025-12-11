package formula

import (
	"io"
	"io/fs"

	"github.com/goplus/llar/pkgs/mod/module"
)

// -----------------------------------------------------------------------------

// Project represents a project (module) being built.
type Project struct {
	DirFS fs.FS
	Deps  []module.Version
}

// ReadFile reads the content of a file in the project.
func (p *Project) ReadFile(path string) ([]byte, error) {
	file, err := p.DirFS.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return io.ReadAll(file)
}

// -----------------------------------------------------------------------------
