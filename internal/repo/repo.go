package repo

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
)

// Repo manages a remote formula repository.
type Repo struct {
	vcs      VCS
	remote   string
	localDir string
}

// NewRepo creates a new Repo instance.
func NewRepo(vcs VCS, remote, localDir string) *Repo {
	return &Repo{
		vcs:      vcs,
		remote:   remote,
		localDir: localDir,
	}
}

// Sync synchronizes the repository to the specified ref (branch, tag, or commit).
// If ref is empty, syncs to the default branch.
func (r *Repo) Sync(ctx context.Context, ref string) error {
	return r.vcs.Sync(ctx, r.remote, ref, r.localDir)
}

// ModulePath returns the local filesystem path for a module.
func (r *Repo) ModulePath(moduleID string) string {
	return filepath.Join(r.localDir, moduleID)
}

// ModuleFS returns a fs.FS for the given module.
func (r *Repo) ModuleFS(moduleID string) fs.FS {
	return os.DirFS(r.ModulePath(moduleID))
}
