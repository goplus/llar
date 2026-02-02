package modules

import (
	"os"
	"path/filepath"

	"github.com/goplus/llar/internal/formula"
	"github.com/goplus/llar/mod/module"
)

func moduleDirOf(modPath string) (string, error) {
	formulaDir, err := formula.Dir()
	if err != nil {
		return "", err
	}
	escapedModPath, err := module.EscapePath(modPath)
	if err != nil {
		return "", err
	}
	moduleDir := filepath.Join(formulaDir, escapedModPath)

	if err := os.MkdirAll(moduleDir, 0700); err != nil {
		return "", err
	}
	return moduleDir, nil
}

func sourceCacheDirOf(mod module.Version) (string, error) {
	moduleDir, err := moduleDirOf(mod.Path)
	if err != nil {
		return "", err
	}
	sourceCacheDir := filepath.Join(moduleDir, ".source", mod.Version)

	if err := os.MkdirAll(sourceCacheDir, 0700); err != nil {
		return "", err
	}
	return sourceCacheDir, nil
}
