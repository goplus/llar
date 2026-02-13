package autotools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	classfile "github.com/goplus/llar/formula"
	"github.com/goplus/llar/mod/module"
)

// AutoTools wraps common Autotools build steps with chainable configuration.
type AutoTools struct {
	matrix classfile.Matrix

	sourceDir    string
	buildDir     string
	installDir   string
	workspaceDir string
	env          map[string]string
}

// New creates a new AutoTools helper. Optional context enables use(mod).
func New(matrix classfile.Matrix, workspaceDir, sourceDir, buildDir, installDir string) *AutoTools {
	return &AutoTools{
		matrix:       matrix,
		sourceDir:    sourceDir,
		buildDir:     buildDir,
		installDir:   installDir,
		workspaceDir: workspaceDir,
		env:          map[string]string{},
	}
}

func (a *AutoTools) Source(dir string) {
	a.sourceDir = dir
}

func (a *AutoTools) Env(key, value string) {
	if a.env == nil {
		a.env = map[string]string{}
	}
	a.env[key] = value
	_ = os.Setenv(key, value)
}

// Use configures the build environment to use the specified module.
func (a *AutoTools) Use(mod module.Version) error {
	modPath, err := module.EscapePath(mod.Path)
	if err != nil {
		return err
	}
	matrixStr := a.matrix.Combinations()[0]

	// TODO(MeteorsLiu): Decouple this into a Workspace module?
	buildDir := filepath.Join(a.workspaceDir, fmt.Sprintf("%s-%s", modPath, matrixStr))

	if _, err := os.Stat(buildDir); os.IsNotExist(err) {
		return fmt.Errorf("failed to use %s@%s: buildDir %s not found", modPath, mod.Version, buildDir)
	}

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

	return nil
}

// Configure runs ./configure with standard flags.
func (a *AutoTools) Configure(args ...string) error {
	buildDir := a.buildDir
	if buildDir == "" {
		buildDir = "."
	}
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return err
	}

	exe := "./configure"
	if buildDir != "." && buildDir != "" {
		exe = filepath.Join(a.sourceDir, "configure")
	}

	configArgs := []string{}
	if a.installDir != "" {
		configArgs = append(configArgs, "--prefix="+a.installDir)
	}
	configArgs = append(configArgs, args...)

	return run(exe, configArgs, a.env, buildDir)
}

// Build runs make (or provided args) in the build directory.
func (a *AutoTools) Build(args ...string) error {
	buildDir := a.buildDir
	if buildDir == "" {
		buildDir = "."
	}
	cmdArgs := []string{}
	if len(args) == 0 {
		cmdArgs = append(cmdArgs, "make")
	} else {
		cmdArgs = append(cmdArgs, args...)
	}
	return run(cmdArgs[0], cmdArgs[1:], a.env, buildDir)
}

// Install runs make install (or provided args) in the build directory.
func (a *AutoTools) Install(args ...string) error {
	buildDir := a.buildDir
	if buildDir == "" {
		buildDir = "."
	}
	cmdArgs := []string{"make", "install"}
	if len(args) > 0 {
		cmdArgs = args
	}
	return run(cmdArgs[0], cmdArgs[1:], a.env, buildDir)
}

// OutputDir returns the install dir if set, otherwise the build dir.
func (a *AutoTools) OutputDir() string {
	if a.installDir != "" {
		return a.installDir
	}
	return a.buildDir
}

func run(bin string, args []string, env map[string]string, workdir string) error {
	cmd := exec.Command(bin, args...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if len(env) > 0 {
		cmd.Env = mergeEnv(os.Environ(), env)
	}
	return cmd.Run()
}

func mergeEnv(base []string, override map[string]string) []string {
	envMap := make(map[string]string, len(base))
	for _, kv := range base {
		if k, v, ok := strings.Cut(kv, "="); ok {
			envMap[k] = v
		}
	}
	for k, v := range override {
		envMap[k] = v
	}
	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+envMap[k])
	}
	return out
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
