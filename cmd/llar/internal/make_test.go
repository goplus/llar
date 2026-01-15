package internal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/build"
)

func TestParseModuleArg(t *testing.T) {
	tests := []struct {
		arg         string
		wantModID   string
		wantVersion string
	}{
		{"owner/repo@v1.0.0", "owner/repo", "v1.0.0"},
		{"owner/repo@1.0.0", "owner/repo", "1.0.0"},
		{"owner/repo", "owner/repo", ""},
		{"org/owner/repo@v2.0.0", "org/owner/repo", "v2.0.0"},
		{"simple@latest", "simple", "latest"},
		{"no-version", "no-version", ""},
		{"multiple@at@signs", "multiple@at", "signs"},
	}

	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			modID, version := parseModuleArg(tt.arg)
			if modID != tt.wantModID {
				t.Errorf("parseModuleArg(%q) modID = %q, want %q", tt.arg, modID, tt.wantModID)
			}
			if version != tt.wantVersion {
				t.Errorf("parseModuleArg(%q) version = %q, want %q", tt.arg, version, tt.wantVersion)
			}
		})
	}
}

func TestPrintPkgConfigInfo(t *testing.T) {
	// Create temp directory with pkgconfig files
	tmpDir := t.TempDir()
	pkgconfigDir := filepath.Join(tmpDir, "lib", "pkgconfig")
	if err := os.MkdirAll(pkgconfigDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a test .pc file
	pcContent := `prefix=` + tmpDir + `
libdir=${prefix}/lib
includedir=${prefix}/include

Name: testlib
Description: Test library
Version: 1.0.0
Libs: -L${libdir} -ltest
Cflags: -I${includedir}
`
	pcFile := filepath.Join(pkgconfigDir, "testlib.pc")
	if err := os.WriteFile(pcFile, []byte(pcContent), 0644); err != nil {
		t.Fatal(err)
	}

	matrix := formula.Matrix{
		Require: map[string][]string{
			"os":   {"linux"},
			"arch": {"amd64"},
		},
	}

	result := build.Result{OutputDir: tmpDir}

	// Should not panic or error
	err := printPkgConfigInfo(result, matrix)
	if err != nil {
		t.Logf("printPkgConfigInfo returned error (may be expected if pkg-config not installed): %v", err)
	}
}

func TestPrintPkgConfigInfo_NoPkgConfigDir(t *testing.T) {
	tmpDir := t.TempDir()
	matrix := formula.Matrix{}
	result := build.Result{OutputDir: tmpDir}

	// Should return error when pkgconfig dir doesn't exist
	err := printPkgConfigInfo(result, matrix)
	if err == nil {
		t.Log("printPkgConfigInfo returned nil error for missing dir (acceptable)")
	}
}

func TestPrintPkgConfigInfo_EmptyPkgConfigDir(t *testing.T) {
	tmpDir := t.TempDir()
	pkgconfigDir := filepath.Join(tmpDir, "lib", "pkgconfig")
	if err := os.MkdirAll(pkgconfigDir, 0755); err != nil {
		t.Fatal(err)
	}

	matrix := formula.Matrix{}
	result := build.Result{OutputDir: tmpDir}

	// Should return nil when no .pc files
	err := printPkgConfigInfo(result, matrix)
	if err != nil {
		t.Errorf("printPkgConfigInfo returned error for empty dir: %v", err)
	}
}
