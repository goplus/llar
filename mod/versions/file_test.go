// Copyright 2024 The llar Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package versions

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/goplus/llar/mod/module"
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
				"path": "example/module",
				"deps": {
					"dep1": [{"path": "dep/one", "version": "v1.0.0"}],
					"dep2": [{"path": "dep/two", "version": "v2.1.0"}]
				}
			}`,
			want: &Versions{
				Path: "example/module",
				Dependencies: map[string][]module.Version{
					"dep1": {{Path: "dep/one", Version: "v1.0.0"}},
					"dep2": {{Path: "dep/two", Version: "v2.1.0"}},
				},
			},
			wantErr: false,
		},
		{
			name: "empty deps",
			data: `{"path": "example/module", "deps": {}}`,
			want: &Versions{
				Path:         "example/module",
				Dependencies: map[string][]module.Version{},
			},
			wantErr: false,
		},
		{
			name: "no deps field",
			data: `{"path": "example/module"}`,
			want: &Versions{
				Path:         "example/module",
				Dependencies: nil,
			},
			wantErr: false,
		},
		{
			name:    "invalpath json",
			data:    `{"path": invalpath}`,
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
			if got.Path != tt.want.Path {
				t.Errorf("Parse() Path = %v, want %v", got.Path, tt.want.Path)
			}
			if len(got.Dependencies) != len(tt.want.Dependencies) {
				t.Errorf("Parse() Dependencies len = %v, want %v", len(got.Dependencies), len(tt.want.Dependencies))
				return
			}
			for k, v := range tt.want.Dependencies {
				if gotDep, ok := got.Dependencies[k]; !ok {
					t.Errorf("Parse() missing dependency %q", k)
				} else if !reflect.DeepEqual(gotDep, v) {
					t.Errorf("Parse() dependency %q = %v, want %v", k, gotDep, v)
				}
			}
		})
	}
}

func TestParse_WithFile(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("valpath file", func(t *testing.T) {
		content := `{"path": "test/module", "deps": {"a": [{"path": "dep/a", "version": "v1.0.0"}]}}`
		file := filepath.Join(tmpDir, "versions.json")
		if err := os.WriteFile(file, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		got, err := Parse(file, nil)
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		if got.Path != "test/module" {
			t.Errorf("Parse() Path = %v, want %v", got.Path, "test/module")
		}
		if len(got.Dependencies) != 1 {
			t.Errorf("Parse() Dependencies len = %v, want 1", len(got.Dependencies))
		}
		if dep := got.Dependencies["a"]; dep[0].Path != "dep/a" || dep[0].Version != "v1.0.0" {
			t.Errorf("Parse() dependency a = %v, want {dep/a v1.0.0}", dep)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := Parse(filepath.Join(tmpDir, "nonexistent.json"), nil)
		if err == nil {
			t.Error("Parse() expected error for nonexistent file")
		}
	})

	t.Run("invalpath json file", func(t *testing.T) {
		file := filepath.Join(tmpDir, "invalpath.json")
		if err := os.WriteFile(file, []byte(`{invalpath`), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := Parse(file, nil)
		if err == nil {
			t.Error("Parse() expected error for invalpath json")
		}
	})
}

func TestParse_DataTakesPrecedence(t *testing.T) {
	tmpDir := t.TempDir()

	fileContent := `{"path": "from/file"}`
	file := filepath.Join(tmpDir, "versions.json")
	if err := os.WriteFile(file, []byte(fileContent), 0644); err != nil {
		t.Fatal(err)
	}

	dataContent := `{"path": "from/data"}`
	got, err := Parse(file, []byte(dataContent))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// data should take precedence over file
	if got.Path != "from/data" {
		t.Errorf("Parse() Path = %v, want from/data (data should take precedence)", got.Path)
	}
}
