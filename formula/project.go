package formula

import (
	"io/fs"

	"github.com/goplus/llar/pkgs/mod/module"
)

// -----------------------------------------------------------------------------

// Project represents a project (module) being built.
type Project struct {
	Deps     []module.Version
	SourceFS fs.ReadFileFS
}

// ReadFile reads the content of a file in the project.
func (p *Project) ReadFile(path string) ([]byte, error) {
	return p.SourceFS.ReadFile(path)
}

// Context represents the build context.
type Context struct {
	SourceDir string

	matrix       Matrix
	buildResults map[module.Version]BuildResult
}

// CurrentMatrix returns the active build matrix for this context.
func (c *Context) CurrentMatrix() Matrix {
	return c.matrix
}

// SetCurrentMatrix sets the active build matrix for this context.
func (c *Context) SetCurrentMatrix(m Matrix) {
	c.matrix = m
}

// BuildResult returns the stored build result for the module, if any.
func (c *Context) BuildResult(mod module.Version) (BuildResult, bool) {
	r, ok := c.buildResults[mod]
	return r, ok
}

// SetBuildResult stores the build result for the given module.
func (c *Context) SetBuildResult(mod module.Version, result BuildResult) {
	if c.buildResults == nil {
		c.buildResults = make(map[module.Version]BuildResult)
	}
	c.buildResults[mod] = result
}

// -----------------------------------------------------------------------------
