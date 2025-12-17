package build

import (
	"context"
	"fmt"
	"io"
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

	lockFile := filepath.Join(formulaDir, ".lock")

	unlock, err := lockedfile.MutexAt(lockFile).Lock()
	if err != nil {
		return err
	}
	defer unlock()

	return vcs.Sync(ctx, remoteFormulaRepo, latest, formulaDir)
}

// BuildOptions contains options for the Build method.
type BuildOptions struct {
	// Verbose enables verbose output during build.
	// If false, build output is suppressed.
	Verbose bool
}

func (b *Builder) Build(ctx context.Context, mainModId, mainModVer string, matrx formula.Matrix, opts BuildOptions) ([]module.Version, error) {
	formulas, err := modload.LoadPackages(ctx, module.Version{mainModId, mainModVer}, modload.PackageOpts{})
	if err != nil {
		return nil, err
	}
	buildResults := make(map[module.Version]*formula.BuildResult)

	build := func(f *modload.Formula) error {
		buildDir := filepath.Join(f.Dir, "build", f.Version.Version, matrx.String())
		cacheFilePath := filepath.Join(buildDir, cacheFile)

		f.Proj.Matrix = matrx
		f.Proj.FormulaDir = buildDir
		f.Proj.BuildResults = buildResults

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

		if err := os.Chdir(f.Proj.BuildDir); err != nil {
			return err
		}

		results := &formula.BuildResult{}

		// Save environment before OnBuild and restore after
		savedEnv := os.Environ()

		// Suppress output if not verbose
		var savedStdout, savedStderr *os.File
		if !opts.Verbose {
			// Redirect gsh.App's fout/ferr
			f.SetStdout(io.Discard)
			f.SetStderr(io.Discard)

			// Also redirect os.Stdout/os.Stderr for direct usage
			savedStdout = os.Stdout
			savedStderr = os.Stderr
			devNull, err := os.Open(os.DevNull)
			if err != nil {
				return err
			}
			os.Stdout = devNull
			os.Stderr = devNull
			defer func() {
				devNull.Close()
				os.Stdout = savedStdout
				os.Stderr = savedStderr
			}()
		}

		if err := f.OnBuild(f.Proj, results); err != nil {
			return err
		}

		// Restore gsh.App's fout/ferr after build
		if !opts.Verbose {
			f.SetStdout(savedStdout)
			f.SetStderr(savedStderr)
		}

		os.Clearenv()
		for _, e := range savedEnv {
			if k, v, ok := strings.Cut(e, "="); ok {
				os.Setenv(k, v)
			}
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

		buildResults[f.Version] = results

		return nil
	}
	// first is main
	for _, f := range formulas[1:] {
		if err := build(f); err != nil {
			return nil, err
		}
	}
	// build main
	if err := build(formulas[0]); err != nil {
		return nil, err
	}

	// Return build list
	buildList := make([]module.Version, len(formulas))
	for i, f := range formulas {
		buildList[i] = f.Version
	}
	return buildList, nil
}

func lock(f *modload.Formula) (unlock func(), err error) {
	buildDir := filepath.Join(f.Dir, "build", f.Version.Version, f.Proj.Matrix.String())
	if err = os.MkdirAll(buildDir, 0700); err != nil {
		return nil, err
	}
	lockFile := filepath.Join(buildDir, ".lock")

	return lockedfile.MutexAt(lockFile).Lock()
}
