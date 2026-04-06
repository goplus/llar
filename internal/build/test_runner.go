package build

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	classfile "github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/modules"
	"github.com/goplus/llar/mod/module"
)

type OnTestFailureError struct {
	Module module.Version
	Err    error
}

func (e *OnTestFailureError) Error() string {
	return fmt.Sprintf("onTest failed for %s@%s: %v", e.Module.Path, e.Module.Version, e.Err)
}

func (e *OnTestFailureError) Unwrap() error {
	return e.Err
}

// RunOnTest executes the main module's OnTest callback against an existing
// output tree. It reuses the builder's matrix and workspace to preserve
// dependency output-dir and cached metadata lookups.
func (b *Builder) RunOnTest(ctx context.Context, targets []*modules.Module, outputDir string) error {
	if len(targets) == 0 {
		return nil
	}
	mod := targets[0]
	if mod.OnTest == nil {
		return nil
	}

	tmpSourceDir, err := os.MkdirTemp("", fmt.Sprintf("source-%s-%s*", strings.ReplaceAll(mod.Path, "/", "-"), mod.Version))
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpSourceDir)

	repo, err := b.newRepo(fmt.Sprintf("github.com/%s", mod.Path))
	if err != nil {
		return err
	}
	if err := repo.Sync(ctx, mod.Version, "", tmpSourceDir); err != nil {
		return err
	}

	getOutputDir := func(_ string, m module.Version) (string, error) {
		return b.installDir(m.Path, m.Version)
	}
	testContext := classfile.NewContext(tmpSourceDir, outputDir, b.matrix, getOutputDir)
	for _, dep := range b.resolveModTransitiveDeps(targets, mod) {
		testContext.AddBuildResult(dep, b.cachedBuildResult(dep))
	}

	project := &classfile.Project{
		Deps:     b.resolveModTransitiveDeps(targets, mod),
		SourceFS: mod.FS.(fs.ReadFileFS),
	}

	savedEnv := os.Environ()
	defer func() {
		os.Clearenv()
		for _, env := range savedEnv {
			k, v, _ := strings.Cut(env, "=")
			_ = os.Setenv(k, v)
		}
	}()

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()
	if err := os.Chdir(tmpSourceDir); err != nil {
		return err
	}

	var out classfile.BuildResult
	mod.OnTest(testContext, project, &out)
	if len(out.Errs()) > 0 {
		return &OnTestFailureError{
			Module: module.Version{Path: mod.Path, Version: mod.Version},
			Err:    errors.Join(out.Errs()...),
		}
	}
	return nil
}

func (b *Builder) cachedBuildResult(mod module.Version) classfile.BuildResult {
	var result classfile.BuildResult
	cache, err := b.loadCache(mod.Path)
	if err != nil {
		return result
	}
	entry, ok := cache.get(mod.Version, b.matrix)
	if !ok || entry == nil || entry.Metadata == "" {
		return result
	}
	result.SetMetadata(entry.Metadata)
	return result
}
