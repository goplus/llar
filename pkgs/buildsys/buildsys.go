package buildsys

import "github.com/goplus/llar/pkgs/mod/module"

// BuildSystem captures shared capabilities of build helpers (CMake, Autotools, etc).
// It keeps the common lifecycle and dependency/env setup; implementations add their own extras.
type BuildSystem interface {
	// Use injects a built dependency into the environment.
	Use(mod module.Version)

	// Basic paths.
	Source(dir string)
	InstallDir(dir string)

	// Environment helper.
	Env(key, val string)

	// Lifecycle.
	Configure(args ...string) error
	Build(args ...string) error
	Install(args ...string) error

	// Where artifacts land.
	OutputDir() string
}
