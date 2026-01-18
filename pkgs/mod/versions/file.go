// Package versions provides functionality for parsing and managing module version files.
package versions

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
)

// Dependency represents a single module dependency with its version information.
type Dependency struct {
	Path    string `json:"path"`    // Module Path
	Version string `json:"version"` // Version string of the dependency (e.g., "v1.0.0")
}

// Versions represents a module's version file containing its dependencies.
type Versions struct {
	Path         string                  `json:"path"` // Module Path
	Dependencies map[string][]Dependency `json:"deps"` // Map of dependency name to dependency details
}

// Parse reads and parses a version file from either provided data or a file path.
// If data is non-nil, it is used directly and the file parameter is ignored.
// Otherwise, the file is read from the provided path.
// Returns the parsed Versions struct or an error if parsing fails.
func Parse(file string, data []byte) (*Versions, error) {
	var reader io.Reader

	if data != nil {
		reader = bytes.NewBuffer(data)
	} else {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer f.Close()

		reader = f
	}

	var v Versions

	if err := json.NewDecoder(reader).Decode(&v); err != nil {
		return nil, err
	}

	return &v, nil
}
