package env

import (
	"os"
	"path/filepath"
)

func WorkDir() (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userCacheDir, ".llar"), nil
}
