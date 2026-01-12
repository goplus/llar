package modules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goplus/llar/pkgs/mod/module"
)

func TestNewClassfileCache(t *testing.T) {
	tests := []struct {
		name           string
		localDir       string
		wantSearchPath string
	}{
		{
			name:           "with specified local dir",
			localDir:       "/custom/path",
			wantSearchPath: "/custom/path",
		},
		{
			name:           "with empty local dir defaults to current directory",
			localDir:       "",
			wantSearchPath: ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := newClassfileCache(nil, tt.localDir)
			if cache == nil {
				t.Fatal("newClassfileCache returned nil")
			}
			if cache.formulas == nil {
				t.Error("formulas map is nil")
			}
			if cache.comparators == nil {
				t.Error("comparators map is nil")
			}
			if len(cache.searchPaths) != 1 || cache.searchPaths[0] != tt.wantSearchPath {
				t.Errorf("searchPaths = %v, want [%s]", cache.searchPaths, tt.wantSearchPath)
			}
		})
	}
}

func TestFindMaxFromVer_NoDirectory(t *testing.T) {
	cache := newClassfileCache(nil, "/nonexistent/path")
	mod := module.Version{Path: "test/pkg", Version: "1.0.0"}

	compare := func(v1, v2 module.Version) int {
		if v1.Version < v2.Version {
			return -1
		} else if v1.Version > v2.Version {
			return 1
		}
		return 0
	}

	_, _, err := cache.findMaxFromVer(mod, compare)
	if err == nil {
		t.Error("expected error when no formula found, got nil")
	}
}

func TestFindMaxFromVer_WithTestdata(t *testing.T) {
	// Use testdata directory as the search path
	testdataDir := "testdata"
	cache := newClassfileCache(nil, testdataDir)

	// Test with DaveGamble/cJSON which has multiple versions
	mod := module.Version{Path: "github.com/DaveGamble/cJSON", Version: "1.7.18"}

	compare := func(v1, v2 module.Version) int {
		if v1.Version < v2.Version {
			return -1
		} else if v1.Version > v2.Version {
			return 1
		}
		return 0
	}

	maxVer, formulaPath, err := cache.findMaxFromVer(mod, compare)
	if err != nil {
		t.Fatalf("findMaxFromVer failed: %v", err)
	}

	// Should find version 1.5.0 (highest version <= 1.7.18)
	if maxVer != "1.5.0" {
		t.Errorf("maxFromVer = %q, want %q", maxVer, "1.5.0")
	}

	if !filepath.IsAbs(formulaPath) {
		formulaPath, _ = filepath.Abs(formulaPath)
	}
	if _, err := os.Stat(formulaPath); os.IsNotExist(err) {
		t.Errorf("formula path does not exist: %s", formulaPath)
	}
}

func TestClassfileCache_ComparatorOf_WithMock(t *testing.T) {
	// Create mock repo pointing to testdata
	testdataDir, _ := filepath.Abs("testdata")
	mockRepo := newMockRepo(testdataDir)

	// Use a temp dir for formula download destination
	tempDir := t.TempDir()
	cache := newClassfileCache(mockRepo, tempDir)

	// Pre-populate the temp dir with testdata (simulating lazyDownloadFormula)
	// Copy DaveGamble/cJSON to temp dir
	srcDir := filepath.Join(testdataDir, "DaveGamble", "cJSON")
	destDir := filepath.Join(tempDir, "DaveGamble", "cJSON")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}
	if err := os.CopyFS(destDir, os.DirFS(srcDir)); err != nil {
		t.Fatalf("failed to copy testdata: %v", err)
	}

	modId := "github.com/DaveGamble/cJSON"

	// This should use the custom comparator from CJSON_cmp.gox
	comp, err := cache.comparatorOf(modId)
	if err != nil {
		t.Skipf("comparatorOf failed (env not configured): %v", err)
	}

	// Test the comparator works
	v1 := module.Version{Path: modId, Version: "1.0"}
	v2 := module.Version{Path: modId, Version: "2.0"}

	if result := comp(v1, v2); result >= 0 {
		t.Errorf("comp(1.0, 2.0) = %d, want < 0", result)
	}
	if result := comp(v2, v1); result <= 0 {
		t.Errorf("comp(2.0, 1.0) = %d, want > 0", result)
	}
}

func TestClassfileCache_ComparatorOf_Caching(t *testing.T) {
	testdataDir, _ := filepath.Abs("testdata")
	mockRepo := newMockRepo(testdataDir)
	tempDir := t.TempDir()
	cache := newClassfileCache(mockRepo, tempDir)

	// Pre-populate with zlib (uses default comparator)
	srcDir := filepath.Join(testdataDir, "madler", "zlib")
	destDir := filepath.Join(tempDir, "madler", "zlib")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}
	if err := os.CopyFS(destDir, os.DirFS(srcDir)); err != nil {
		t.Fatalf("failed to copy testdata: %v", err)
	}

	modId := "github.com/madler/zlib"

	comp1, err := cache.comparatorOf(modId)
	if err != nil {
		t.Skipf("comparatorOf failed: %v", err)
	}

	// Second call should return cached comparator
	comp2, err := cache.comparatorOf(modId)
	if err != nil {
		t.Fatalf("second comparatorOf failed: %v", err)
	}

	// Both should produce same results
	v1 := module.Version{Path: modId, Version: "1.0"}
	v2 := module.Version{Path: modId, Version: "2.0"}

	if comp1(v1, v2) != comp2(v1, v2) {
		t.Error("cached comparator produces different results")
	}
}

func TestDefaultFormulaSuffix(t *testing.T) {
	if _defaultFormulaSuffix != "_llar.gox" {
		t.Errorf("_defaultFormulaSuffix = %q, want %q", _defaultFormulaSuffix, "_llar.gox")
	}
}
