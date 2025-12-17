package vcs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// VCS defines the interface for version control operations.
type VCS interface {
	// Sync ensures the local repo exists and is at the specified ref.
	// ref can be branch, tag, or commit hash.
	// If dir doesn't exist, clones the repo.
	// If dir exists, fetches updates and checks out the ref.
	Sync(ctx context.Context, remote, ref, dir string) error

	// Tags returns all tags from the remote repository.
	Tags(ctx context.Context, remote string) ([]string, error)

	// Latest returns the latest commit hash (HEAD) from the remote repository.
	// Returns error if no commits exist.
	Latest(ctx context.Context, remote string) (string, error)
}

// gitVCS implements VCS using git.
type gitVCS struct {
	git string
}

// GitOption configures gitVCS.
type GitOption func(*gitVCS)

// WithGitPath sets a custom git executable path.
func WithGitPath(path string) GitOption {
	return func(g *gitVCS) {
		g.git = path
	}
}

// NewGitVCS creates a new git VCS instance.
func NewGitVCS(opts ...GitOption) VCS {
	g := &gitVCS{git: "git"}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

func (g *gitVCS) ensureInit(ctx context.Context, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		return g.run(ctx, dir, "init")
	}
	return nil
}

func (g *gitVCS) Sync(ctx context.Context, remote, ref, dir string) error {
	if err := g.ensureInit(ctx, dir); err != nil {
		return err
	}
	if err := g.fetch(ctx, remote, dir, ref); err != nil {
		return err
	}
	return g.checkout(ctx, dir, "FETCH_HEAD")
}

func (g *gitVCS) fetch(ctx context.Context, remote, dir, ref string) error {
	args := []string{"fetch", "--depth", "1", remote, ref}
	if err := g.run(ctx, dir, args...); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	return nil
}

func (g *gitVCS) checkout(ctx context.Context, dir, ref string) error {
	if err := g.run(ctx, dir, "checkout", ref); err != nil {
		return fmt.Errorf("checkout %s: %w", ref, err)
	}
	return nil
}

func (g *gitVCS) Tags(ctx context.Context, remote string) ([]string, error) {
	output, err := g.output(ctx, "", "ls-remote", "--tags", "--refs", remote)
	if err != nil {
		return nil, fmt.Errorf("list remote tags: %w", err)
	}

	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}

	var tags []string
	for _, line := range strings.Split(output, "\n") {
		// format: <hash>\trefs/tags/<tag>
		parts := strings.Split(line, "\t")
		if len(parts) == 2 {
			tag := strings.TrimPrefix(parts[1], "refs/tags/")
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

func (g *gitVCS) Latest(ctx context.Context, remote string) (string, error) {
	output, err := g.output(ctx, "", "ls-remote", remote, "HEAD")
	if err != nil {
		return "", fmt.Errorf("get remote HEAD: %w", err)
	}

	output = strings.TrimSpace(output)
	if output == "" {
		return "", fmt.Errorf("no HEAD found in remote %s", remote)
	}

	// format: <hash>\tHEAD
	parts := strings.Split(output, "\t")
	if len(parts) < 1 {
		return "", fmt.Errorf("invalid ls-remote output")
	}
	return parts[0], nil
}

func (g *gitVCS) run(ctx context.Context, dir string, args ...string) error {
	_, err := g.output(ctx, dir, args...)
	return err
}

func (g *gitVCS) output(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, g.git, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
		return "", err
	}
	return stdout.String(), nil
}
