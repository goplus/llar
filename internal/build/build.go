package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	classfile "github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/lockedfile"
	"github.com/goplus/llar/internal/modules"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/pkgs/mod/module"
)

type Builder struct {
	initOnce sync.Once
	// newRepo creates a vcs.Repo for downloading source code.
	// If nil, uses vcs.NewRepo.
	newRepo func(repoPath string) (vcs.Repo, error)
}

// NewBuilder creates a new Builder with optional custom repo creator.
// If newRepo is nil, uses vcs.NewRepo.
func NewBuilder(newRepo func(repoPath string) (vcs.Repo, error)) *Builder {
	if newRepo == nil {
		newRepo = vcs.NewRepo
	}
	return &Builder{newRepo: newRepo}
}

func (b *Builder) Build(ctx context.Context, mainModule module.Version, targets []*modules.Module, matrx classfile.Matrix) error {
	buildResults := make(map[module.Version]*classfile.BuildResult)

	build := func(target *modules.Module) error {
		buildDir := filepath.Join(target.Dir, ".source", target.Version)
		outputDir := filepath.Join(target.Dir, ".build", target.Version, matrx.String())
		cacheFilePath := filepath.Join(outputDir, cacheFile)

		proj := &classfile.Project{
			Deps:         moduleVersionsOf(target.Deps),
			BuildDir:     buildDir,
			BuildResults: buildResults,
			Matrix:       matrx,
		}
		if target.OnBuild == nil {
			return fmt.Errorf("failed to build %s: no onBuild", target.ID)
		}
		// TODO(MeteorsLiu): Support different code host sites.
		repo, err := b.newRepo(fmt.Sprintf("github.com/%s", target.ID))
		if err != nil {
			return err
		}
		if err = repo.Sync(ctx, target.Version, "", buildDir); err != nil {
			return err
		}
		unlock, err := lockTarget(target, matrx)
		if err != nil {
			return err
		}
		defer unlock()

		// Double-check cache after acquiring lock (another process may have built it)
		if cache, err := loadBuildCache(cacheFilePath); err == nil {
			buildResults[module.Version{target.ID, target.Version}] = &cache.BuildResult
			return nil
		}
		if err := os.Chdir(buildDir); err != nil {
			return err
		}

		results := &classfile.BuildResult{}

		if err := target.OnBuild(proj, results); err != nil {
			return err
		}

		os.RemoveAll(buildDir)
		// Move the result to build directory
		if err := os.Rename(results.OutputDir, buildDir); err != nil {
			return err
		}

		// Save build cache
		cache := buildCache{
			BuildResult: *results,
			BuildTime:   time.Now(),
		}
		if err := saveBuildCache(cacheFilePath, &cache); err != nil {
			return err
		}

		buildResults[module.Version{target.ID, target.Version}] = results

		return nil
	}

	// Save environment before Build and restore after
	savedEnv := os.Environ()

	defer func() {
		os.Clearenv()
		for _, e := range savedEnv {
			if k, v, ok := strings.Cut(e, "="); ok {
				os.Setenv(k, v)
			}
		}
	}()

	// Build dependencies first, then main
	var mainTarget *modules.Module
	for _, target := range targets {
		if target.ID == mainModule.ID && target.Version == mainModule.Version {
			mainTarget = target
			continue // skip main for now
		}
		if err := build(target); err != nil {
			return err
		}
	}
	// build main
	if err := build(mainTarget); err != nil {
		return err
	}

	return nil
}

func lockTarget(target *modules.Module, matrx classfile.Matrix) (unlock func(), err error) {
	buildDir := filepath.Join(target.Dir, ".build", target.Version, matrx.String())
	if err = os.MkdirAll(buildDir, 0700); err != nil {
		return nil, err
	}
	lockFile := filepath.Join(buildDir, ".lock")

	return lockedfile.MutexAt(lockFile).Lock()
}

func moduleVersionsOf(mod []*modules.Module) []module.Version {
	var versions []module.Version

	for _, m := range mod {
		versions = append(versions, module.Version{m.ID, m.Version})
	}

	return versions
}
