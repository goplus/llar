package modules

import (
	"os"
	"path/filepath"

	"github.com/goplus/llar/internal/env"
	"github.com/goplus/llar/pkgs/mod/module"
)

func moduleDirOf(modPath string) (string, error) {
	formulaDir, err := env.FormulaDir()
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
