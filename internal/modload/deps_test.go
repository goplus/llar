package modload

import (
	"context"
	"testing"

	"github.com/goplus/llar/formula"
	repo "github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/pkgs/mod/module"
)

func TestInitProj(t *testing.T) {
	f := &Formula{
		Version:       module.Version{ID: "test/repo", Version: "1.0.0"},
		vcs:           repo.NewGitVCS(),
		remoteRepoUrl: "https://github.com/test/repo",
	}

	// Test that initProj is a no-op when Proj is already set
	f.Proj = &formula.Project{}
	err := initProj(context.Background(), f)
	if err != nil {
		t.Errorf("initProj() with existing Proj should not error, got %v", err)
	}
}

func TestResolveDeps_FromVersionsJSON(t *testing.T) {
	// Create a formula with Proj already set (to skip initProj's Sync)
	// and OnRequire as nil (to use versions.json fallback)
	f := &Formula{
		Version:   module.Version{ID: "DaveGamble/cJSON", Version: "1.7.18"},
		Dir:       "testdata/DaveGamble/cJSON",
		Proj:      &formula.Project{},
		OnRequire: nil,
	}

	deps, err := resolveDeps(context.Background(), f)
	if err != nil {
		t.Fatalf("resolveDeps() error = %v", err)
	}

	// Should find dependency from versions.json
	if len(deps) != 1 {
		t.Fatalf("resolveDeps() got %d deps, want 1", len(deps))
	}

	if deps[0].ID != "madler/zlib" {
		t.Errorf("deps[0].ID = %q, want %q", deps[0].ID, "madler/zlib")
	}
	if deps[0].Version != "1.2.1" {
		t.Errorf("deps[0].Version = %q, want %q", deps[0].Version, "1.2.1")
	}
}

func TestResolveDeps_FromOnRequire(t *testing.T) {
	// Create a formula with OnRequire that sets dependencies
	f := &Formula{
		Version: module.Version{ID: "test/repo", Version: "1.0.0"},
		Dir:     "testdata/test/repo",
		Proj:    &formula.Project{},
		OnRequire: func(proj *formula.Project, deps *formula.ModuleDeps) {
			deps.Require("foo/bar", "2.0.0")
			deps.Require("baz/qux", "3.0.0")
		},
	}

	deps, err := resolveDeps(context.Background(), f)
	if err != nil {
		t.Fatalf("resolveDeps() error = %v", err)
	}

	if len(deps) != 2 {
		t.Fatalf("resolveDeps() got %d deps, want 2", len(deps))
	}

	if deps[0].ID != "foo/bar" || deps[0].Version != "2.0.0" {
		t.Errorf("deps[0] = %v, want {foo/bar 2.0.0}", deps[0])
	}
	if deps[1].ID != "baz/qux" || deps[1].Version != "3.0.0" {
		t.Errorf("deps[1] = %v, want {baz/qux 3.0.0}", deps[1])
	}
}

func TestResolveDeps_EmptyDeps(t *testing.T) {
	// Create a formula with OnRequire that sets no dependencies
	f := &Formula{
		Version:   module.Version{ID: "madler/zlib", Version: "1.0.0"},
		Dir:       "testdata/madler/zlib",
		Proj:      &formula.Project{},
		OnRequire: func(proj *formula.Project, deps *formula.ModuleDeps) {},
	}

	deps, err := resolveDeps(context.Background(), f)
	if err != nil {
		t.Fatalf("resolveDeps() error = %v", err)
	}

	// OnRequire returned nothing, and version 1.0.0 has no deps in versions.json
	if len(deps) != 0 {
		t.Errorf("resolveDeps() got %d deps, want 0", len(deps))
	}
}
