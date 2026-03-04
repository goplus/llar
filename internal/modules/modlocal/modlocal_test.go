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

func TestResolve_WildcardUnsupported(t *testing.T) {
	tmp := t.TempDir()
	invalid := []string{"...", "owner/...", "owner/...@v1.0.0"}
	for _, pattern := range invalid {
		t.Run(pattern, func(t *testing.T) {
			_, err := Resolve(tmp, pattern)
			if err == nil {
				t.Fatalf("Resolve(%q) expected error", pattern)
			}
		})
	}
}

func TestResolve_ParentRefUnsupported(t *testing.T) {
	tmp := t.TempDir()
	invalid := []string{"..", "../repo", "owner/../repo"}
	for _, pattern := range invalid {
		t.Run(pattern, func(t *testing.T) {
			_, err := Resolve(tmp, pattern)
			if err == nil {
				t.Fatalf("Resolve(%q) expected error", pattern)
			}
		})
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
