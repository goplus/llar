// Copyright 2024 The llar Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vcs

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
)

// RepoFS provides a filesystem view of a repository at a specific ref.
type RepoFS struct {
	repo     *Repo
	ref      string
	localDir string
}

// ReadFile reads the content of a file.
// It first checks the local directory, then fetches from remote if not found.
func (r *RepoFS) ReadFile(name string) ([]byte, error) {
	// Try local first
	local := filepath.Join(r.localDir, name)
	if data, err := os.ReadFile(local); err == nil {
		return data, nil
	}

	// Fetch from remote
	ctx := context.Background()
	data, err := r.repo.client.ReadFile(ctx, r.repo.owner, r.repo.repo, r.ref, name)
	if err != nil {
		return nil, err
	}

	// Save to local
	if err := r.saveToLocal(name, data); err != nil {
		return nil, err
	}

	return data, nil
}

// ReadDir reads the contents of a directory.
// It downloads the entire directory if not present locally.
func (r *RepoFS) ReadDir(name string) ([]fs.DirEntry, error) {
	local := filepath.Join(r.localDir, name)

	// Local directory exists and is not empty, read directly
	if entries, err := os.ReadDir(local); err == nil && len(entries) > 0 {
		return entries, nil
	}

	// Download the entire directory
	ctx := context.Background()
	if err := r.repo.client.SyncDir(ctx, r.repo.owner, r.repo.repo, r.ref, name, local); err != nil {
		return nil, err
	}

	return os.ReadDir(local)
}

// Stat returns file info for the given path.
func (r *RepoFS) Stat(name string) (fs.FileInfo, error) {
	// Try local first
	local := filepath.Join(r.localDir, name)
	if info, err := os.Stat(local); err == nil {
		return info, nil
	}

	// Fetch from remote
	ctx := context.Background()
	return r.repo.client.Stat(ctx, r.repo.owner, r.repo.repo, r.ref, name)
}

// Sync downloads the specified path to localDir.
// If path is ".", it syncs the entire repository at the ref.
func (r *RepoFS) Sync(ctx context.Context, path string) error {
	local := filepath.Join(r.localDir, path)
	return r.repo.client.SyncDir(ctx, r.repo.owner, r.repo.repo, r.ref, path, local)
}

// saveToLocal saves data to the local directory.
func (r *RepoFS) saveToLocal(name string, data []byte) error {
	local := filepath.Join(r.localDir, name)
	dir := filepath.Dir(local)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(local, data, 0644)
}
