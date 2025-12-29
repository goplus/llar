// Copyright 2024 The llar Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vcs

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// githubClient implements client interface using raw GitHub URLs (no API).
type githubClient struct {
	httpClient *http.Client
}

// newGitHubClient creates a new GitHub client.
func newGitHubClient() *githubClient {
	return &githubClient{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Tags returns all tags from the repository using git ls-remote.
func (g *githubClient) Tags(ctx context.Context, owner, repo string) ([]string, error) {
	repoURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)

	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--tags", "--refs", repoURL)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-remote: %w", err)
	}

	var tags []string
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Format: <sha>\trefs/tags/<tagname>
		parts := strings.Split(line, "\t")
		if len(parts) != 2 {
			continue
		}
		ref := parts[1]
		tag := strings.TrimPrefix(ref, "refs/tags/")
		tags = append(tags, tag)
	}

	return tags, nil
}

// Latest returns the latest commit hash on the default branch using git ls-remote.
func (g *githubClient) Latest(ctx context.Context, owner, repo string) (string, error) {
	repoURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)

	cmd := exec.CommandContext(ctx, "git", "ls-remote", repoURL, "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote: %w", err)
	}

	line := strings.TrimSpace(string(output))
	if line == "" {
		return "", fmt.Errorf("no HEAD ref found")
	}

	// Format: <sha>\tHEAD
	parts := strings.Split(line, "\t")
	if len(parts) < 1 {
		return "", fmt.Errorf("invalid ls-remote output: %s", line)
	}

	return parts[0], nil
}

// Stat returns file info for the given path using HEAD request.
func (g *githubClient) Stat(ctx context.Context, owner, repo, ref, path string) (fs.FileInfo, error) {
	// Try as file first
	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, ref, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		// It's a file
		size := resp.ContentLength
		if size < 0 {
			size = 0
		}
		return &fileInfo{
			name:  filepath.Base(path),
			size:  size,
			mode:  0644,
			isDir: false,
		}, nil
	}

	if resp.StatusCode == http.StatusNotFound {
		// Could be a directory - we can't easily check this without API
		// Return as directory if it looks like one (no extension)
		if filepath.Ext(path) == "" {
			return &fileInfo{
				name:  filepath.Base(path),
				mode:  fs.ModeDir | 0755,
				isDir: true,
			}, nil
		}
		return nil, fmt.Errorf("path not found: %s", path)
	}

	return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
}

// ReadFile reads the content of a file using raw.githubusercontent.com.
func (g *githubClient) ReadFile(ctx context.Context, owner, repo, ref, path string) ([]byte, error) {
	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, ref, path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("file not found: %s", path)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, path)
	}

	return io.ReadAll(resp.Body)
}

// ReadDir reads the contents of a directory by downloading the tarball.
// Note: This downloads the entire repo tarball - use SyncDir for efficiency.
func (g *githubClient) ReadDir(ctx context.Context, owner, repo, ref, path string) ([]fs.DirEntry, error) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "llar-readdir-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	// Download and extract the tarball
	if err := g.SyncDir(ctx, owner, repo, ref, path, tmpDir); err != nil {
		return nil, err
	}

	// Read the directory
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil, err
	}

	return entries, nil
}

// SyncDir downloads a directory to the destination directory using tarball.
func (g *githubClient) SyncDir(ctx context.Context, owner, repo, ref, path, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	// Download tarball
	url := fmt.Sprintf("https://github.com/%s/%s/archive/%s.tar.gz", owner, repo, ref)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download tarball: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download tarball: status %d", resp.StatusCode)
	}

	// Decompress gzip
	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gzr.Close()

	// Extract tar
	tr := tar.NewReader(gzr)

	// The tarball has a root directory like "repo-ref/"
	// We need to find and strip this prefix
	var rootPrefix string

	// Normalize path for matching
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	if path == "." {
		path = ""
	}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		// Skip PAX extended headers (GitHub tarballs have pax_global_header as first entry)
		if header.Typeflag == tar.TypeXGlobalHeader || header.Typeflag == tar.TypeXHeader {
			continue
		}

		// Get the root prefix from the first real entry (should be a directory)
		if rootPrefix == "" {
			parts := strings.SplitN(header.Name, "/", 2)
			if len(parts) > 0 {
				rootPrefix = parts[0] + "/"
			}
		}

		// Strip the root prefix
		name := strings.TrimPrefix(header.Name, rootPrefix)
		if name == "" {
			continue
		}

		// Check if this entry is under our target path
		var relPath string
		if path == "" {
			relPath = name
		} else if strings.HasPrefix(name, path+"/") {
			relPath = strings.TrimPrefix(name, path+"/")
		} else if name == path {
			// Exact match (probably a file)
			relPath = filepath.Base(name)
		} else {
			// Not under our target path
			continue
		}

		if relPath == "" {
			continue
		}

		target := filepath.Join(destDir, relPath)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil && !os.IsExist(err) {
				return err
			}
		}
	}

	return nil
}

// fileInfo implements fs.FileInfo.
type fileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	isDir   bool
}

func (f *fileInfo) Name() string       { return f.name }
func (f *fileInfo) Size() int64        { return f.size }
func (f *fileInfo) Mode() fs.FileMode  { return f.mode }
func (f *fileInfo) ModTime() time.Time { return f.modTime }
func (f *fileInfo) IsDir() bool        { return f.isDir }
func (f *fileInfo) Sys() any           { return nil }

// dirEntry implements fs.DirEntry.
type dirEntry struct {
	info *fileInfo
}

func (d *dirEntry) Name() string               { return d.info.Name() }
func (d *dirEntry) IsDir() bool                { return d.info.IsDir() }
func (d *dirEntry) Type() fs.FileMode          { return d.info.Mode().Type() }
func (d *dirEntry) Info() (fs.FileInfo, error) { return d.info, nil }
