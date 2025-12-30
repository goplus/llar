// Copyright 2024 The llar Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vcs

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// RepoFS provides a filesystem view of a repository at a specific ref.
type RepoFS struct {
	repo     *Repo
	ref      string
	localDir string
}

// Open opens the named file for reading (lazy loading).
func (r *RepoFS) Open(name string) (fs.File, error) {
	return &repoFile{
		name:   name,
		local:  filepath.Join(r.localDir, name),
		client: r.repo.client,
		owner:  r.repo.owner,
		repo:   r.repo.repo,
		ref:    r.ref,
	}, nil
}

// ReadFile reads the content of a file.
func (r *RepoFS) ReadFile(name string) ([]byte, error) {
	f, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// Stat returns file info for the given path.
func (r *RepoFS) Stat(name string) (fs.FileInfo, error) {
	f, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

// ReadDir reads the contents of a directory.
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

// Sync downloads the specified path to localDir.
func (r *RepoFS) Sync(ctx context.Context, path string) error {
	local := filepath.Join(r.localDir, path)
	return r.repo.client.SyncDir(ctx, r.repo.owner, r.repo.repo, r.ref, path, local)
}

// repoFile implements fs.File with lazy loading.
type repoFile struct {
	name   string
	local  string
	client client
	owner  string
	repo   string
	ref    string

	once   sync.Once
	reader *bytes.Reader
	err    error
}

func (f *repoFile) load() {
	// Try local first
	if data, err := os.ReadFile(f.local); err == nil {
		f.reader = bytes.NewReader(data)
		return
	}

	// Fetch from remote
	ctx := context.Background()
	data, err := f.client.ReadFile(ctx, f.owner, f.repo, f.ref, f.name)
	if err != nil {
		f.err = err
		return
	}

	// Save to local
	if err := os.MkdirAll(filepath.Dir(f.local), 0755); err != nil {
		f.err = err
		return
	}
	if err := os.WriteFile(f.local, data, 0644); err != nil {
		f.err = err
		return
	}

	f.reader = bytes.NewReader(data)
}

func (f *repoFile) Read(p []byte) (int, error) {
	f.once.Do(f.load)
	if f.err != nil {
		return 0, f.err
	}
	return f.reader.Read(p)
}

func (f *repoFile) Stat() (fs.FileInfo, error) {
	// Try local first
	if info, err := os.Stat(f.local); err == nil {
		return info, nil
	}

	// Fetch from remote
	ctx := context.Background()
	return f.client.Stat(ctx, f.owner, f.repo, f.ref, f.name)
}

func (f *repoFile) Close() error { return nil }
