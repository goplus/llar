// Copyright 2024 The llar Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vcs

import (
	"context"
	"fmt"
	"strings"
)

// Repo represents a code hosting repository.
// It provides version queries and can create filesystem views for specific refs.
type Repo struct {
	client client
	host   string
	owner  string
	repo   string
}

// NewRepo creates a new Repo for the given repository path.
// repoPath format: "github.com/owner/repo"
func NewRepo(repoPath string) (*Repo, error) {
	host, owner, repo, err := parseRepoPath(repoPath)
	if err != nil {
		return nil, err
	}

	c, err := newClient(host)
	if err != nil {
		return nil, err
	}

	return &Repo{
		client: c,
		host:   host,
		owner:  owner,
		repo:   repo,
	}, nil
}

// Tags returns all tags from the repository.
func (r *Repo) Tags(ctx context.Context) ([]string, error) {
	return r.client.Tags(ctx, r.owner, r.repo)
}

// Latest returns the latest commit hash on the default branch.
func (r *Repo) Latest(ctx context.Context) (string, error) {
	return r.client.Latest(ctx, r.owner, r.repo)
}

// At returns a filesystem view of the repository at the specified ref.
// ref can be a tag, branch, or commit hash.
// localDir is where the files will be stored locally.
func (r *Repo) At(ref, localDir string) *RepoFS {
	return &RepoFS{
		repo:     r,
		ref:      ref,
		localDir: localDir,
	}
}

// parseRepoPath parses "github.com/owner/repo" into components.
func parseRepoPath(repoPath string) (host, owner, repo string, err error) {
	parts := strings.Split(repoPath, "/")
	if len(parts) < 3 {
		return "", "", "", fmt.Errorf("invalid repo path: %s, expected host/owner/repo", repoPath)
	}
	return parts[0], parts[1], strings.Join(parts[2:], "/"), nil
}
