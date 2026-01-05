package modules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goplus/llar/pkgs/mod/module"
)

func TestModuleDirOf(t *testing.T) {
	tests := []struct {
		name    string
		modId   string
		wantErr bool
	}{
		{
			name:    "valid module id",
			modId:   "owner/repo",
			wantErr: false,
		},
		{
			name:    "nested module id",
			modId:   "github.com/owner/repo",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, err := moduleDirOf(tt.modId)
			if (err != nil) != tt.wantErr {
				t.Errorf("moduleDirOf(%q) error = %v, wantErr %v", tt.modId, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			// Verify the path contains the escaped module ID
			escapedModId, _ := module.EscapeID(tt.modId)
			if !strings.HasSuffix(dir, escapedModId) {
				t.Errorf("moduleDirOf(%q) = %q, should contain escaped mod ID %q", tt.modId, dir, escapedModId)
			}
		})
	}
}

func TestSourceCacheDirOf(t *testing.T) {
	// Skip if FormulaDir is not configured
	mod := module.Version{ID: "test/pkg", Version: "v1.0.0"}

	dir, err := sourceCacheDirOf(mod)
	if err != nil {
		t.Skipf("Skipping test (FormulaDir not configured): %v", err)
	}

	// Verify the directory was created
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("sourceCacheDirOf created dir but stat failed: %v", err)
	}

	if !info.IsDir() {
		t.Error("sourceCacheDirOf should create a directory")
	}

	// Verify path structure contains .source and version
	if !strings.Contains(dir, ".source") {
		t.Errorf("dir %q should contain '.source'", dir)
	}
	if !strings.Contains(dir, mod.Version) {
		t.Errorf("dir %q should contain version %q", dir, mod.Version)
	}

	// Cleanup
	os.RemoveAll(filepath.Dir(filepath.Dir(dir))) // Remove .source parent
}

func TestSourceCacheDirOf_CreatesDirectory(t *testing.T) {
	mod := module.Version{ID: "test/newpkg", Version: "v2.0.0"}

	dir, err := sourceCacheDirOf(mod)
	if err != nil {
		t.Skipf("Skipping test (FormulaDir not configured): %v", err)
	}

	// Directory should exist after call
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("sourceCacheDirOf should create the directory, but it doesn't exist")
	}

	// Cleanup
	os.RemoveAll(filepath.Dir(filepath.Dir(dir)))
}

func TestSourceCacheDirOf_DifferentVersions(t *testing.T) {
	modV1 := module.Version{ID: "test/multipkg", Version: "v1.0.0"}
	modV2 := module.Version{ID: "test/multipkg", Version: "v2.0.0"}

	dirV1, err := sourceCacheDirOf(modV1)
	if err != nil {
		t.Skipf("Skipping test: %v", err)
	}

	dirV2, err := sourceCacheDirOf(modV2)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	// Directories should be different for different versions
	if dirV1 == dirV2 {
		t.Errorf("different versions should have different cache dirs, got same: %s", dirV1)
	}

	// Both should contain their respective versions
	if !strings.Contains(dirV1, "v1.0.0") {
		t.Errorf("dirV1 %q should contain v1.0.0", dirV1)
	}
	if !strings.Contains(dirV2, "v2.0.0") {
		t.Errorf("dirV2 %q should contain v2.0.0", dirV2)
	}

	// Cleanup
	os.RemoveAll(filepath.Dir(filepath.Dir(dirV1)))
}
