package build

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/goplus/llar/formula"
)

func TestSaveAndLoadBuildCache(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFilePath := filepath.Join(tmpDir, cacheFile)

	now := time.Now().Truncate(time.Second)
	cache := &buildCache{
		BuildResult: formula.BuildResult{
			Dir: "/tmp/output",
		},
		BuildTime: now,
	}

	if err := saveBuildCache(cacheFilePath, cache); err != nil {
		t.Fatalf("saveBuildCache failed: %v", err)
	}

	loaded, err := loadBuildCache(cacheFilePath)
	if err != nil {
		t.Fatalf("loadBuildCache failed: %v", err)
	}

	if loaded.BuildResult.Dir != cache.BuildResult.Dir {
		t.Errorf("Dir mismatch: got %q, want %q", loaded.BuildResult.Dir, cache.BuildResult.Dir)
	}
	if !loaded.BuildTime.Truncate(time.Second).Equal(now) {
		t.Errorf("BuildTime mismatch: got %v, want %v", loaded.BuildTime, now)
	}
}

func TestLoadBuildCache_NotExist(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFilePath := filepath.Join(tmpDir, "not_exist.json")

	_, err := loadBuildCache(cacheFilePath)
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
}

func TestLoadBuildCache_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFilePath := filepath.Join(tmpDir, cacheFile)

	if err := os.WriteFile(cacheFilePath, []byte("invalid json"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := loadBuildCache(cacheFilePath)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
