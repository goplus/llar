package build

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/goplus/llar/formula"
)

func TestCacheKey(t *testing.T) {
	tests := []struct {
		version, matrix, want string
	}{
		{"1.0.0", "amd64-linux", "1.0.0-amd64-linux"},
		{"2.0.0", "arm64-darwin", "2.0.0-arm64-darwin"},
		{"1.0.0", "amd64-linux|openssl", "1.0.0-amd64-linux|openssl"},
	}
	for _, tt := range tests {
		if got := cacheKey(tt.version, tt.matrix); got != tt.want {
			t.Errorf("cacheKey(%q, %q) = %q, want %q", tt.version, tt.matrix, got, tt.want)
		}
	}
}

func TestBuildCache_GetSet(t *testing.T) {
	c := &buildCache{}

	// get from empty cache
	if _, ok := c.get("1.0.0", "amd64-linux"); ok {
		t.Fatal("get from empty cache should return false")
	}

	// set and get
	entry := &buildEntry{
		BuildTime: time.Now(),
	}
	c.set("1.0.0", "amd64-linux", entry)

	got, ok := c.get("1.0.0", "amd64-linux")
	if !ok {
		t.Fatal("get after set should return true")
	}
	if got != entry {
		t.Error("get returned different entry")
	}

	// different matrix miss
	if _, ok := c.get("1.0.0", "arm64-linux"); ok {
		t.Error("different matrix should miss")
	}

	// different version miss
	if _, ok := c.get("2.0.0", "amd64-linux"); ok {
		t.Error("different version should miss")
	}
}

func TestBuildCache_MultipleEntries(t *testing.T) {
	c := &buildCache{}

	e1 := &buildEntry{BuildTime: time.Now()}
	e2 := &buildEntry{BuildTime: time.Now().Add(time.Hour)}
	e3 := &buildEntry{BuildTime: time.Now().Add(2 * time.Hour)}

	c.set("1.0.0", "amd64-linux", e1)
	c.set("1.0.0", "arm64-linux", e2)
	c.set("2.0.0", "amd64-linux", e3)

	if got, _ := c.get("1.0.0", "amd64-linux"); got != e1 {
		t.Error("wrong entry for 1.0.0-amd64-linux")
	}
	if got, _ := c.get("1.0.0", "arm64-linux"); got != e2 {
		t.Error("wrong entry for 1.0.0-arm64-linux")
	}
	if got, _ := c.get("2.0.0", "amd64-linux"); got != e3 {
		t.Error("wrong entry for 2.0.0-amd64-linux")
	}
}

func TestBuildCache_Overwrite(t *testing.T) {
	c := &buildCache{}

	old := &buildEntry{BuildTime: time.Now()}
	c.set("1.0.0", "amd64-linux", old)

	updated := &buildEntry{BuildTime: time.Now().Add(time.Hour)}
	c.set("1.0.0", "amd64-linux", updated)

	got, _ := c.get("1.0.0", "amd64-linux")
	if got != updated {
		t.Error("overwrite did not replace entry")
	}
	if len(c.Cache) != 1 {
		t.Errorf("cache size = %d, want 1", len(c.Cache))
	}
}

func TestBuildWorkspace_Dir(t *testing.T) {
	w := newBuildWorkspace("/tmp/ws")

	dir, err := w.Dir("madler/zlib", "1.0.0", "amd64-linux")
	if err != nil {
		t.Fatalf("Dir() failed: %v", err)
	}
	want := filepath.Join("/tmp/ws", "madler", "zlib", "1.0.0", "amd64-linux")
	if dir != want {
		t.Errorf("Dir() = %q, want %q", dir, want)
	}
}

func TestBuildWorkspace_Dir_InvalidPath(t *testing.T) {
	w := newBuildWorkspace("/tmp/ws")
	_, err := w.Dir("", "1.0.0", "amd64-linux")
	if err == nil {
		t.Error("Dir() expected error for empty module path")
	}
}

func TestBuildWorkspace_Has(t *testing.T) {
	tmpDir := t.TempDir()
	w := newBuildWorkspace(tmpDir)

	if w.Has("madler/zlib", "1.0.0", "amd64-linux") {
		t.Error("Has() = true for non-existent")
	}

	// create the directory
	dir, _ := w.Dir("madler/zlib", "1.0.0", "amd64-linux")
	os.MkdirAll(dir, 0700)

	if !w.Has("madler/zlib", "1.0.0", "amd64-linux") {
		t.Error("Has() = false for existing directory")
	}
}

func TestBuildWorkspace_FS(t *testing.T) {
	tmpDir := t.TempDir()
	w := newBuildWorkspace(tmpDir)

	// create output with a file
	dir, _ := w.Dir("madler/zlib", "1.0.0", "amd64-linux")
	os.MkdirAll(filepath.Join(dir, "lib"), 0700)
	os.WriteFile(filepath.Join(dir, "lib", "libz.a"), []byte("archive"), 0600)

	fsys, err := w.FS("madler/zlib", "1.0.0", "amd64-linux")
	if err != nil {
		t.Fatalf("FS() failed: %v", err)
	}

	f, err := fsys.Open("lib/libz.a")
	if err != nil {
		t.Fatalf("failed to open file from FS: %v", err)
	}
	f.Close()
}

func TestBuildWorkspace_SaveLoadCache(t *testing.T) {
	tmpDir := t.TempDir()
	w := newBuildWorkspace(tmpDir)
	now := time.Now().Truncate(time.Second)

	result := formula.BuildResult{}
	result.SetMetadata("-lssl")

	original := &buildCache{}
	original.set("1.0.0", "amd64-linux", &buildEntry{
		BuildResult: result,
		BuildTime:   now,
	})
	original.set("2.0.0", "arm64-darwin", &buildEntry{
		BuildTime: now.Add(time.Hour),
	})

	if err := w.saveCache("madler/zlib", original); err != nil {
		t.Fatalf("saveCache() failed: %v", err)
	}

	t.Run("hit", func(t *testing.T) {
		cache, err := w.loadCache("madler/zlib")
		if err != nil {
			t.Fatalf("loadCache() failed: %v", err)
		}
		entry, ok := cache.get("1.0.0", "amd64-linux")
		if !ok {
			t.Fatal("expected entry, got miss")
		}
		if entry.BuildResult.Metadata() != "-lssl" {
			t.Errorf("metadata = %q, want %q", entry.BuildResult.Metadata(), "-lssl")
		}
		if !entry.BuildTime.Equal(now) {
			t.Errorf("build time = %v, want %v", entry.BuildTime, now)
		}
	})

	t.Run("miss", func(t *testing.T) {
		cache, err := w.loadCache("madler/zlib")
		if err != nil {
			t.Fatalf("loadCache() failed: %v", err)
		}
		_, ok := cache.get("9.9.9", "amd64-linux")
		if ok {
			t.Error("expected miss for nonexistent key")
		}
	})

	t.Run("no cache file", func(t *testing.T) {
		_, err := w.loadCache("nonexistent/mod")
		if err == nil {
			t.Fatal("expected error for missing cache file")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		badDir, _ := w.modDir("bad/json")
		os.MkdirAll(badDir, 0700)
		os.WriteFile(filepath.Join(badDir, cacheFile), []byte("bad"), 0644)
		_, err := w.loadCache("bad/json")
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}
