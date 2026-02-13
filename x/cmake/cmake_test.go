package cmake

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUseSetsEnv(t *testing.T) {
	root := t.TempDir()
	includeDir := filepath.Join(root, "include")
	libDir := filepath.Join(root, "lib")
	pkgconfigDir := filepath.Join(libDir, "pkgconfig")
	for _, d := range []string{includeDir, libDir, pkgconfigDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	for _, key := range []string{
		"PKG_CONFIG_PATH", "CMAKE_PREFIX_PATH", "CMAKE_INCLUDE_PATH",
		"CMAKE_LIBRARY_PATH", "INCLUDE", "LIB", "CPPFLAGS", "LDFLAGS",
	} {
		t.Setenv(key, "")
	}

	c := New("", "", "")
	c.Use(root)

	for key, want := range map[string]string{
		"PKG_CONFIG_PATH":    pkgconfigDir,
		"CMAKE_PREFIX_PATH":  root,
		"CMAKE_INCLUDE_PATH": includeDir,
		"CMAKE_LIBRARY_PATH": libDir,
	} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}

	if runtime.GOOS == "windows" {
		if got := os.Getenv("INCLUDE"); got != includeDir {
			t.Errorf("INCLUDE = %q, want %q", got, includeDir)
		}
		if got := os.Getenv("LIB"); got != libDir {
			t.Errorf("LIB = %q, want %q", got, libDir)
		}
	} else {
		if got := os.Getenv("CPPFLAGS"); strings.TrimSpace(got) != "-I"+includeDir {
			t.Errorf("CPPFLAGS = %q, want %q", got, "-I"+includeDir)
		}
		if got := os.Getenv("LDFLAGS"); strings.TrimSpace(got) != "-L"+libDir {
			t.Errorf("LDFLAGS = %q, want %q", got, "-L"+libDir)
		}
	}
}

func TestUsePartialDirs(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "include"), 0o755)

	for _, key := range []string{
		"PKG_CONFIG_PATH", "CMAKE_LIBRARY_PATH",
	} {
		t.Setenv(key, "")
	}

	c := New("", "", "")
	c.Use(root)

	if got := os.Getenv("PKG_CONFIG_PATH"); got != "" {
		t.Errorf("PKG_CONFIG_PATH = %q, want empty", got)
	}
	if got := os.Getenv("CMAKE_LIBRARY_PATH"); got != "" {
		t.Errorf("CMAKE_LIBRARY_PATH = %q, want empty", got)
	}
}

func TestOutputDir(t *testing.T) {
	if got := New("", "build", "").OutputDir(); got != "build" {
		t.Errorf("OutputDir = %q, want %q", got, "build")
	}
	if got := New("", "build", "inst").OutputDir(); got != "inst" {
		t.Errorf("OutputDir = %q, want %q", got, "inst")
	}
}

func TestDefinesArgs(t *testing.T) {
	c := New("", "", "")
	c.Define("FOO", "BAR")
	c.DefineBool("ENABLE", true)
	c.DefineBool("DISABLE", false)

	args := c.definesArgs()
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"-DDISABLE:BOOL=OFF",
		"-DENABLE:BOOL=ON",
		"-DFOO:STRING=BAR",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("definesArgs missing %q, got %q", want, joined)
		}
	}

	// Verify sorted order
	if args[0] != "-DDISABLE:BOOL=OFF" || args[1] != "-DENABLE:BOOL=ON" || args[2] != "-DFOO:STRING=BAR" {
		t.Errorf("definesArgs not sorted: %v", args)
	}
}

func TestDefinesArgsEmpty(t *testing.T) {
	c := New("", "", "")
	if args := c.definesArgs(); args != nil {
		t.Errorf("definesArgs on empty = %v, want nil", args)
	}
}

func TestSource(t *testing.T) {
	c := New("orig", "", "")
	c.Source("/new")
	if c.sourceDir != "/new" {
		t.Errorf("sourceDir = %q, want %q", c.sourceDir, "/new")
	}
}

func TestPrependPath(t *testing.T) {
	t.Setenv("TEST_PREPEND", "/existing")
	prependPath("TEST_PREPEND", "/new")

	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	want := "/new" + sep + "/existing"
	if got := os.Getenv("TEST_PREPEND"); got != want {
		t.Errorf("TEST_PREPEND = %q, want %q", got, want)
	}
}

func TestAppendFlag(t *testing.T) {
	t.Setenv("TEST_FLAGS", "-Ifoo")
	appendFlag("TEST_FLAGS", "-Ibar")

	if got := os.Getenv("TEST_FLAGS"); got != "-Ifoo -Ibar" {
		t.Errorf("TEST_FLAGS = %q, want %q", got, "-Ifoo -Ibar")
	}
}

func TestConfigureBuildInstallE2E(t *testing.T) {
	if _, err := exec.LookPath("cmake"); err != nil {
		t.Skip("cmake not found in PATH")
	}

	tmp := t.TempDir()
	installDir := filepath.Join(tmp, "install")
	buildDir := filepath.Join(tmp, "build")

	c := New(filepath.Join("testdata", "project"), buildDir, installDir)
	c.BuildType("Release")
	c.Generator("Unix Makefiles")

	toolchain := filepath.Join(tmp, "toolchain.cmake")
	os.WriteFile(toolchain, []byte("# dummy toolchain"), 0o644)
	c.Toolchain(toolchain)

	c.Define("FOO", "BAR")
	c.DefineBool("ENABLE", true)
	c.DefineBool("DISABLE", false)

	if err := c.Configure(); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if err := c.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := c.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}

	for _, path := range []string{
		filepath.Join(installDir, "lib", "libdummy.a"),
		filepath.Join(installDir, "include", "dummy.h"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s", path)
		}
	}

	data, err := os.ReadFile(filepath.Join(buildDir, "CMakeCache.txt"))
	if err != nil {
		t.Fatalf("read CMakeCache.txt: %v", err)
	}
	cache := string(data)
	for _, want := range []string{
		"FOO:STRING=BAR",
		"ENABLE:BOOL=ON",
		"DISABLE:BOOL=OFF",
		"CMAKE_BUILD_TYPE:STRING=Release",
		"CMAKE_INSTALL_PREFIX",
		"CMAKE_TOOLCHAIN_FILE",
	} {
		if !strings.Contains(cache, want) {
			t.Errorf("cache missing %q", want)
		}
	}
}
