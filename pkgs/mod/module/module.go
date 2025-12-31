// Package module defines the module.Version type along with support code.
package module

import (
	"fmt"
	"path/filepath"
	"strings"
)

// A Version (for clients, a module.Version) represents a specific version
// of a module identified by its ID.
type Version struct {
	ID      string // Module ID in the form "owner/repo"
	Version string // Version string (e.g., "1.0.0")
}

// SplitID returns owner and repo such that owner + "/" + repo == modId.
// It returns an error if the module ID does not contain a slash.
func SplitID(modId string) (owner, repo string, err error) {
	owner, repo, ok := strings.Cut(modId, "/")
	if !ok {
		return "", "", fmt.Errorf("failed to split module id: separator not found")
	}
	return owner, repo, nil
}

// EscapeID returns the escaped form of the given module ID as a valid
// file system path. It fails if the module ID is invalid.
func EscapeID(modId string) (escaped string, err error) {
	return filepath.Localize(modId)
}
