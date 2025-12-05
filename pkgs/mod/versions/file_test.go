package versions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_WithData(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    *Versions
		wantErr bool
	}{
		{
			name: "basic version file",
			data: `{
				"id": "example/module",
				"deps": {
					"dep1": {"id": "dep/one", "version": "v1.0.0"},
					"dep2": {"id": "dep/two", "version": "v2.1.0"}
				}
			}`,
			want: &Versions{
				ModuleID: "example/module",
				Dependencies: map[string]Dependency{
					"dep1": {ModuleID: "dep/one", Version: "v1.0.0"},
					"dep2": {ModuleID: "dep/two", Version: "v2.1.0"},
				},
			},
			wantErr: false,
		},
		{
			name: "empty deps",
			data: `{"id": "example/module", "deps": {}}`,
			want: &Versions{
				ModuleID:     "example/module",
				Dependencies: map[string]Dependency{},
			},
			wantErr: false,
		},
		{
			name: "no deps field",
			data: `{"id": "example/module"}`,
			want: &Versions{
				ModuleID:     "example/module",
				Dependencies: nil,
			},
			wantErr: false,
		},
		{
			name:    "invalid json",
			data:    `{"id": invalid}`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty json",
			data:    `{}`,
			want:    &Versions{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse("", []byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.ModuleID != tt.want.ModuleID {
				t.Errorf("Parse() ModuleID = %v, want %v", got.ModuleID, tt.want.ModuleID)
			}
			if len(got.Dependencies) != len(tt.want.Dependencies) {
				t.Errorf("Parse() Dependencies len = %v, want %v", len(got.Dependencies), len(tt.want.Dependencies))
				return
			}
			for k, v := range tt.want.Dependencies {
				if gotDep, ok := got.Dependencies[k]; !ok {
					t.Errorf("Parse() missing dependency %q", k)
				} else if gotDep != v {
					t.Errorf("Parse() dependency %q = %v, want %v", k, gotDep, v)
				}
			}
		})
	}
}

func TestParse_WithFile(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("valid file", func(t *testing.T) {
		content := `{"id": "test/module", "deps": {"a": {"id": "dep/a", "version": "v1.0.0"}}}`
		file := filepath.Join(tmpDir, "versions.json")
		if err := os.WriteFile(file, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		got, err := Parse(file, nil)
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		if got.ModuleID != "test/module" {
			t.Errorf("Parse() ModuleID = %v, want %v", got.ModuleID, "test/module")
		}
		if len(got.Dependencies) != 1 {
			t.Errorf("Parse() Dependencies len = %v, want 1", len(got.Dependencies))
		}
		if dep := got.Dependencies["a"]; dep.ModuleID != "dep/a" || dep.Version != "v1.0.0" {
			t.Errorf("Parse() dependency a = %v, want {dep/a v1.0.0}", dep)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := Parse(filepath.Join(tmpDir, "nonexistent.json"), nil)
		if err == nil {
			t.Error("Parse() expected error for nonexistent file")
		}
	})

	t.Run("invalid json file", func(t *testing.T) {
		file := filepath.Join(tmpDir, "invalid.json")
		if err := os.WriteFile(file, []byte(`{invalid`), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := Parse(file, nil)
		if err == nil {
			t.Error("Parse() expected error for invalid json")
		}
	})
}

func TestParse_DataTakesPrecedence(t *testing.T) {
	tmpDir := t.TempDir()

	fileContent := `{"id": "from/file"}`
	file := filepath.Join(tmpDir, "versions.json")
	if err := os.WriteFile(file, []byte(fileContent), 0644); err != nil {
		t.Fatal(err)
	}

	dataContent := `{"id": "from/data"}`
	got, err := Parse(file, []byte(dataContent))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// data should take precedence over file
	if got.ModuleID != "from/data" {
		t.Errorf("Parse() ModuleID = %v, want from/data (data should take precedence)", got.ModuleID)
	}
}
