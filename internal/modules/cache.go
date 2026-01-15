package modules

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/goplus/llar/internal/env"
	"github.com/goplus/llar/internal/formula"
	"github.com/goplus/llar/internal/lockedfile"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/pkgs/gnu"
	"github.com/goplus/llar/pkgs/mod/module"
)

const _defaultFormulaSuffix = "_llar.gox"

// classfileCache manages formula loading and caching.
// It maintains a cache of loaded formulas and version comparators.
type classfileCache struct {
	formulaRepo vcs.Repo
	formulas    map[module.Version]*formula.Formula
	comparators map[string]func(v1, v2 module.Version) int
	searchPaths []string // formula search paths (first match wins)
}

func newClassfileCache(formulaRepo vcs.Repo, localDir string) *classfileCache {
	if localDir == "" {
		localDir = "."
	}

	return &classfileCache{
		formulaRepo: formulaRepo,
		formulas:    make(map[module.Version]*formula.Formula),
		comparators: make(map[string]func(v1, v2 module.Version) int),
		searchPaths: []string{localDir},
	}
}

func (m *classfileCache) lazyDownloadFormula(modId string) error {
	moduleDir, err := moduleDirOf(modId)
	if err != nil {
		return err
	}
	lockfile := filepath.Join(moduleDir, ".lock")

	unlock, err := lockedfile.MutexAt(lockfile).Lock()
	if err != nil {
		return err
	}
	defer unlock()
	// normally, the file structure of repo is like:
	// ├── madler
	// │   └── zlib
	// │       ├── 1.0.0
	// │       │   └── zlib_llar.gox
	// │       └── versions.json
	// └── pnggroup
	// 	└── libpng
	// 		├── 1.0.0
	// 		│   └── libpng_llar.gox
	// 		├── libpng_cmp.gox
	// 		└── versions.json
	// so modId is the sub directory of a module
	formulaDir, err := env.FormulaDir()
	if err != nil {
		return err
	}
	return m.formulaRepo.Sync(context.TODO(), "main", modId, formulaDir)
}

// comparatorOf returns a version comparator for the specified module.
// It caches comparators to avoid reloading them.
func (m *classfileCache) comparatorOf(modId string) (func(v1, v2 module.Version) int, error) {
	if comp, ok := m.comparators[modId]; ok {
		return comp, nil
	}
	if err := m.lazyDownloadFormula(modId); err != nil {
		return nil, err
	}
	moduleDir, err := moduleDirOf(modId)
	if err != nil {
		return nil, err
	}
	seachPaths := append([]string{moduleDir}, m.searchPaths...)

	var comp func(v1 module.Version, v2 module.Version) int

	for _, searchPath := range seachPaths {
		comp, err = loadComparator(searchPath)
		if err == nil {
			break
		}
	}
	if comp == nil {
		comp = func(v1, v2 module.Version) int {
			return gnu.Compare(v1.Version, v2.Version)
		}
	}
	m.comparators[modId] = comp
	return comp, nil
}

// formulaOf returns the formula for the specified module version.
// It finds the appropriate formula file based on version and caches the result.
func (m *classfileCache) formulaOf(mod module.Version) (*formula.Formula, error) {
	comparator, err := m.comparatorOf(mod.Path)
	if err != nil {
		return nil, err
	}
	maxFromVer, formulaPath, err := m.findMaxFromVer(mod, comparator)
	if err != nil {
		return nil, err
	}
	cacheKey := module.Version{Path: mod.Path, Version: maxFromVer}
	f, ok := m.formulas[cacheKey]
	if ok {
		return f, nil
	}
	f, err = formula.Load(formulaPath)
	if err != nil {
		return nil, err
	}
	m.formulas[cacheKey] = f
	return f, nil
}

// findMaxFromVer finds the formula file with the highest FromVer that is <= the target version.
// It searches through all searchPaths in order, returning the first match.
func (m *classfileCache) findMaxFromVer(mod module.Version, compare func(v1, v2 module.Version) int) (maxFromVer, formulaPath string, err error) {
	moduleDir, err := moduleDirOf(mod.Path)
	if err != nil {
		return "", "", err
	}
	seachPaths := append([]string{moduleDir}, m.searchPaths...)

	for _, seachPath := range seachPaths {
		// Skip if directory doesn't exist
		if _, statErr := os.Stat(seachPath); os.IsNotExist(statErr) {
			continue
		}

		err = filepath.WalkDir(seachPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !strings.HasSuffix(path, _defaultFormulaSuffix) {
				return nil
			}
			fromVer, err := formula.FromVerOf(path)
			if err != nil {
				return err
			}
			fromVerMod := module.Version{mod.Path, fromVer}

			if compare(fromVerMod, mod) > 0 {
				return nil
			}
			if maxFromVer == "" || compare(fromVerMod, module.Version{mod.Path, maxFromVer}) > 0 {
				maxFromVer = fromVer
				formulaPath = path
			}
			return nil
		})

		if err != nil {
			return "", "", err
		}

		// Found in this search path, return immediately
		if formulaPath != "" {
			return maxFromVer, formulaPath, nil
		}
	}

	return "", "", fmt.Errorf("failed to load formula: no formula found for %s", mod.Path)
}
