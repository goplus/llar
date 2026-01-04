package formula

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/goplus/llar/pkgs/mod/module"
)

// -----------------------------------------------------------------------------

// Project represents a project (module) being built.
type Project struct {
	FileFS       fs.ReadFileFS
	Deps         []module.Version
	BuildDir     string
	BuildResults map[module.Version]*BuildResult
	Matrix       Matrix
}

// ReadFile reads the content of a file in the project.
func (p *Project) ReadFile(path string) ([]byte, error) {
	return p.FileFS.ReadFile(path)
}

// Use configures the build environment to use the specified module.
func (p *Project) Use(mod module.Version) {
	depResult := p.BuildResults[mod]
	if depResult == nil {
		panic("dep not found")
	}
	buildDir := depResult.OutputDir

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
