package formula

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/goplus/llar/internal/env"
	"github.com/goplus/llar/pkgs/mod/module"
	"github.com/goplus/llar/pkgs/mod/versions"
	"github.com/qiniu/x/gsh"
)

const GopPackage = true

// -----------------------------------------------------------------------------

// ModuleF represents the build formula of a module.
type ModuleF struct {
	gsh.App

	fOnRequire func(proj *Project, deps *ModuleDeps)
	fOnBuild   func(proj *Project, out *BuildResult) error

	modID      string
	modFromVer string
	matrix     Matrix
}

type Matrix struct {
	Require        map[string][]string
	Options        map[string][]string
	DefaultOptions map[string][]string
}

// String returns the string representation of a single matrix.
// Keys are sorted alphabetically, values are joined with "-".
// Require and options are joined with "|".
func (m *Matrix) String() string {
	// Helper function to build string for a map (single value per key)
	buildPart := func(kvs map[string][]string) string {
		if len(kvs) == 0 {
			return ""
		}

		// Sort keys alphabetically
		keys := make([]string, 0, len(kvs))
		for k := range kvs {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		// Build result by joining values with "-"
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			if len(kvs[k]) > 0 {
				parts = append(parts, kvs[k][0])
			}
		}
		return strings.Join(parts, "-")
	}

	requirePart := buildPart(m.Require)
	optionsPart := buildPart(m.Options)

	if requirePart == "" {
		return optionsPart
	}
	if optionsPart == "" {
		return requirePart
	}
	return requirePart + "|" + optionsPart
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

// prependEnv prepends a value to an environment variable using the appropriate separator.
func prependEnv(key, value string) {
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	current := os.Getenv(key)
	if current == "" {
		os.Setenv(key, value)
	} else {
		os.Setenv(key, value+sep+current)
	}
}

// appendFlag appends a flag to an environment variable (space-separated).
func appendFlag(key, flag string) {
	current := os.Getenv(key)
	if current == "" {
		os.Setenv(key, flag)
	} else {
		os.Setenv(key, strings.TrimSpace(current+" "+flag))
	}
}

// Use configures the build environment to use the specified module.
func (p *ModuleF) Use(mod module.Version, matrix Matrix) {
	if len(matrix.Require) == 0 {
		return
	}
	formulaDir, err := env.FormulaDir()
	if err != nil {
		return
	}
	// TODO(MeteorsLiu): Localize path with filepath.Localize
	buildDir := filepath.Join(formulaDir, mod.ID, "build", mod.Version, matrix.String())

	includeDir := filepath.Join(buildDir, "include")
	libDir := filepath.Join(buildDir, "lib")
	pkgconfigDir := filepath.Join(buildDir, "lib", "pkgconfig")

	// PKG_CONFIG_PATH - pkg-config path (all platforms)
	if _, err := os.Stat(pkgconfigDir); err == nil {
		prependEnv("PKG_CONFIG_PATH", pkgconfigDir)
	}

	// CMAKE paths (all platforms)
	if _, err := os.Stat(buildDir); err == nil {
		prependEnv("CMAKE_PREFIX_PATH", buildDir)
	}
	if _, err := os.Stat(includeDir); err == nil {
		prependEnv("CMAKE_INCLUDE_PATH", includeDir)
	}
	if _, err := os.Stat(libDir); err == nil {
		prependEnv("CMAKE_LIBRARY_PATH", libDir)
	}

	// Platform-specific settings
	if runtime.GOOS == "windows" {
		// Windows MSVC environment variables
		if _, err := os.Stat(includeDir); err == nil {
			prependEnv("INCLUDE", includeDir)
		}
		if _, err := os.Stat(libDir); err == nil {
			prependEnv("LIB", libDir)
		}
	} else {
		// Unix (Linux/macOS) - Autotools/GCC style flags
		if _, err := os.Stat(includeDir); err == nil {
			appendFlag("CPPFLAGS", "-I"+includeDir)
		}
		if _, err := os.Stat(libDir); err == nil {
			appendFlag("LDFLAGS", "-L"+libDir)
		}
	}
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
	Deps []versions.Dependency
}

// Require declares that the module being built depends on the specified
// module (by its modID and version).
func (p *ModuleDeps) Require(modID, ver string) {
	p.Deps = append(p.Deps, versions.Dependency{ModuleID: modID, Version: ver})
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
	OutputDir string
}

// OnBuild event is used to instruct the Formula to compile a project.
func (p *ModuleF) OnBuild(f func(proj *Project, out *BuildResult) error) {
	p.fOnBuild = f
}

// -----------------------------------------------------------------------------

// Gopt_App_Main is main entry of this classfile.
func Gopt_ModuleF_Main(this interface {
	app() *gsh.App
	MainEntry()
}) {
	this.MainEntry()
	gsh.InitApp(this.app())
}
