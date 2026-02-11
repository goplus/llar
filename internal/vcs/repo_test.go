// Copyright 2024 The llar Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vcs

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRepoPath(t *testing.T) {
	tests := []struct {
		input     string
		wantHost  string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"github.com/owner/repo", "github.com", "owner", "repo", false},
		{"github.com/owner/repo/sub", "github.com", "owner", "repo/sub", false},
		{"gitlab.com/org/project", "gitlab.com", "org", "project", false},
		{"invalid", "", "", "", true},
		{"only/two", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			host, owner, repo, err := parseRepoPath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRepoPath(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if host != tt.wantHost || owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("parseRepoPath(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.input, host, owner, repo, tt.wantHost, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestNewRepo(t *testing.T) {
	// Test unsupported host
	_, err := NewRepo("unsupported.com/owner/repo")
	if err == nil {
		t.Error("NewRepo with unsupported host should return error")
	}

	// Test valid GitHub repo
	r, err := NewRepo("github.com/golang/go")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}
	rr := r.(*repo)
	if rr.host != "github.com" || rr.owner != "golang" || rr.name != "go" {
		t.Errorf("NewRepo parsed incorrectly: got host=%q owner=%q name=%q",
			rr.host, rr.owner, rr.name)
	}
}

func TestRepoAt(t *testing.T) {
	r, err := NewRepo("github.com/golang/go")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	localDir := t.TempDir()
	fsys := r.At("v1.21.0", localDir)
	rfs := fsys.(*repoFS)

	if rfs.ref != "v1.21.0" {
		t.Errorf("RepoFS.ref = %q, want %q", rfs.ref, "v1.21.0")
	}
	if rfs.localDir != localDir {
		t.Errorf("RepoFS.localDir = %q, want %q", rfs.localDir, localDir)
	}
}

// Integration tests (require network access)

func TestRepoTags(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo, err := NewRepo("github.com/google/go-github")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	ctx := context.Background()
	tags, err := repo.Tags(ctx)
	if err != nil {
		t.Fatalf("Tags failed: %v", err)
	}

	if len(tags) == 0 {
		t.Error("Tags returned empty list")
	}
}

func TestRepoLatest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo, err := NewRepo("github.com/google/go-github")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	ctx := context.Background()
	latest, err := repo.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest failed: %v", err)
	}

	if latest == "" {
		t.Error("Latest returned empty string")
	}
	t.Logf("Latest commit: %s", latest)
}

func TestRepoFSReadFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	r, err := NewRepo("github.com/google/go-github")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	localDir := t.TempDir()
	fsys := r.At("v68.0.0", localDir)
	rfs := fsys.(fs.ReadFileFS)

	// Read README.md
	data, err := rfs.ReadFile("README.md")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("ReadFile returned empty data")
	}

	// Check file is cached locally
	cached := filepath.Join(localDir, "README.md")
	if _, err := os.Stat(cached); os.IsNotExist(err) {
		t.Error("File was not cached locally")
	}

	// Second read should come from cache
	data2, err := rfs.ReadFile("README.md")
	if err != nil {
		t.Fatalf("ReadFile (cached) failed: %v", err)
	}

	if string(data) != string(data2) {
		t.Error("Cached data differs from original")
	}
}

func TestRepoFSReadDir(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	r, err := NewRepo("github.com/google/go-github")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	localDir := t.TempDir()
	fsys := r.At("v68.0.0", localDir)
	rfs := fsys.(fs.ReadDirFS)

	// Read root directory
	entries, err := rfs.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	if len(entries) == 0 {
		t.Error("ReadDir returned empty list")
	}

	// Check some expected files/dirs exist
	found := make(map[string]bool)
	for _, e := range entries {
		found[e.Name()] = true
	}

	if !found["README.md"] {
		t.Error("README.md not found in directory listing")
	}
	if !found["github"] {
		t.Error("github directory not found in directory listing")
	}
}

func TestRepoSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	r, err := NewRepo("github.com/google/go-github")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	localDir := t.TempDir()
	syncedDir := filepath.Join(localDir, ".github")

	// Sync only the .github directory (smaller)
	ctx := context.Background()
	if err := r.Sync(ctx, "v68.0.0", ".github", syncedDir); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Check directory was synced
	if _, err := os.Stat(syncedDir); os.IsNotExist(err) {
		t.Error(".github directory was not synced")
	}

	// Check some files exist in synced directory
	entries, err := os.ReadDir(syncedDir)
	if err != nil {
		t.Fatalf("ReadDir synced dir failed: %v", err)
	}

	if len(entries) == 0 {
		t.Error("Synced directory is empty")
	}
}

func TestRepoFSOpen(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo, err := NewRepo("github.com/google/go-github")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	localDir := t.TempDir()
	repoFS := repo.At("v68.0.0", localDir)

	// Open file (lazy loading)
	f, err := repoFS.Open("README.md")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	// File not downloaded yet (lazy)
	cached := filepath.Join(localDir, "README.md")
	if _, err := os.Stat(cached); !os.IsNotExist(err) {
		t.Error("File should not be cached before Read")
	}

	// Read triggers download
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("Read returned empty data")
	}

	// Now file should be cached
	if _, err := os.Stat(cached); os.IsNotExist(err) {
		t.Error("File should be cached after Read")
	}

	// Stat should work
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Name() != "README.md" {
		t.Errorf("Stat Name = %q, want %q", info.Name(), "README.md")
	}
}

func TestRepoFSOpenStatBeforeRead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo, err := NewRepo("github.com/google/go-github")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	localDir := t.TempDir()
	repoFS := repo.At("v68.0.0", localDir)

	f, err := repoFS.Open("README.md")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	// Stat before Read should fetch from remote
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Name() != "README.md" {
		t.Errorf("Stat Name = %q, want %q", info.Name(), "README.md")
	}

	// File still not cached (Stat doesn't download content)
	cached := filepath.Join(localDir, "README.md")
	if _, err := os.Stat(cached); !os.IsNotExist(err) {
		t.Error("File should not be cached after Stat only")
	}
}

func TestRepoFSOpenMultipleReads(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo, err := NewRepo("github.com/google/go-github")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	localDir := t.TempDir()
	repoFS := repo.At("v68.0.0", localDir)

	f, err := repoFS.Open("LICENSE")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	// First read
	buf1 := make([]byte, 10)
	n1, err := f.Read(buf1)
	if err != nil {
		t.Fatalf("First Read failed: %v", err)
	}

	// Second read continues from where we left off
	buf2 := make([]byte, 10)
	n2, err := f.Read(buf2)
	if err != nil {
		t.Fatalf("Second Read failed: %v", err)
	}

	// Should read different content
	if string(buf1[:n1]) == string(buf2[:n2]) && n1 == n2 {
		t.Error("Multiple reads should advance position")
	}
}

func TestRepoFSOpenNonExistent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo, err := NewRepo("github.com/google/go-github")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	localDir := t.TempDir()
	repoFS := repo.At("v68.0.0", localDir)

	// Open succeeds (lazy)
	f, err := repoFS.Open("nonexistent-file.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	// Read should fail for non-existent file
	_, err = io.ReadAll(f)
	if err == nil {
		t.Error("Read should fail for non-existent file")
	}
}

func TestRepoFSOpenFromCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo, err := NewRepo("github.com/google/go-github")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	localDir := t.TempDir()
	repoFS := repo.At("v68.0.0", localDir)

	// Pre-populate cache
	cached := filepath.Join(localDir, "cached.txt")
	cachedContent := []byte("cached content")
	if err := os.WriteFile(cached, cachedContent, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Open should read from cache, not remote
	f, err := repoFS.Open("cached.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if string(data) != string(cachedContent) {
		t.Errorf("Read = %q, want %q (from cache)", string(data), string(cachedContent))
	}
}

func TestGitHubClientSyncDirSparse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := newGitHubClient()
	ctx := context.Background()
	destDir := t.TempDir()

	// Test sparse-checkout for a subdirectory
	err := client.SyncDir(ctx, "google", "go-github", "v68.0.0", ".github", destDir)
	if err != nil {
		t.Fatalf("SyncDir sparse failed: %v", err)
	}

	// Check .git exists
	gitDir := filepath.Join(destDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Error(".git directory should exist")
	}

	// Read root directory entries (excluding .git)
	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	// Filter out .git directory
	var nonGitEntries []os.DirEntry
	for _, e := range entries {
		if e.Name() != ".git" {
			nonGitEntries = append(nonGitEntries, e)
		}
	}

	// Sparse checkout should only have the target directory (.github)
	if len(nonGitEntries) != 1 {
		var names []string
		for _, e := range nonGitEntries {
			names = append(names, e.Name())
		}
		t.Errorf("sparse-checkout should only contain target directory, got %d entries: %v", len(nonGitEntries), names)
	}

	if len(nonGitEntries) > 0 && nonGitEntries[0].Name() != ".github" {
		t.Errorf("expected .github directory, got %s", nonGitEntries[0].Name())
	}

	// Check .github directory has content
	githubDir := filepath.Join(destDir, ".github")
	githubEntries, err := os.ReadDir(githubDir)
	if err != nil {
		t.Fatalf("ReadDir .github failed: %v", err)
	}

	if len(githubEntries) == 0 {
		t.Error(".github directory should have content")
	}

	t.Logf("Sparse checkout: only .github with %d files", len(githubEntries))
}

func TestGitHubClientSyncDirShallowClone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := newGitHubClient()
	ctx := context.Background()
	destDir := t.TempDir()

	// Test shallow clone for root directory (empty path)
	err := client.SyncDir(ctx, "google", "go-github", "v68.0.0", "", destDir)
	if err != nil {
		t.Fatalf("SyncDir shallow clone failed: %v", err)
	}

	// Check README.md exists
	readme := filepath.Join(destDir, "README.md")
	if _, err := os.Stat(readme); os.IsNotExist(err) {
		t.Error("README.md should exist")
	}

	// Check .git exists
	gitDir := filepath.Join(destDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Error(".git directory should exist")
	}

	// Check github directory exists (full repo)
	githubDir := filepath.Join(destDir, "github")
	if _, err := os.Stat(githubDir); os.IsNotExist(err) {
		t.Error("github directory should exist in full clone")
	}
}

func TestGitHubClientSyncDirSparseUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := newGitHubClient()
	ctx := context.Background()
	destDir := t.TempDir()

	// First sync
	err := client.SyncDir(ctx, "google", "go-github", "v68.0.0", ".github", destDir)
	if err != nil {
		t.Fatalf("First SyncDir failed: %v", err)
	}

	// Second sync should succeed (update scenario)
	err = client.SyncDir(ctx, "google", "go-github", "v68.0.0", ".github", destDir)
	if err != nil {
		t.Fatalf("Second SyncDir (update) failed: %v", err)
	}

	// Verify content still exists
	githubDir := filepath.Join(destDir, ".github")
	entries, err := os.ReadDir(githubDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	if len(entries) == 0 {
		t.Error(".github directory should have content after update")
	}
}

func TestGitHubClientSyncDirShallowCloneUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := newGitHubClient()
	ctx := context.Background()
	destDir := t.TempDir()

	// First sync
	err := client.SyncDir(ctx, "google", "go-github", "v68.0.0", "", destDir)
	if err != nil {
		t.Fatalf("First SyncDir failed: %v", err)
	}

	// Second sync should succeed (update scenario)
	err = client.SyncDir(ctx, "google", "go-github", "v68.0.0", "", destDir)
	if err != nil {
		t.Fatalf("Second SyncDir (update) failed: %v", err)
	}

	// Verify content still exists
	readme := filepath.Join(destDir, "README.md")
	if _, err := os.Stat(readme); os.IsNotExist(err) {
		t.Error("README.md should exist after update")
	}
}

func TestGitHubClientSyncDirSparseMultiplePaths(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := newGitHubClient()
	ctx := context.Background()
	destDir := t.TempDir()

	// Sync first directory
	err := client.SyncDir(ctx, "google", "go-github", "v68.0.0", ".github", destDir)
	if err != nil {
		t.Fatalf("First SyncDir failed: %v", err)
	}

	// Verify .github exists
	githubDir := filepath.Join(destDir, ".github")
	if _, err := os.Stat(githubDir); os.IsNotExist(err) {
		t.Fatal(".github should exist after first sync")
	}

	// Sync second directory into the same destDir
	err = client.SyncDir(ctx, "google", "go-github", "v68.0.0", "github", destDir)
	if err != nil {
		t.Fatalf("Second SyncDir failed: %v", err)
	}

	// Both directories should exist
	if _, err := os.Stat(githubDir); os.IsNotExist(err) {
		t.Error(".github should still exist after second sync")
	}
	codeDir := filepath.Join(destDir, "github")
	if _, err := os.Stat(codeDir); os.IsNotExist(err) {
		t.Error("github should exist after second sync")
	}

	// Only the two target directories (plus .git) should be present
	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	var nonGitEntries []string
	for _, e := range entries {
		if e.Name() != ".git" {
			nonGitEntries = append(nonGitEntries, e.Name())
		}
	}
	if len(nonGitEntries) != 2 {
		t.Errorf("expected 2 directories, got %d: %v", len(nonGitEntries), nonGitEntries)
	}

	t.Logf("Sparse checkout with multiple paths: %v", nonGitEntries)
}
