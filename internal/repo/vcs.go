package repo

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

type VCS interface {
	Sync(remote, branch, dir string) error
}

type gitVCS struct{}

func NewGitVCS() VCS {
	return &gitVCS{}
}

func (g *gitVCS) Sync(remote, branch, dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return gitClone(remote, branch, dir)
	}
	return gitPull(remote, branch, dir)
}

func gitClone(remote, branch, dir string) error {
	var buf bytes.Buffer
	cmd := exec.Command("git", "clone", "-b", branch, remote, dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = &buf
	err := cmd.Run()

	if err != nil {
		return fmt.Errorf("failed to clone project: %s", buf.String())
	}
	return nil
}

func gitPull(remote, branch, dir string) error {
	var buf bytes.Buffer
	cmd := exec.Command("git", "pull", remote, branch)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = &buf
	err := cmd.Run()

	if err != nil {
		return fmt.Errorf("failed to clone project: %s", buf.String())
	}
	return nil
}
