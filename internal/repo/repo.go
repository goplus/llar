package repo

import (
	"path/filepath"
	"sync"
)

type Repo struct {
	vcs      VCS
	remote   string
	localDir string
	syncOnce sync.Once
}

func NewRepo(vcs VCS, remote, localDir string) *Repo {
	return &Repo{vcs: vcs, remote: remote, localDir: localDir}
}

func (r *Repo) lazyInit() error {
	var err error

	r.syncOnce.Do(func() {
		// FIXME(MeteorsLiu): allow non-main branch
		err = r.vcs.Sync(r.remote, "main", r.localDir)
	})

	return err
}

func (r *Repo) ModulePath(moduleID string) (string, error) {
	if err := r.lazyInit(); err != nil {
		return "", err
	}
	return filepath.Join(r.localDir, moduleID), nil
}
