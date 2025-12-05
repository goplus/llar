package versions

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
)

type Dependency struct {
	ModuleID string `json:"id"`
	Version  string `json:"version"`
}

type Versions struct {
	ModuleID     string                `json:"id"`
	Dependencies map[string]Dependency `json:"deps"`
}

func Parse(file string, data []byte) (*Versions, error) {
	var reader io.Reader

	if data != nil {
		reader = bytes.NewBuffer(data)
	} else {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		reader = f
	}

	var v Versions

	if err := json.NewDecoder(reader).Decode(&v); err != nil {
		return nil, err
	}

	return &v, nil
}
