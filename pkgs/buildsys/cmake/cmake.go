package cmake

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/pkgs/buildsys"
	"github.com/goplus/llar/pkgs/mod/module"
)

// CMake wraps common CMake build steps with chainable configuration.
type defineValue struct {
	value    string
	typeName string
}

type CMake struct {
	ctx        *formula.Context
	SourceDir  string
	buildDir   string
	installDir string
	generator  string
	buildType  string
	toolchain  string
	Defines    map[string]defineValue
	env        map[string]string
}

var _ buildsys.BuildSystem = (*CMake)(nil)

// New creates a new CMake helper. Optional context enables use(mod).
func New(ctx *formula.Context) *CMake {
	sourceDir := ""
	if ctx != nil {
		sourceDir = ctx.SourceDir
	}
	buildDir, err := os.MkdirTemp("", "llar-build-")
	if err != nil {
		buildDir = filepath.Join(sourceDir, "build")
	}
	c := &CMake{
		SourceDir:  sourceDir,
		buildDir:   buildDir,
		installDir: filepath.Join(sourceDir, "build"),
		Defines:    map[string]defineValue{},
		env:        map[string]string{},
	}
	c.ctx = ctx
	return c
}

func (c *CMake) Source(dir string) {
	c.SourceDir = dir
}

func (c *CMake) InstallDir(dir string) {
	c.installDir = dir
}

func (c *CMake) Generator(name string) *CMake {
	c.generator = name
	return c
}

func (c *CMake) BuildType(name string) *CMake {
	c.buildType = name
	return c
}

func (c *CMake) Toolchain(path string) *CMake {
	c.toolchain = path
	return c
}

func (c *CMake) Define(key, value string) *CMake {
	if c.Defines == nil {
		c.Defines = map[string]defineValue{}
	}
	c.Defines[key] = defineValue{value: value, typeName: "STRING"}
	return c
}

func (c *CMake) DefineBool(key string, value bool) *CMake {
	if c.Defines == nil {
		c.Defines = map[string]defineValue{}
	}
	if value {
		c.Defines[key] = defineValue{value: "ON", typeName: "BOOL"}
		return c
	}
	c.Defines[key] = defineValue{value: "OFF", typeName: "BOOL"}
	return c
}

func (c *CMake) Env(key, value string) {
	if c.env == nil {
		c.env = map[string]string{}
	}
	c.env[key] = value
	_ = os.Setenv(key, value)
}

// Use configures the build environment to use the specified module.
func (c *CMake) Use(mod module.Version) {
	if c.ctx == nil || c.ctx.BuildResults == nil {
		panic("cmake: context is not set")
	}
	depResult, ok := c.ctx.BuildResults[mod]
	if !ok {
		panic(fmt.Sprintf("cmake: dep not found: %s@%s", mod.Path, mod.Version))
	}
	buildDir := depResult.Dir

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

func (c *CMake) Configure(args ...string) error {
	buildDir := c.buildDir
	if buildDir == "" {
		buildDir = "build"
	}
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return err
	}
	cmakeArgs := []string{"-S", c.SourceDir, "-B", buildDir}
	if c.generator != "" {
		cmakeArgs = append(cmakeArgs, "-G", c.generator)
	}
	if c.installDir != "" {
		c.Define("CMAKE_INSTALL_PREFIX", c.installDir)
	}
	if c.toolchain != "" {
		c.Define("CMAKE_TOOLCHAIN_FILE", c.toolchain)
	}
	if c.buildType != "" {
		c.Define("CMAKE_BUILD_TYPE", c.buildType)
	}
	cmakeArgs = append(cmakeArgs, c.definesArgs()...)
	cmakeArgs = append(cmakeArgs, args...)

	return run("cmake", cmakeArgs, c.env)
}

func (c *CMake) Build(args ...string) error {
	buildDir := c.buildDir
	if buildDir == "" {
		buildDir = "build"
	}
	cmdArgs := []string{"--build", buildDir}
	if c.buildType != "" {
		cmdArgs = append(cmdArgs, "--config", c.buildType)
	}
	cmdArgs = append(cmdArgs, args...)
	return run("cmake", cmdArgs, c.env)
}

func (c *CMake) Install(args ...string) error {
	buildDir := c.buildDir
	if buildDir == "" {
		buildDir = "build"
	}
	cmdArgs := []string{"--install", buildDir}
	if c.installDir != "" {
		cmdArgs = append(cmdArgs, "--prefix", c.installDir)
	}
	cmdArgs = append(cmdArgs, args...)
	return run("cmake", cmdArgs, c.env)
}

// OutputDir returns the install dir if set, otherwise the build dir.
func (c *CMake) OutputDir() string {
	if c.installDir != "" {
		return c.installDir
	}
	return c.buildDir
}

func (c *CMake) definesArgs() []string {
	if len(c.Defines) == 0 {
		return nil
	}
	keys := make([]string, 0, len(c.Defines))
	for k := range c.Defines {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(keys))
	for _, k := range keys {
		def := c.Defines[k]
		if def.typeName != "" {
			args = append(args, "-D"+k+":"+def.typeName+"="+def.value)
			continue
		}
		args = append(args, "-D"+k+"="+def.value)
	}
	return args
}

func run(bin string, args []string, env map[string]string) error {
	cmd := exec.Command(bin, args...)
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
