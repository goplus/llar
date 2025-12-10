package formula

import (
	"io"
	"io/fs"
)

// -----------------------------------------------------------------------------

// Project represents a project (module) being built.
type Project struct {
	DirFS fs.FS
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
