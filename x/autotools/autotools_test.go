package autotools

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/mod/module"
)

func TestUseSetsEnv(t *testing.T) {
	tempDir := t.TempDir()

	// Simulate a built module at workspaceDir/dep-amd64-linux
	mod := module.Version{Path: "dep", Version: "1.0.0"}
	matrixStr := "amd64-linux"
	modBuildDir := filepath.Join(tempDir, "dep-"+matrixStr)

	includeDir := filepath.Join(modBuildDir, "include")
	libDir := filepath.Join(modBuildDir, "lib")
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

	matrix := formula.Matrix{
		Require: map[string][]string{
			"arch": {"amd64"},
			"os":   {"linux"},
		},
	}
	a := New(matrix, tempDir, "", "", "")

	if err := a.Use(mod); err != nil {
		t.Fatalf("Use() failed: %v", err)
	}

	expectEq := map[string]string{
		"PKG_CONFIG_PATH":    pkgconfigDir,
		"CMAKE_PREFIX_PATH":  modBuildDir,
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

func TestUseNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	matrix := formula.Matrix{
		Require: map[string][]string{
			"arch": {"amd64"},
			"os":   {"linux"},
		},
	}
	a := New(matrix, tmpDir, "", "", "")
	err := a.Use(module.Version{Path: "nonexistent/mod", Version: "1.0.0"})
	if err == nil {
		t.Fatal("Use() expected error for non-existent module build dir")
	}
}

func TestOutputDirPrefersInstall(t *testing.T) {
	a := New(formula.Matrix{}, "", "", "build", "")
	if got := a.OutputDir(); got != "build" {
		t.Fatalf("default OutputDir = %q, want %q", got, "build")
	}

	a2 := New(formula.Matrix{}, "", "", "build", "custom-install")
	if got := a2.OutputDir(); got != "custom-install" {
		t.Fatalf("OutputDir with installDir = %q, want %q", got, "custom-install")
	}
}

func TestConfigureBuildInstallE2E(t *testing.T) {
	for _, bin := range []string{"make", "cc", "ar"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not found in PATH", bin)
		}
	}

	tmp := t.TempDir()
	installDir := filepath.Join(tmp, "install")
	buildDir := filepath.Join(tmp, "build")
	sourceDir := filepath.Join("testdata", "project")
	absSourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		t.Fatalf("abs source dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(absSourceDir, "configure")); err != nil {
		t.Fatalf("missing configure: %v", err)
	}

	t.Setenv("CUSTOM", "")

	a := New(formula.Matrix{}, "", absSourceDir, buildDir, installDir)
	a.Env("CUSTOM", "VAL")

	if err := a.Configure("--enable-foo"); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if err := a.Build(); err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := a.Install(); err != nil {
		t.Fatalf("install: %v", err)
	}

	cache := filepath.Join(buildDir, "config.log")
	data, err := os.ReadFile(cache)
	if err != nil {
		t.Fatalf("read config.log: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"CUSTOM=VAL",
		"PREFIX=" + installDir,
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("config.log missing %q", snippet)
		}
	}

	wantLib := filepath.Join(installDir, "lib", "libdummy.a")
	if _, err := os.Stat(wantLib); err != nil {
		t.Fatalf("installed lib missing: %v", err)
	}
	wantHeader := filepath.Join(installDir, "include", "dummy.h")
	if _, err := os.Stat(wantHeader); err != nil {
		t.Fatalf("installed header missing: %v", err)
	}
}
