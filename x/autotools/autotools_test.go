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
	tmpDir := t.TempDir()

	// Build the expected directory: workspace/dep-amd64-linux/{include,lib/pkgconfig}
	matrixStr := "amd64-linux"
	modBuildDir := filepath.Join(tmpDir, "dep-"+matrixStr)
	includeDir := filepath.Join(modBuildDir, "include")
	libDir := filepath.Join(modBuildDir, "lib")
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

	matrix := formula.Matrix{
		Require: map[string][]string{"arch": {"amd64"}, "os": {"linux"}},
	}
	a := New(matrix, tmpDir, "", "", "")

	if err := a.Use(module.Version{Path: "dep", Version: "1.0.0"}); err != nil {
		t.Fatalf("Use: %v", err)
	}

	for key, want := range map[string]string{
		"PKG_CONFIG_PATH":    pkgconfigDir,
		"CMAKE_PREFIX_PATH":  modBuildDir,
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

func TestUseNotFound(t *testing.T) {
	matrix := formula.Matrix{
		Require: map[string][]string{"arch": {"amd64"}, "os": {"linux"}},
	}
	a := New(matrix, t.TempDir(), "", "", "")
	if err := a.Use(module.Version{Path: "no/such", Version: "1.0.0"}); err == nil {
		t.Fatal("expected error for missing dependency dir")
	}
}

func TestOutputDir(t *testing.T) {
	a := New(formula.Matrix{}, "", "", "build", "")
	if got := a.OutputDir(); got != "build" {
		t.Errorf("OutputDir = %q, want %q", got, "build")
	}
	a2 := New(formula.Matrix{}, "", "", "build", "inst")
	if got := a2.OutputDir(); got != "inst" {
		t.Errorf("OutputDir = %q, want %q", got, "inst")
	}
}

func TestMergeEnv(t *testing.T) {
	base := []string{"A=1", "B=2", "C=3"}
	got := mergeEnv(base, map[string]string{"B": "X", "D": "4"})

	m := make(map[string]string)
	for _, kv := range got {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	for key, want := range map[string]string{"A": "1", "B": "X", "C": "3", "D": "4"} {
		if m[key] != want {
			t.Errorf("%s = %q, want %q", key, m[key], want)
		}
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

	absSource, err := filepath.Abs(filepath.Join("testdata", "project"))
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("CUSTOM", "")

	a := New(formula.Matrix{}, "", absSource, buildDir, installDir)
	a.Env("CUSTOM", "VAL")

	if err := a.Configure("--enable-foo"); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if err := a.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := a.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Verify config.log captured our env and prefix.
	data, err := os.ReadFile(filepath.Join(buildDir, "config.log"))
	if err != nil {
		t.Fatalf("read config.log: %v", err)
	}
	log := string(data)
	for _, want := range []string{"CUSTOM=VAL", "PREFIX=" + installDir} {
		if !strings.Contains(log, want) {
			t.Errorf("config.log missing %q", want)
		}
	}

	// Verify installed artifacts.
	for _, path := range []string{
		filepath.Join(installDir, "lib", "libdummy.a"),
		filepath.Join(installDir, "include", "dummy.h"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s", path)
		}
	}
}
