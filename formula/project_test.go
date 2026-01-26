package formula

import (
	"errors"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/goplus/llar/mod/module"
)

func TestModuleDeps_Require(t *testing.T) {
	deps := &ModuleDeps{}

	deps.Require("owner/repo", "1.2.3")
	deps.Require("foo/bar", "0.9.0")

	want := []module.Version{
		{Path: "owner/repo", Version: "1.2.3"},
		{Path: "foo/bar", Version: "0.9.0"},
	}
	if got := deps.Deps(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ModuleDeps.Deps() = %#v, want %#v", got, want)
	}
}

func TestBuildResult_ErrsAndMetadata(t *testing.T) {
	result := &BuildResult{}
	errA := errors.New("first")
	errB := errors.New("second")

	result.AddErr(errA)
	result.AddErr(errB)

	if got := result.Errs(); len(got) != 2 || got[0] != errA || got[1] != errB {
		t.Fatalf("BuildResult.Errs() = %#v, want [%v %v]", got, errA, errB)
	}

	if result.Metadata() != "" {
		t.Fatalf("BuildResult.Metadata() = %q, want empty string", result.Metadata())
	}
	result.SetMetadata("-lssl")
	if result.Metadata() != "-lssl" {
		t.Fatalf("BuildResult.Metadata() = %q, want %q", result.Metadata(), "-lssl")
	}
}

func TestProject_ReadFile(t *testing.T) {
	proj := &Project{
		SourceFS: fstest.MapFS{
			"hello.txt": {Data: []byte("hello")},
		},
	}

	t.Run("existing file", func(t *testing.T) {
		got, err := proj.ReadFile("hello.txt")
		if err != nil {
			t.Fatalf("Project.ReadFile() error = %v", err)
		}
		if string(got) != "hello" {
			t.Fatalf("Project.ReadFile() = %q, want %q", string(got), "hello")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		if _, err := proj.ReadFile("missing.txt"); err == nil {
			t.Fatalf("Project.ReadFile() error = nil, want error")
		}
	})
}

func TestContext_CurrentMatrix(t *testing.T) {
	ctx := &Context{}
	matrix := Matrix{
		Require: map[string][]string{
			"os": {"linux"},
		},
		Options: map[string][]string{
			"ssl": {"sslON"},
		},
	}

	ctx.SetCurrentMatrix(matrix)
	if got := ctx.CurrentMatrix(); !reflect.DeepEqual(got, matrix) {
		t.Fatalf("Context.CurrentMatrix() = %#v, want %#v", got, matrix)
	}
}

func TestContext_BuildResult(t *testing.T) {
	ctx := &Context{}
	mod := module.Version{Path: "owner/repo", Version: "1.0.0"}

	if _, ok := ctx.BuildResult(mod); ok {
		t.Fatalf("Context.BuildResult() ok = true, want false")
	}

	result := BuildResult{}
	result.SetMetadata("metadata")

	ctx.AddBuildResult(mod, result)
	got, ok := ctx.BuildResult(mod)
	if !ok {
		t.Fatalf("Context.BuildResult() ok = false, want true")
	}
	if got.Metadata() != "metadata" {
		t.Fatalf("Context.BuildResult() metadata = %q, want %q", got.Metadata(), "metadata")
	}
}
