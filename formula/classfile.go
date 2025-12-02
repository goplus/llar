package formula

import (
	"github.com/qiniu/x/gsh"
)

// -----------------------------------------------------------------------------

// ModuleF represents the build formula of a module.
type ModuleF struct {
	gsh.App

	fOnRequire func(proj *Project, deps *ModuleDeps)
	fOnBuild   func(proj *Project, out *BuildResult)

	modID      string
	modFromVer string
}

// Id sets the module ID that this formula serves.
// modID should be in the form of "owner/repo".
func (p *ModuleF) Id(modID string) {
	p.modID = modID
}

// FromVer sets the minimum version of the module that this formula serves.
func (p *ModuleF) FromVer(ver string) {
	p.modFromVer = ver
}

// -----------------------------------------------------------------------------

// ModuleDeps represents the dependencies of a module.
type ModuleDeps struct {
}

// Require declares that the module being built depends on the specified
// module (by its modID and version).
func (p *ModuleDeps) Require(modID, ver string) {
	panic("TODO")
}

// OnRequire event is used to retrieve all direct dependencies of a
// project (module). proj is the project being built, deps is used to
// declare dependencies.
func (p *ModuleF) OnRequire(f func(proj *Project, deps *ModuleDeps)) {
	p.fOnRequire = f
}

// -----------------------------------------------------------------------------

// BuildResult represents the result of building a project.
type BuildResult struct {
}

// OnBuild event is used to instruct the Formula to compile a project.
func (p *ModuleF) OnBuild(f func(proj *Project, out *BuildResult)) {
	p.fOnBuild = f
}

// -----------------------------------------------------------------------------
