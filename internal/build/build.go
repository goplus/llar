package build

import (
	"context"
	"fmt"
	"io/fs"
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
	matrix   classfile.Matrix
	initOnce sync.Once
}

type Result struct {
	OutputDir string
}

// NewBuilder creates a new Builder.
func NewBuilder(matrx classfile.Matrix) *Builder {
	return &Builder{matrix: matrx}
}

func (b *Builder) Build(ctx context.Context, mainModule module.Version, targets []*modules.Module) ([]Result, error) {
	buildResults := make(map[module.Version]classfile.BuildResult)

	build := func(target *modules.Module) (Result, error) {
		// modID/.build/1.2.11/x86
		sourceDir := filepath.Join(target.Dir, ".source")
		outputDir := filepath.Join(target.Dir, ".build", target.Version, b.matrix.String())
		cacheFilePath := filepath.Join(outputDir, cacheFile)

		// Create output directory
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return Result{}, err
		}

		if target.OnBuild == nil {
			return Result{}, fmt.Errorf("failed to build %s: no onBuild", target.ID)
		}
		// TODO(MeteorsLiu): Support different code host sites.
		repo, err := vcs.NewRepo(fmt.Sprintf("github.com/%s", target.ID))
		if err != nil {
			return Result{}, err
		}
		// Sync source to .source/ (git manages versions)
		if err = repo.Sync(ctx, target.Version, "", sourceDir); err != nil {
			return Result{}, err
		}
		unlock, err := lockTarget(target, b.matrix)
		if err != nil {
			return Result{}, err
		}
		defer unlock()

		// Double-check cache after acquiring lock (another process may have built it)
		if cache, err := loadBuildCache(cacheFilePath); err == nil {
			buildResults[module.Version{target.ID, target.Version}] = cache.BuildResult
			return Result{OutputDir: cache.BuildResult.OutputDir}, nil
		}
		if err := os.Chdir(sourceDir); err != nil {
			return Result{}, err
		}

		var results classfile.BuildResult

		ctx := &classfile.Context{
			Matrix:       b.matrix,
			BuildResults: buildResults,
		}
		proj := &classfile.Project{
			Deps:   moduleVersionsOf(target.Deps),
			FileFS: repo.At(target.Version, sourceDir).(fs.ReadFileFS),
		}

		if err := target.OnBuild(ctx, proj, &results); err != nil {
			return Result{}, err
		}
		// Save build cache
		cache := buildCache{
			BuildResult: results,
			BuildTime:   time.Now(),
		}
		if err := saveBuildCache(cacheFilePath, &cache); err != nil {
			return Result{}, err
		}

		buildResults[module.Version{target.ID, target.Version}] = results
		return Result{OutputDir: outputDir}, nil
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

	// 1st pos for main target
	output := []Result{{}}

	for _, target := range targets {
		if target.ID == mainModule.Path && target.Version == mainModule.Version {
			mainTarget = target
			continue // skip main for now
		}
		result, err := build(target)
		if err != nil {
			return nil, err
		}
		output = append(output, result)
	}
	// build main
	result, err := build(mainTarget)
	if err != nil {
		return nil, err
	}
	output[0] = result
	return output, nil
}

func lockTarget(target *modules.Module, matrx classfile.Matrix) (unlock func(), err error) {
	outputDir := filepath.Join(target.Dir, ".build", target.Version, matrx.String())
	if err = os.MkdirAll(outputDir, 0700); err != nil {
		return nil, err
	}
	lockFile := filepath.Join(outputDir, ".lock")

	return lockedfile.MutexAt(lockFile).Lock()
}

func moduleVersionsOf(mod []*modules.Module) []module.Version {
	var versions []module.Version

	for _, m := range mod {
		versions = append(versions, module.Version{m.ID, m.Version})
	}

	return versions
}
