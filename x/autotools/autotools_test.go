package autotools

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

	a := New("", "", "")
	a.Use(root)

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
	// root with only include, no lib or pkgconfig
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "include"), 0o755)

	for _, key := range []string{
		"PKG_CONFIG_PATH", "CMAKE_LIBRARY_PATH", "CPPFLAGS", "LDFLAGS",
	} {
		t.Setenv(key, "")
	}

	a := New("", "", "")
	a.Use(root)

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

	a := New(absSource, buildDir, installDir)

	if err := a.Configure("--enable-foo"); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if err := a.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := a.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(buildDir, "config.log"))
	if err != nil {
		t.Fatalf("read config.log: %v", err)
	}
	if log := string(data); !strings.Contains(log, "PREFIX="+installDir) {
		t.Errorf("config.log missing PREFIX=%s", installDir)
	}

	for _, path := range []string{
		filepath.Join(installDir, "lib", "libdummy.a"),
		filepath.Join(installDir, "include", "dummy.h"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s", path)
		}
	}
}
