package vcs

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitVCS_Tags(t *testing.T) {
	vcs := NewGitVCS()
	ctx := context.Background()

	tags, err := vcs.Tags(ctx, "https://github.com/golang/go")
	if err != nil {
		t.Fatalf("Tags failed: %v", err)
	}

	if len(tags) == 0 {
		t.Fatal("expected at least one tag")
	}

	// Check that go1.21.0 exists (a known tag)
	found := false
	for _, tag := range tags {
		if tag == "go1.21.0" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find tag go1.21.0")
	}
}

func TestGitVCS_Latest(t *testing.T) {
	vcs := NewGitVCS()
	ctx := context.Background()

	hash, err := vcs.Latest(ctx, "https://github.com/golang/go")
	if err != nil {
		t.Fatalf("Latest failed: %v", err)
	}

	if len(hash) != 40 {
		t.Errorf("expected 40-char hash, got %d chars: %s", len(hash), hash)
	}
}

func TestGitVCS_Sync(t *testing.T) {
	vcs := NewGitVCS()
	ctx := context.Background()

	dir := filepath.Join(t.TempDir(), "test-repo")

	// Clone with a specific tag
	err := vcs.Sync(ctx, "https://github.com/google/uuid", "v1.3.0", dir)
	if err != nil {
		t.Fatalf("Sync (clone) failed: %v", err)
	}

	// Verify we're at v1.3.0
	hash1 := getHeadCommit(t, dir)
	if hash1 == "" {
		t.Fatal("failed to get HEAD after clone")
	}

	// Sync again with different tag
	err = vcs.Sync(ctx, "https://github.com/google/uuid", "v1.4.0", dir)
	if err != nil {
		t.Fatalf("Sync (update) failed: %v", err)
	}

	// Verify HEAD changed
	hash2 := getHeadCommit(t, dir)
	if hash2 == "" {
		t.Fatal("failed to get HEAD after update")
	}
	if hash1 == hash2 {
		t.Errorf("HEAD should have changed after switching tags, got %s both times", hash1)
	}
}

func getHeadCommit(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD failed: %v", err)
	}
	return strings.TrimSpace(string(out))
}
