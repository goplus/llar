package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/build/lockedfile"
	"github.com/goplus/llar/internal/env"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/pkgs/mod/module"
)

type Builder struct {
	initOnce sync.Once
}

// BuildTarget represents a single target to build.
type BuildTarget struct {
	Version module.Version
	Dir     string // formula directory
	Project *formula.Project
	OnBuild func(*formula.Project, *formula.BuildResult) error
}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) Init(ctx context.Context, vcs vcs.VCS, remoteFormulaRepo string) error {
	formulaDir, err := env.FormulaDir()
	if err != nil {
		return err
	}
	latest, err := vcs.Latest(ctx, remoteFormulaRepo)

	lockFile := filepath.Join(formulaDir, ".lock")

	unlock, err := lockedfile.MutexAt(lockFile).Lock()
	if err != nil {
		return err
	}
	defer unlock()

	return vcs.Sync(ctx, remoteFormulaRepo, latest, formulaDir)
}

func (b *Builder) Build(ctx context.Context, mainModule module.Version, targets []BuildTarget, matrx formula.Matrix) error {
	buildResults := make(map[module.Version]*formula.BuildResult)

	build := func(target BuildTarget) error {
		buildDir := filepath.Join(target.Dir, "build", target.Version.Version, matrx.String())
		cacheFilePath := filepath.Join(buildDir, cacheFile)

		target.Project.Matrix = matrx
		target.Project.FormulaDir = buildDir
		target.Project.BuildResults = buildResults

		if target.OnBuild == nil {
			panic(fmt.Sprintf("failed to build %s: no onBuild", target.Version.ID))
		}

		unlock, err := lockTarget(&target, matrx)
		if err != nil {
			return err
		}
		defer unlock()

		// Double-check cache after acquiring lock (another process may have built it)
		if cache, err := loadBuildCache(cacheFilePath); err == nil {
			buildResults[target.Version] = &cache.BuildResult
			return nil
		}

		if err := os.Chdir(target.Project.BuildDir); err != nil {
			return err
		}

		results := &formula.BuildResult{}

		if err := target.OnBuild(target.Project, results); err != nil {
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

		buildResults[target.Version] = results

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
	var mainTarget BuildTarget
	for _, target := range targets {
		if target.Version.ID == mainModule.ID && target.Version.Version == mainModule.Version {
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

func lockTarget(target *BuildTarget, matrx formula.Matrix) (unlock func(), err error) {
	buildDir := filepath.Join(target.Dir, "build", target.Version.Version, matrx.String())
	if err = os.MkdirAll(buildDir, 0700); err != nil {
		return nil, err
	}
	lockFile := filepath.Join(buildDir, ".lock")

	return lockedfile.MutexAt(lockFile).Lock()
}
