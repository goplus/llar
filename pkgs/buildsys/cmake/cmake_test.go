package cmake

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/pkgs/mod/module"
)

func TestUseSetsEnv(t *testing.T) {
	tempDir := t.TempDir()
	includeDir := filepath.Join(tempDir, "include")
	libDir := filepath.Join(tempDir, "lib")
	pkgconfigDir := filepath.Join(libDir, "pkgconfig")

	for _, dir := range []string{includeDir, libDir, pkgconfigDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	for _, key := range []string{
		"PKG_CONFIG_PATH",
		"CMAKE_PREFIX_PATH",
		"CMAKE_INCLUDE_PATH",
		"CMAKE_LIBRARY_PATH",
		"INCLUDE",
		"LIB",
		"CPPFLAGS",
		"LDFLAGS",
	} {
		t.Setenv(key, "")
	}

	ctx := &formula.Context{
		BuildResults: map[module.Version]formula.BuildResult{
			{Path: "dep", Version: "1.0.0"}: {Dir: tempDir},
		},
	}
	c := New(ctx)

	c.Use(module.Version{Path: "dep", Version: "1.0.0"})

	expectEq := map[string]string{
		"PKG_CONFIG_PATH":    pkgconfigDir,
		"CMAKE_PREFIX_PATH":  tempDir,
		"CMAKE_INCLUDE_PATH": includeDir,
		"CMAKE_LIBRARY_PATH": libDir,
	}
	for k, v := range expectEq {
		if got := os.Getenv(k); got != v {
			t.Fatalf("%s = %q, want %q", k, got, v)
		}
	}

	if runtime.GOOS == "windows" {
		if got := os.Getenv("INCLUDE"); got != includeDir {
			t.Fatalf("INCLUDE = %q, want %q", got, includeDir)
		}
		if got := os.Getenv("LIB"); got != libDir {
			t.Fatalf("LIB = %q, want %q", got, libDir)
		}
	} else {
		if got := os.Getenv("CPPFLAGS"); strings.TrimSpace(got) != "-I"+includeDir {
			t.Fatalf("CPPFLAGS = %q, want %q", got, "-I"+includeDir)
		}
		if got := os.Getenv("LDFLAGS"); strings.TrimSpace(got) != "-L"+libDir {
			t.Fatalf("LDFLAGS = %q, want %q", got, "-L"+libDir)
		}
	}
}

func TestOutputDirPrefersInstall(t *testing.T) {
	c := New(nil)
	if got := c.OutputDir(); got != "build" {
		t.Fatalf("default OutputDir = %q, want %q", got, "build")
	}
	c.InstallDir("custom-install")
	if got := c.OutputDir(); got != "custom-install" {
		t.Fatalf("OutputDir after InstallDir = %q, want %q", got, "custom-install")
	}
}

func TestConfigureBuildInstallE2E(t *testing.T) {
	if _, err := exec.LookPath("cmake"); err != nil {
		t.Skip("cmake not found in PATH")
	}

	tmp := t.TempDir()
	installDir := filepath.Join(tmp, "install")
	sourceDir := filepath.Join("testdata", "project")

	c := New(nil)
	defer os.RemoveAll(c.buildDir)
	c.Env("CUSTOM", "VAL")
	c.Source(sourceDir)
	c.InstallDir(installDir)
	c.BuildType("Release")
	c.Generator("Unix Makefiles")
	toolchain := filepath.Join(tmp, "toolchain.cmake")
	if err := os.WriteFile(toolchain, []byte("# dummy toolchain"), 0o644); err != nil {
		t.Fatalf("write toolchain: %v", err)
	}
	c.Toolchain(toolchain)
	c.Define("FOO", "BAR")
	c.DefineBool("ENABLE", true)
	c.DefineBool("DISABLE", false)

	if err := c.Configure(); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if err := c.Build("--config", "Release"); err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := c.Install(); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Verify install outputs.
	wantLib := filepath.Join(installDir, "lib", "libdummy.a")
	if _, err := os.Stat(wantLib); err != nil {
		t.Fatalf("installed lib missing: %v", err)
	}
	wantHeader := filepath.Join(installDir, "include", "dummy.h")
	if _, err := os.Stat(wantHeader); err != nil {
		t.Fatalf("installed header missing: %v", err)
	}

	// Verify cache contains our definitions.
	cache := filepath.Join(c.buildDir, "CMakeCache.txt")
	data, err := os.ReadFile(cache)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"FOO:STRING=BAR",
		"ENABLE:BOOL=ON",
		"DISABLE:BOOL=OFF",
		"CMAKE_BUILD_TYPE:STRING=Release",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("cache missing %q", snippet)
		}
	}
}
