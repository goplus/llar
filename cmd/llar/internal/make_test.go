package internal

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestParseModuleArg(t *testing.T) {
	tests := []struct {
		arg         string
		wantModPath string
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
			modPath, version := parseModuleArg(tt.arg)
			if modPath != tt.wantModPath {
				t.Errorf("parseModuleArg(%q) modPath = %q, want %q", tt.arg, modPath, tt.wantModPath)
			}
			if version != tt.wantVersion {
				t.Errorf("parseModuleArg(%q) version = %q, want %q", tt.arg, version, tt.wantVersion)
			}
		})
	}
}

func setupTestSrcDir(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "lib"), 0755)
	os.WriteFile(filepath.Join(src, "lib", "libfoo.a"), []byte("archive"), 0644)
	os.MkdirAll(filepath.Join(src, "include"), 0755)
	os.WriteFile(filepath.Join(src, "include", "foo.h"), []byte("#pragma once"), 0644)
	return src
}

func TestOutputResult_CopyDir(t *testing.T) {
	src := setupTestSrcDir(t)
	dest := filepath.Join(t.TempDir(), "out")

	if err := outputResult(src, dest); err != nil {
		t.Fatalf("outputResult copy: %v", err)
	}

	// Verify files exist
	for _, rel := range []string{"lib/libfoo.a", "include/foo.h"} {
		path := filepath.Join(dest, rel)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}

	// Verify content
	data, err := os.ReadFile(filepath.Join(dest, "lib", "libfoo.a"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "archive" {
		t.Errorf("content = %q, want %q", data, "archive")
	}
}

func TestOutputResult_Zip(t *testing.T) {
	src := setupTestSrcDir(t)
	dest := filepath.Join(t.TempDir(), "out.zip")

	if err := outputResult(src, dest); err != nil {
		t.Fatalf("outputResult zip: %v", err)
	}

	// Open and verify zip contents
	r, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()

	want := map[string]bool{
		"lib/libfoo.a":  false,
		"include/foo.h": false,
	}
	for _, f := range r.File {
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("zip missing %s", name)
		}
	}
}
