package build

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/build/lockedfile"
	"github.com/goplus/llar/internal/env"
	"github.com/goplus/llar/internal/modload"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/pkgs/mod/module"
)

const cacheFile = ".cache.json"

// BuildCache contains metadata about a successful build.
type BuildCache struct {
	BuildResult formula.BuildResult `json:"build_result"`
	BuildTime   time.Time           `json:"build_time"`
}

type Builder struct {
	initOnce sync.Once
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
	if err != nil {
		return err
	}
	lockFile := filepath.Join(formulaDir, ".lock")

	unlock, err := lockedfile.MutexAt(lockFile).Lock()
	if err != nil {
		return err
	}
	defer unlock()

	return vcs.Sync(ctx, remoteFormulaRepo, latest, formulaDir)
}

func (b *Builder) Build(ctx context.Context, mainModId, mainModVer string, matrx formula.Matrix) error {
	formulas, err := modload.LoadPackages(ctx, module.Version{mainModId, mainModVer}, modload.PackageOpts{})
	if err != nil {
		return err
	}
	buildResults := make(map[module.Version]*formula.BuildResult)

	build := func(f *modload.Formula) error {
		f.Proj.Matrix = matrx
		f.Proj.BuildResults = buildResults

		buildDir := filepath.Join(f.Dir, "build", f.Version.Version, f.Proj.Matrix.String())
		cacheFilePath := filepath.Join(buildDir, cacheFile)

		if f.OnBuild == nil {
			panic(fmt.Sprintf("failed to build %s: no onBuild", f.ID))
		}

		unlock, err := lock(f)
		if err != nil {
			return err
		}
		defer unlock()

		// Double-check cache after acquiring lock (another process may have built it)
		if cache, err := loadBuildCache(cacheFilePath); err == nil {
			buildResults[f.Version] = &cache.BuildResult
			return nil
		}

		results := &formula.BuildResult{}

		// Save environment before OnBuild and restore after
		savedEnv := os.Environ()

		if err := f.OnBuild(f.Proj, results); err != nil {
			return err
		}

		os.Clearenv()
		for _, e := range savedEnv {
			if k, v, ok := strings.Cut(e, "="); ok {
				os.Setenv(k, v)
			}
		}

		// Move the result to build directory
		if err := os.Rename(results.OutputDir, buildDir); err != nil {
			return err
		}

		// Save build cache
		cache := BuildCache{
			BuildResult: *results,
			BuildTime:   time.Now(),
		}
		if err := saveBuildCache(cacheFilePath, &cache); err != nil {
			return err
		}

		buildResults[f.Version] = results
		return nil
	}
	// first is main
	for _, f := range formulas[1:] {
		if err := build(f); err != nil {
			return err
		}
	}
	// build main
	if err := build(formulas[0]); err != nil {
		return err
	}

	return nil
}

func lock(f *modload.Formula) (unlock func(), err error) {
	buildDir := filepath.Join(f.Dir, "build", f.Version.Version, f.Proj.Matrix.String())
	if err = os.MkdirAll(buildDir, 0700); err != nil {
		return nil, err
	}
	lockFile := filepath.Join(buildDir, ".lock")

	return lockedfile.MutexAt(lockFile).Lock()
}

func loadBuildCache(path string) (*BuildCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cache BuildCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func saveBuildCache(path string, cache *BuildCache) error {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
