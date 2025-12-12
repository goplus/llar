package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
	formulas, err := modload.LoadPackages(ctx, module.Version{mainModId, mainModVer})
	if err != nil {
		return err
	}
	buildResults := make(map[module.Version]*formula.BuildResult)

	build := func(f *modload.Formula) error {
		f.Proj.Matrix = matrx
		f.Proj.BuildResults = buildResults
		results := &formula.BuildResult{}

		if f.OnBuild == nil {
			panic(fmt.Sprintf("failed to build %s: no onBuild", f.ID))
		}
		unlock, err := lockBuild(f)
		if err != nil {
			return err
		}
		defer unlock()

		f.OnBuild(f.Proj, results)

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

	// finally, we could move the result
	for _, f := range formulas {
		outputDir := buildResults[f.Version].OutputDir
		buildDir := filepath.Join(f.Dir, "build", f.Version.Version, f.Proj.Matrix.String())

		if err := os.Rename(outputDir, buildDir); err != nil {
			return err
		}
	}

	return nil
}

func lockBuild(f *modload.Formula) (unlock func(), err error) {
	buildDir := filepath.Join(f.Dir, "build", f.Version.Version, f.Proj.Matrix.String())
	if err = os.MkdirAll(buildDir, 0700); err != nil {
		return nil, err
	}
	lockFile := filepath.Join(buildDir, ".lock")

	return lockedfile.MutexAt(lockFile).Lock()
}
