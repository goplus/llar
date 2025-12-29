package module

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestSplitID(t *testing.T) {
	tests := []struct {
		name      string
		modId     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "valid module id",
			modId:     "owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantErr:   false,
		},
		{
			name:      "valid module id with complex repo name",
			modId:     "DaveGamble/cJSON",
			wantOwner: "DaveGamble",
			wantRepo:  "cJSON",
			wantErr:   false,
		},
		{
			name:      "module id with multiple slashes",
			modId:     "owner/repo/extra",
			wantOwner: "owner",
			wantRepo:  "repo/extra",
			wantErr:   false,
		},
		{
			name:      "missing separator",
			modId:     "noslash",
			wantOwner: "",
			wantRepo:  "",
			wantErr:   true,
		},
		{
			name:      "empty string",
			modId:     "",
			wantOwner: "",
			wantRepo:  "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := SplitID(tt.modId)
			if (err != nil) != tt.wantErr {
				t.Errorf("SplitID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("SplitID() owner = %v, want %v", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("SplitID() repo = %v, want %v", repo, tt.wantRepo)
			}
		})
	}
}

func TestEscapeID(t *testing.T) {
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
			escaped, err := EscapeID(tt.modId)
			if (err != nil) != tt.wantErr {
				t.Errorf("EscapeID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if escaped != tt.wantEscaped {
				t.Errorf("EscapeID() = %v, want %v", escaped, tt.wantEscaped)
			}
		})
	}
}

func TestEscapeID_Invalid(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("absolute path test only applies to windows")
	}

	_, err := EscapeID("C:\\absolute\\path")
	if err == nil {
		t.Error("EscapeID() expected error for absolute path on windows")
	}
}

func TestVersion(t *testing.T) {
	v := Version{
		ID:      "owner/repo",
		Version: "v1.0.0",
	}

	if v.ID != "owner/repo" {
		t.Errorf("Version.ID = %v, want %v", v.ID, "owner/repo")
	}
	if v.Version != "v1.0.0" {
		t.Errorf("Version.Version = %v, want %v", v.Version, "v1.0.0")
	}
}
