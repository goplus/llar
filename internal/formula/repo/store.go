// Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package repo

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/mod/module"
)

// Store manages a formula repository, handling storage layout and synchronization.
// It provides access to module formulas through a filesystem abstraction.
type Store struct {
	dir     string
	vcsRepo vcs.Repo
}

// New creates a new Store with the given directory and vcs.Repo.
// The dir specifies where this formula repository is stored locally.
func New(dir string, vcsRepo vcs.Repo) *Store {
	return &Store{
		dir:     dir,
		vcsRepo: vcsRepo,
	}
}

// ModuleFS returns a filesystem interface for the specified module.
// It synchronizes the module from remote and returns an fs.FS rooted at the module's directory.
func (s *Store) ModuleFS(ctx context.Context, modPath string) (fs.FS, error) {
	modDir, err := s.moduleDirOf(modPath)
	if err != nil {
		return nil, err
	}

	// Sync to the repository root directory, not the module directory.
	// The vcs.Repo.Sync will create the module path structure within the destination.
	if err := s.vcsRepo.Sync(ctx, "", modPath, s.dir); err != nil {
		return nil, err
	}

	return os.DirFS(modDir), nil
}

// moduleDirOf returns the directory path for a module within the repository.
// It creates the directory with 0700 permissions if it doesn't exist.
func (s *Store) moduleDirOf(modPath string) (string, error) {
	escapedModPath, err := module.EscapePath(modPath)
	if err != nil {
		return "", err
	}
	moduleDir := filepath.Join(s.dir, escapedModPath)

	if err := os.MkdirAll(moduleDir, 0700); err != nil {
		return "", err
	}
	return moduleDir, nil
}

// DefaultDir returns the default root directory where all formula repositories are stored.
// It creates the directory with 0700 permissions if it doesn't exist.
// The directory is located at <UserCacheDir>/.llar/formulas.
func DefaultDir() (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	formulaDir := filepath.Join(userCacheDir, ".llar", "formulas")

	if err := os.MkdirAll(formulaDir, 0700); err != nil {
		return "", err
	}
	return formulaDir, nil
}
