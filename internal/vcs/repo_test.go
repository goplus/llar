// Copyright 2024 The llar Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vcs

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// Unit tests with mock client (no network required)

func TestFileInfo(t *testing.T) {
	now := time.Now()
	fi := &fileInfo{
		name:    "test.txt",
		size:    42,
		mode:    0644,
		modTime: now,
		isDir:   false,
	}

	if fi.Name() != "test.txt" {
		t.Errorf("Name() = %q, want %q", fi.Name(), "test.txt")
	}
	if fi.Size() != 42 {
		t.Errorf("Size() = %d, want 42", fi.Size())
	}
	if fi.Mode() != 0644 {
		t.Errorf("Mode() = %v, want 0644", fi.Mode())
	}
	if !fi.ModTime().Equal(now) {
		t.Errorf("ModTime() = %v, want %v", fi.ModTime(), now)
	}
	if fi.IsDir() {
		t.Error("IsDir() = true, want false")
	}
	if fi.Sys() != nil {
		t.Errorf("Sys() = %v, want nil", fi.Sys())
	}

	// Test directory
	dir := &fileInfo{name: "dir", isDir: true}
	if !dir.IsDir() {
		t.Error("IsDir() = false, want true")
	}
}

func TestNewRepo_InvalidPath(t *testing.T) {
	// Too few parts
	_, err := NewRepo("x")
	if err == nil {
		t.Error("NewRepo(\"x\") should return error")
	}

	_, err = NewRepo("only/two")
	if err == nil {
		t.Error("NewRepo(\"only/two\") should return error")
	}
}

func newMockRepo(mc *mockClient) *repo {
	return &repo{
		client: mc,
		host:   "mock.com",
		owner:  "owner",
		name:   "repo",
	}
}

func TestRepoFSReadFile_LoadError(t *testing.T) {
	mc := &mockClient{
		readFunc: func(ctx context.Context, owner, repo, ref, path string) ([]byte, error) {
			return nil, fmt.Errorf("remote read error")
		},
	}
	r := newMockRepo(mc)

	localDir := t.TempDir()
	fsys := r.At("v1.0", localDir).(fs.ReadFileFS)

	_, err := fsys.ReadFile("missing.txt")
	if err == nil {
		t.Error("ReadFile should fail when remote returns error")
	}
	if err.Error() != "remote read error" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRepoFSReadFile_LoadMkdirError(t *testing.T) {
	mc := &mockClient{
		readFunc: func(ctx context.Context, owner, repo, ref, path string) ([]byte, error) {
			return []byte("data"), nil
		},
	}
	r := newMockRepo(mc)

	// Use a path where MkdirAll will fail (file exists where dir is expected)
	localDir := t.TempDir()
	blockingFile := filepath.Join(localDir, "sub")
	os.WriteFile(blockingFile, []byte("block"), 0644)

	fsys := r.At("v1.0", localDir)
	rfs := fsys.(*repoFS)

	f, _ := rfs.Open("sub/file.txt")
	_, err := io.ReadAll(f)
	if err == nil {
		t.Error("ReadFile should fail when MkdirAll fails")
	}
}

func TestRepoFSReadFile_LoadWriteError(t *testing.T) {
	mc := &mockClient{
		readFunc: func(ctx context.Context, owner, repo, ref, path string) ([]byte, error) {
			return []byte("data"), nil
		},
	}
	r := newMockRepo(mc)

	// Use a read-only directory to trigger WriteFile error
	localDir := t.TempDir()
	os.Chmod(localDir, 0555)
	defer os.Chmod(localDir, 0755) // restore for cleanup

	fsys := r.At("v1.0", localDir)
	rfs := fsys.(*repoFS)

	f, _ := rfs.Open("file.txt")
	_, err := io.ReadAll(f)
	if err == nil {
		t.Error("ReadFile should fail when WriteFile fails")
	}
}

func TestRepoFSReadDir_RemoteFallback(t *testing.T) {
	syncCalled := false
	mc := &mockClient{
		syncDirFunc: func(ctx context.Context, owner, repo, ref, path, destDir string) error {
			syncCalled = true
			// Create a file in destDir to simulate successful sync
			os.MkdirAll(destDir, 0755)
			os.WriteFile(filepath.Join(destDir, "synced.txt"), []byte("ok"), 0644)
			return nil
		},
	}
	r := newMockRepo(mc)

	localDir := t.TempDir()
	fsys := r.At("v1.0", localDir).(fs.ReadDirFS)

	entries, err := fsys.ReadDir("subdir")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if !syncCalled {
		t.Error("SyncDir should be called when local dir is empty")
	}
	if len(entries) != 1 || entries[0].Name() != "synced.txt" {
		t.Errorf("unexpected entries: %v", entries)
	}
}

func TestRepoFSReadDir_SyncError(t *testing.T) {
	mc := &mockClient{
		syncDirFunc: func(ctx context.Context, owner, repo, ref, path, destDir string) error {
			return fmt.Errorf("sync failed")
		},
	}
	r := newMockRepo(mc)

	localDir := t.TempDir()
	fsys := r.At("v1.0", localDir).(fs.ReadDirFS)

	_, err := fsys.ReadDir("missing")
	if err == nil {
		t.Error("ReadDir should fail when SyncDir fails")
	}
}

func TestRepoFSOpen_StatRemote(t *testing.T) {
	mc := &mockClient{
		statFunc: func(ctx context.Context, owner, repo, ref, path string) (fs.FileInfo, error) {
			return &fileInfo{name: "remote.txt", size: 100, mode: 0644}, nil
		},
	}
	r := newMockRepo(mc)

	localDir := t.TempDir()
	fsys := r.At("v1.0", localDir)

	f, err := fsys.Open("remote.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	// Stat before Read — no local file, goes to remote
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Name() != "remote.txt" {
		t.Errorf("Stat Name = %q, want %q", info.Name(), "remote.txt")
	}
	if info.Size() != 100 {
		t.Errorf("Stat Size = %d, want 100", info.Size())
	}
}

func TestRepoFSOpen_StatRemoteError(t *testing.T) {
	mc := &mockClient{
		statFunc: func(ctx context.Context, owner, repo, ref, path string) (fs.FileInfo, error) {
			return nil, fmt.Errorf("stat error")
		},
	}
	r := newMockRepo(mc)

	localDir := t.TempDir()
	fsys := r.At("v1.0", localDir)

	f, _ := fsys.Open("nope.txt")
	defer f.Close()

	_, err := f.Stat()
	if err == nil {
		t.Error("Stat should fail when remote Stat fails")
	}
}

func TestRepoTags_Mock(t *testing.T) {
	mc := &mockClient{
		tagsFunc: func(ctx context.Context, owner, repo string) ([]string, error) {
			return []string{"v1.0", "v2.0"}, nil
		},
	}
	r := newMockRepo(mc)

	tags, err := r.Tags(context.Background())
	if err != nil {
		t.Fatalf("Tags failed: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(tags))
	}
}

func TestRepoLatest_Mock(t *testing.T) {
	mc := &mockClient{
		latestFunc: func(ctx context.Context, owner, repo string) (string, error) {
			return "abc123", nil
		},
	}
	r := newMockRepo(mc)

	latest, err := r.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest failed: %v", err)
	}
	if latest != "abc123" {
		t.Errorf("Latest = %q, want %q", latest, "abc123")
	}
}

func TestRepoSync_Mock(t *testing.T) {
	syncCalled := false
	mc := &mockClient{
		syncDirFunc: func(ctx context.Context, owner, repo, ref, path, destDir string) error {
			syncCalled = true
			return nil
		},
	}
	r := newMockRepo(mc)

	err := r.Sync(context.Background(), "v1.0", "path", "/tmp/test")
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if !syncCalled {
		t.Error("SyncDir should be called")
	}
}

func TestRepoFSReadDir_LocalNotEmpty(t *testing.T) {
	mc := &mockClient{
		syncDirFunc: func(ctx context.Context, owner, repo, ref, path, destDir string) error {
			t.Error("SyncDir should not be called when local dir has entries")
			return nil
		},
	}
	r := newMockRepo(mc)

	localDir := t.TempDir()
	// Pre-populate local directory
	subDir := filepath.Join(localDir, "mydir")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "local.txt"), []byte("local"), 0644)

	fsys := r.At("v1.0", localDir).(fs.ReadDirFS)

	entries, err := fsys.ReadDir("mydir")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "local.txt" {
		t.Errorf("unexpected entries: %v", entries)
	}
}

func TestRepoFSOpen_ReadFromCache(t *testing.T) {
	mc := &mockClient{
		readFunc: func(ctx context.Context, owner, repo, ref, path string) ([]byte, error) {
			t.Error("ReadFile should not be called when file exists locally")
			return nil, nil
		},
	}
	r := newMockRepo(mc)

	localDir := t.TempDir()
	// Pre-populate cache
	os.WriteFile(filepath.Join(localDir, "cached.txt"), []byte("cached"), 0644)

	fsys := r.At("v1.0", localDir)
	f, _ := fsys.Open("cached.txt")
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(data) != "cached" {
		t.Errorf("Read = %q, want %q", string(data), "cached")
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
