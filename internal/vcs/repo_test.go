// Copyright 2024 The llar Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vcs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRepoPath(t *testing.T) {
	tests := []struct {
		input       string
		wantHost    string
		wantOwner   string
		wantRepo    string
		wantErr     bool
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
	repo, err := NewRepo("github.com/golang/go")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}
	if repo.host != "github.com" || repo.owner != "golang" || repo.repo != "go" {
		t.Errorf("NewRepo parsed incorrectly: got host=%q owner=%q repo=%q",
			repo.host, repo.owner, repo.repo)
	}
}

func TestRepoAt(t *testing.T) {
	repo, err := NewRepo("github.com/golang/go")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	localDir := t.TempDir()
	repoFS := repo.At("v1.21.0", localDir)

	if repoFS.ref != "v1.21.0" {
		t.Errorf("RepoFS.ref = %q, want %q", repoFS.ref, "v1.21.0")
	}
	if repoFS.localDir != localDir {
		t.Errorf("RepoFS.localDir = %q, want %q", repoFS.localDir, localDir)
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

	repo, err := NewRepo("github.com/google/go-github")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	localDir := t.TempDir()
	repoFS := repo.At("v68.0.0", localDir)

	// Read README.md
	data, err := repoFS.ReadFile("README.md")
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
	data2, err := repoFS.ReadFile("README.md")
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

	repo, err := NewRepo("github.com/google/go-github")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	localDir := t.TempDir()
	repoFS := repo.At("v68.0.0", localDir)

	// Read root directory
	entries, err := repoFS.ReadDir(".")
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

func TestRepoFSSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo, err := NewRepo("github.com/google/go-github")
	if err != nil {
		t.Fatalf("NewRepo failed: %v", err)
	}

	localDir := t.TempDir()
	repoFS := repo.At("v68.0.0", localDir)

	// Sync only the .github directory (smaller)
	ctx := context.Background()
	if err := repoFS.Sync(ctx, ".github"); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Check directory was synced
	syncedDir := filepath.Join(localDir, ".github")
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
