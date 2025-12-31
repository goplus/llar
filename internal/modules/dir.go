package modules

import (
	"os"
	"path/filepath"

	"github.com/goplus/llar/internal/env"
	"github.com/goplus/llar/pkgs/mod/module"
)

func moduleDirOf(modId string) (string, error) {
	formulaDir, err := env.FormulaDir()
	if err != nil {
		return "", err
	}
	escapedModId, err := module.EscapeID(modId)
	if err != nil {
		return "", err
	}
	moduleDir := filepath.Join(formulaDir, escapedModId)

	if err := os.MkdirAll(moduleDir, 0700); err != nil {
		return "", err
	}
	return moduleDir, nil
}

func sourceCacheDirOf(mod module.Version) (string, error) {
	moduleDir, err := moduleDirOf(mod.ID)
	if err != nil {
		return "", err
	}
	sourceCacheDir := filepath.Join(moduleDir, ".source", mod.Version)

	if err := os.MkdirAll(sourceCacheDir, 0700); err != nil {
		return "", err
	}
	return sourceCacheDir, nil
}
