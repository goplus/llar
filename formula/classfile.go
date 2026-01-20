package formula

import (
	"sort"

	"github.com/goplus/llar/pkgs/mod/module"
	"github.com/qiniu/x/gsh"
)

const GopPackage = true

// -----------------------------------------------------------------------------

// ModuleF represents the build formula of a module.
type ModuleF struct {
	gsh.App

	fOnRequire func(proj *Project, deps *ModuleDeps)
	fOnBuild   func(proj *Project, out *BuildResult)

	modID      string
	modFromVer string
	matrix     Matrix
}

type Matrix struct {
	Require        map[string][]string
	Options        map[string][]string
	DefaultOptions map[string][]string
}

// Combinations returns all cartesian product combinations of the matrix.
// Keys are sorted alphabetically, and combinations are built layer by layer.
// Require fields are joined with "-", then combined with options using "|".
func (m *Matrix) Combinations() []string {
	// Helper function to compute cartesian product for a map
	cartesian := func(kvs map[string][]string) []string {
		if len(kvs) == 0 {
			return nil
		}

		// Sort keys alphabetically
		keys := make([]string, 0, len(kvs))
		for k := range kvs {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		// Start with first key's values
		result := make([]string, len(kvs[keys[0]]))
		copy(result, kvs[keys[0]])

		// Combine with subsequent layers using "-"
		for i := 1; i < len(keys); i++ {
			values := kvs[keys[i]]
			newResult := make([]string, 0, len(result)*len(values))
			for _, prev := range result {
				for _, v := range values {
					newResult = append(newResult, prev+"-"+v)
				}
			}
			result = newResult
		}
		return result
	}

	// Compute require combinations
	requireCombos := cartesian(m.Require)

	// Compute options combinations
	optionsCombos := cartesian(m.Options)

	// If no require, just return options
	if len(requireCombos) == 0 {
		return optionsCombos
	}

	// If no options, just return require
	if len(optionsCombos) == 0 {
		return requireCombos
	}

	// Combine require with options using "|"
	result := make([]string, 0, len(requireCombos)*len(optionsCombos))
	for _, req := range requireCombos {
		for _, opt := range optionsCombos {
			result = append(result, req+"|"+opt)
		}
	}

	return result
}

// CombinationCount returns the total number of cartesian product combinations.
func (m *Matrix) CombinationCount() int {
	countPart := func(kvs map[string][]string) int {
		if len(kvs) == 0 {
			return 0
		}
		count := 1
		for _, v := range kvs {
			count *= len(v)
		}
		return count
	}

	requireCount := countPart(m.Require)
	optionsCount := countPart(m.Options)

	if requireCount == 0 {
		return optionsCount
	}
	if optionsCount == 0 {
		return requireCount
	}
	return requireCount * optionsCount
}

func (p *ModuleF) app() *gsh.App {
	return &p.App
}

func (p *ModuleF) Matrix(m Matrix) {
	p.matrix = m
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
	Deps []module.Version
}

// Require declares that the module being built depends on the specified
// module (by its modID and version).
func (p *ModuleDeps) Require(modID, ver string) {
	p.Deps = append(p.Deps, module.Version{Path: modID, Version: ver})
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

// Gopt_ModuleF_Main is main entry of this classfile.
func Gopt_ModuleF_Main(this interface {
	app() *gsh.App
	MainEntry()
}) {
	this.MainEntry()
	gsh.InitApp(this.app())
}
