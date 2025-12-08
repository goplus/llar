package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/goplus/ixgo"
	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/loader"
	"github.com/goplus/llar/internal/repo"
	"github.com/goplus/llar/pkgs/mod/module"
)

type Engine struct {
	ctx    *ixgo.Context
	loader loader.Loader
}

func NewEngine() *Engine {
	ctx := ixgo.NewContext(ixgo.SupportMultipleInterp)
	return &Engine{ctx: ctx, loader: loader.NewFormulaLoader(ctx)}
}

func (e *Engine) Exec(task *Task) error {

}

type Task struct {
	tempDir string
	proj    *formula.Project
	main    module.Version
}

func remoteRepoOf(mod module.Version) string {
	// TODO(MeteorsLiu): Support different code hosted website.
	return "https://github.com/" + mod.ID
}

func matchRef(refs []string, version string) (ref string, ok bool) {
	for _, r := range refs {
		if strings.HasSuffix(r, version) {
			return r, true
		}
	}
	return "", false
}

func NewTask(main module.Version) (*Task, error) {
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("llar-build-%s-%s*", main.ID, main.Version))
	if err != nil {
		return nil, err
	}
	return &Task{
		main:    main,
		tempDir: tempDir,
		proj:    &formula.Project{},
	}, nil
}

func (e *Task) Close() error {
	return os.RemoveAll(e.tempDir)
}

func (e *Task) Resolve(c *buildContext) error {
	vcs := repo.NewGitVCS()
	refs, err := vcs.Tags(context.TODO(), remoteRepoOf(e.main))
	if err != nil {
		return err
	}
	ref, ok := matchRef(refs, e.main.Version)
	if !ok {
		return fmt.Errorf("failed to resolve version: cannot find a ref from version: %s", e.main.Version)
	}
	err = vcs.Sync(context.TODO(), remoteRepoOf(e.main), ref, e.tempDir)
	if err != nil {
		return err
	}

}

func (e *Task) Build() error {

}
