package modlocal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeVersionsJSON creates a versions.json with the given path field.
func writeVersionsJSON(t *testing.T, dir, modPath string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	data, _ := json.Marshal(map[string]any{"path": modPath})
	if err := os.WriteFile(filepath.Join(dir, "versions.json"), data, 0644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}
}

func TestResolve_CurrentDir(t *testing.T) {
	tmp := t.TempDir()
	writeVersionsJSON(t, tmp, "madler/zlib")

	mods, err := Resolve(tmp, "")
	if err != nil {
		t.Fatalf("Resolve(cwd, \"\") failed: %v", err)
	}
	if len(mods) != 1 {
		t.Fatalf("got %d modules, want 1", len(mods))
	}
	if mods[0].Path != "madler/zlib" {
		t.Errorf("path = %q, want %q", mods[0].Path, "madler/zlib")
	}
	if mods[0].Dir != tmp {
		t.Errorf("dir = %q, want %q", mods[0].Dir, tmp)
	}
}

func TestResolve_CurrentDir_WalkUp(t *testing.T) {
	tmp := t.TempDir()
	writeVersionsJSON(t, tmp, "madler/zlib")

	// Create a subdirectory and resolve from there
	subdir := filepath.Join(tmp, "sub", "deep")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	mods, err := Resolve(subdir, "")
	if err != nil {
		t.Fatalf("Resolve from subdir failed: %v", err)
	}
	if len(mods) != 1 {
		t.Fatalf("got %d modules, want 1", len(mods))
	}
	if mods[0].Path != "madler/zlib" {
		t.Errorf("path = %q, want %q", mods[0].Path, "madler/zlib")
	}
	if mods[0].Dir != tmp {
		t.Errorf("dir = %q, want %q", mods[0].Dir, tmp)
	}
}

func TestResolve_CurrentDir_NoVersionsJSON(t *testing.T) {
	tmp := t.TempDir()
	_, err := Resolve(tmp, "")
	if err == nil {
		t.Fatal("expected error when no versions.json exists")
	}
}

func TestResolve_CurrentDir_EmptyPath(t *testing.T) {
	tmp := t.TempDir()
	// versions.json with empty path field
	data, _ := json.Marshal(map[string]any{"path": ""})
	os.WriteFile(filepath.Join(tmp, "versions.json"), data, 0644)

	_, err := Resolve(tmp, "")
	if err == nil {
		t.Fatal("expected error when path field is empty")
	}
}

func TestResolve_SingleLocal(t *testing.T) {
	tmp := t.TempDir()
	modDir := filepath.Join(tmp, "madler", "zlib")
	writeVersionsJSON(t, modDir, "madler/zlib")

	mods, err := Resolve(tmp, "madler/zlib")
	if err != nil {
		t.Fatalf("Resolve single local failed: %v", err)
	}
	if len(mods) != 1 {
		t.Fatalf("got %d modules, want 1", len(mods))
	}
	if mods[0].Path != "madler/zlib" {
		t.Errorf("path = %q, want %q", mods[0].Path, "madler/zlib")
	}
	if mods[0].Dir != modDir {
		t.Errorf("dir = %q, want %q", mods[0].Dir, modDir)
	}
}

func TestResolve_SingleLocal_NotFound(t *testing.T) {
	tmp := t.TempDir()
	_, err := Resolve(tmp, "nonexistent/repo")
	if err == nil {
		t.Fatal("expected error for nonexistent module")
	}
}

func TestResolve_OwnerWildcard(t *testing.T) {
	tmp := t.TempDir()
	writeVersionsJSON(t, filepath.Join(tmp, "madler", "zlib"), "madler/zlib")
	writeVersionsJSON(t, filepath.Join(tmp, "madler", "brotli"), "madler/brotli")

	mods, err := Resolve(tmp, "madler/...")
	if err != nil {
		t.Fatalf("Resolve owner wildcard failed: %v", err)
	}
	if len(mods) != 2 {
		t.Fatalf("got %d modules, want 2", len(mods))
	}

	paths := map[string]bool{}
	for _, m := range mods {
		paths[m.Path] = true
	}
	if !paths["madler/zlib"] {
		t.Error("missing madler/zlib")
	}
	if !paths["madler/brotli"] {
		t.Error("missing madler/brotli")
	}
}

func TestResolve_AllWildcard(t *testing.T) {
	tmp := t.TempDir()
	writeVersionsJSON(t, filepath.Join(tmp, "madler", "zlib"), "madler/zlib")
	writeVersionsJSON(t, filepath.Join(tmp, "pnggroup", "libpng"), "pnggroup/libpng")

	mods, err := Resolve(tmp, "...")
	if err != nil {
		t.Fatalf("Resolve all wildcard failed: %v", err)
	}
	if len(mods) != 2 {
		t.Fatalf("got %d modules, want 2", len(mods))
	}

	paths := map[string]bool{}
	for _, m := range mods {
		paths[m.Path] = true
	}
	if !paths["madler/zlib"] {
		t.Error("missing madler/zlib")
	}
	if !paths["pnggroup/libpng"] {
		t.Error("missing pnggroup/libpng")
	}
}

func TestResolve_Wildcard_SkipsDotAndUnderscore(t *testing.T) {
	tmp := t.TempDir()
	writeVersionsJSON(t, filepath.Join(tmp, "madler", "zlib"), "madler/zlib")
	writeVersionsJSON(t, filepath.Join(tmp, "madler", ".hidden"), "madler/.hidden")
	writeVersionsJSON(t, filepath.Join(tmp, "madler", "_internal"), "madler/_internal")

	mods, err := Resolve(tmp, "madler/...")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if len(mods) != 1 {
		t.Fatalf("got %d modules, want 1 (should skip . and _ dirs)", len(mods))
	}
	if mods[0].Path != "madler/zlib" {
		t.Errorf("path = %q, want %q", mods[0].Path, "madler/zlib")
	}
}

func TestResolve_AllWildcard_SkipsDotOwner(t *testing.T) {
	tmp := t.TempDir()
	writeVersionsJSON(t, filepath.Join(tmp, "madler", "zlib"), "madler/zlib")
	writeVersionsJSON(t, filepath.Join(tmp, ".hidden", "repo"), ".hidden/repo")
	writeVersionsJSON(t, filepath.Join(tmp, "_internal", "repo"), "_internal/repo")

	mods, err := Resolve(tmp, "...")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if len(mods) != 1 {
		t.Fatalf("got %d modules, want 1 (should skip . and _ owner dirs)", len(mods))
	}
	if mods[0].Path != "madler/zlib" {
		t.Errorf("path = %q, want %q", mods[0].Path, "madler/zlib")
	}
}

func TestResolve_Wildcard_NoMatches(t *testing.T) {
	tmp := t.TempDir()
	_, err := Resolve(tmp, "nonexistent/...")
	if err == nil {
		t.Fatal("expected error when no modules match")
	}
}

func TestResolve_Wildcard_SkipsEmptyPath(t *testing.T) {
	tmp := t.TempDir()
	writeVersionsJSON(t, filepath.Join(tmp, "owner", "good"), "owner/good")
	// versions.json with empty path
	dir := filepath.Join(tmp, "owner", "empty")
	os.MkdirAll(dir, 0755)
	data, _ := json.Marshal(map[string]any{"path": ""})
	os.WriteFile(filepath.Join(dir, "versions.json"), data, 0644)

	mods, err := Resolve(tmp, "owner/...")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if len(mods) != 1 {
		t.Fatalf("got %d modules, want 1 (should skip empty path)", len(mods))
	}
	if mods[0].Path != "owner/good" {
		t.Errorf("path = %q, want %q", mods[0].Path, "owner/good")
	}
}

func TestResolve_CurrentDir_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "versions.json"), []byte("{invalid"), 0644)

	_, err := Resolve(tmp, "")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestResolve_SingleLocal_EmptyPath(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "owner", "repo")
	os.MkdirAll(dir, 0755)
	data, _ := json.Marshal(map[string]any{"path": ""})
	os.WriteFile(filepath.Join(dir, "versions.json"), data, 0644)

	_, err := Resolve(tmp, "owner/repo")
	if err == nil {
		t.Fatal("expected error for empty path field")
	}
}

func TestResolve_Wildcard_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "owner", "bad")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "versions.json"), []byte("{invalid"), 0644)

	_, err := Resolve(tmp, "owner/...")
	if err == nil {
		t.Fatal("expected error for invalid JSON in wildcard")
	}
}

func TestResolve_Wildcard_AllSkipped(t *testing.T) {
	tmp := t.TempDir()
	// Only dot/underscore dirs — all get skipped
	writeVersionsJSON(t, filepath.Join(tmp, "owner", ".hidden"), "owner/.hidden")
	writeVersionsJSON(t, filepath.Join(tmp, "owner", "_internal"), "owner/_internal")

	_, err := Resolve(tmp, "owner/...")
	if err == nil {
		t.Fatal("expected error when all modules are skipped")
	}
}
