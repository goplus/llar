package modules

import (
	"context"
	"fmt"
	"testing"

	"github.com/goplus/llar/pkgs/gnu"
	"github.com/goplus/llar/pkgs/mod/module"
)

func TestE2E(t *testing.T) {
	mods, err := LoadPackages(context.TODO(), module.Version{ID: "DaveGamble/cJSON", Version: "1.7.18"}, PackageOpts{})
	if err != nil {
		t.Error(err)
		return
	}
	for _, f := range mods {
		fmt.Println(f.ID, f.Proj.Deps)
	}
}

func TestLatestVersion(t *testing.T) {
	comparator := func(v1, v2 module.Version) int {
		return gnu.Compare(v1.Version, v2.Version)
	}

	tests := []struct {
		name    string
		modID   string
		wantErr bool
	}{
		{
			name:    "valid repo with tags",
			modID:   "DaveGamble/cJSON",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := latestVersion(tt.modID, comparator)
			if tt.wantErr {
				if err == nil {
					t.Errorf("latestVersion() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("latestVersion() error = %v", err)
				return
			}
			if version == "" {
				t.Errorf("latestVersion() returned empty version")
			}
			t.Logf("latestVersion(%s) = %s", tt.modID, version)
		})
	}
}
