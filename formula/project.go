package formula

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/goplus/llar/internal/env"
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

// pathSeparator returns the path list separator for the current OS.
// Windows uses ";", Unix-like systems use ":".
func pathSeparator() string {
	if runtime.GOOS == "windows" {
		return ";"
	}
	return ":"
}

// prependEnv prepends a value to an environment variable using the appropriate separator.
func prependEnv(key, value string) {
	sep := pathSeparator()
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
// Following Conan's approach, it sets up environment variables for different build systems:
//
// For Autotools/GCC (Unix):
//   - CPPFLAGS: -I flags for include paths
//   - LDFLAGS: -L flags for library paths
//
// For CMake (all platforms):
//   - CMAKE_PREFIX_PATH: CMake package search path
//   - CMAKE_INCLUDE_PATH: CMake include search path
//   - CMAKE_LIBRARY_PATH: CMake library search path
//
// For pkg-config (all platforms):
//   - PKG_CONFIG_PATH: pkg-config .pc file search path
//
// For Windows MSVC:
//   - INCLUDE: MSVC include search path
//   - LIB: MSVC library search path
func (p *Project) Use(mod module.Version) {
	if len(p.Matrix.Require) == 0 {
		return
	}
	formulaDir, err := env.FormulaDir()
	if err != nil {
		return
	}
	buildDir := filepath.Join(formulaDir, mod.ID, "build", mod.Version, p.Matrix.String())

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

// -----------------------------------------------------------------------------
