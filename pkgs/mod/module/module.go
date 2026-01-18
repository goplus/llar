// Package module defines the module.Version type along with support code.
package module

import (
	"path/filepath"
)

// A Version (for clients, a module.Version) represents a specific version
// of a module identified by its path.
type Version struct {
	Path    string // Module path in the form "owner/repo"
	Version string // Version string (e.g., "1.0.0")
}

// EscapePath returns the escaped form of the given module path as a valid
// file system path. It fails if the module path is invalid.
func EscapePath(path string) (escaped string, err error) {
	return filepath.Localize(path)
}
