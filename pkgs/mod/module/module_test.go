package module

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestEscapePath(t *testing.T) {
	tests := []struct {
		name        string
		modId       string
		wantEscaped string
		wantErr     bool
	}{
		{
			name:        "simple path",
			modId:       "owner/repo",
			wantEscaped: filepath.Join("owner", "repo"),
			wantErr:     false,
		},
		{
			name:        "empty string",
			modId:       "",
			wantEscaped: "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			escaped, err := EscapePath(tt.modId)
			if (err != nil) != tt.wantErr {
				t.Errorf("EscapePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if escaped != tt.wantEscaped {
				t.Errorf("EscapePath() = %v, want %v", escaped, tt.wantEscaped)
			}
		})
	}
}

func TestEscapePath_Invalid(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("absolute path test only applies to windows")
	}

	_, err := EscapePath("C:\\absolute\\path")
	if err == nil {
		t.Error("EscapePath() expected error for absolute path on windows")
	}
}

func TestVersion(t *testing.T) {
	v := Version{
		Path:    "owner/repo",
		Version: "v1.0.0",
	}

	if v.Path != "owner/repo" {
		t.Errorf("Version.Path = %v, want %v", v.Path, "owner/repo")
	}
	if v.Version != "v1.0.0" {
		t.Errorf("Version.Version = %v, want %v", v.Version, "v1.0.0")
	}
}
