package env

import (
	"os"
	"path/filepath"
)

func FormulaDir() (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	formulaDir := filepath.Join(userCacheDir, ".llar", "formulas")

	if err := os.MkdirAll(formulaDir, 0700); err != nil {
		return "", err
	}
	return formulaDir, nil
}
