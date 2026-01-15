package autotools

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
	a := New(ctx)

	a.Use(module.Version{Path: "dep", Version: "1.0.0"})

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
	a := New(nil)
	if got := a.OutputDir(); got != "build" {
		t.Fatalf("default OutputDir = %q, want %q", got, "build")
	}
	a.InstallDir("custom-install")
	if got := a.OutputDir(); got != "custom-install" {
		t.Fatalf("OutputDir after InstallDir = %q, want %q", got, "custom-install")
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
	sourceDir := filepath.Join("testdata", "project")
	absSourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		t.Fatalf("abs source dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(absSourceDir, "configure")); err != nil {
		t.Fatalf("missing configure: %v", err)
	}

	t.Setenv("CUSTOM", "")

	a := New(nil)
	defer os.RemoveAll(a.buildDir)
	a.Env("CUSTOM", "VAL")
	a.Source(absSourceDir)
	a.InstallDir(installDir)

	if err := a.Configure("--enable-foo"); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if err := a.Build(); err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := a.Install(); err != nil {
		t.Fatalf("install: %v", err)
	}

	cache := filepath.Join(a.buildDir, "config.log")
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
