// Copyright 2024 The llar Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vcs

import (
	"context"
	"io/fs"
)

// mockClient implements client interface for unit testing.
type mockClient struct {
	tagsFunc    func(ctx context.Context, owner, repo string) ([]string, error)
	latestFunc  func(ctx context.Context, owner, repo string) (string, error)
	statFunc    func(ctx context.Context, owner, repo, ref, path string) (fs.FileInfo, error)
	readFunc    func(ctx context.Context, owner, repo, ref, path string) ([]byte, error)
	syncDirFunc func(ctx context.Context, owner, repo, ref, path, destDir string) error
}

func (m *mockClient) Tags(ctx context.Context, owner, repo string) ([]string, error) {
	if m.tagsFunc != nil {
		return m.tagsFunc(ctx, owner, repo)
	}
	return nil, nil
}

func (m *mockClient) Latest(ctx context.Context, owner, repo string) (string, error) {
	if m.latestFunc != nil {
		return m.latestFunc(ctx, owner, repo)
	}
	return "", nil
}

func (m *mockClient) Stat(ctx context.Context, owner, repo, ref, path string) (fs.FileInfo, error) {
	if m.statFunc != nil {
		return m.statFunc(ctx, owner, repo, ref, path)
	}
	return nil, nil
}

func (m *mockClient) ReadFile(ctx context.Context, owner, repo, ref, path string) ([]byte, error) {
	if m.readFunc != nil {
		return m.readFunc(ctx, owner, repo, ref, path)
	}
	return nil, nil
}

func (m *mockClient) SyncDir(ctx context.Context, owner, repo, ref, path, destDir string) error {
	if m.syncDirFunc != nil {
		return m.syncDirFunc(ctx, owner, repo, ref, path, destDir)
	}
	return nil
}
