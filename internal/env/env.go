// Package env provides environment-related utility functions for llar.
package env

import (
	"os"
	"path/filepath"
	"testing"
)

// FormulaDir returns the directory path where formulas are stored.
// It creates the directory with 0700 permissions if it doesn't exist.
// The directory is located at <UserCacheDir>/.llar/formulas.
//
// Returns:
//   - string: The absolute path to the formulas directory
//   - error: An error if the user cache directory cannot be determined or the directory cannot be created
func FormulaDir() (string, error) {
	if testing.Testing() {
		return "testdata", nil
	}
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
