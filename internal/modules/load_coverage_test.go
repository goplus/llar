package modules

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/goplus/llar/internal/formula"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/mod/module"
)

type fakeReadFileFS struct {
	readFile func(name string) ([]byte, error)
	open     func(name string) (fs.File, error)
}

func (f fakeReadFileFS) ReadFile(name string) ([]byte, error) {
	if f.readFile == nil {
		return nil, fs.ErrNotExist
	}
	return f.readFile(name)
}

func (f fakeReadFileFS) Open(name string) (fs.File, error) {
	if f.open == nil {
		return nil, fs.ErrNotExist
	}
	return f.open(name)
}

type fakeFile struct {
	stat func() (fs.FileInfo, error)
}

func (f fakeFile) Stat() (fs.FileInfo, error) {
	if f.stat == nil {
		return nil, fs.ErrNotExist
	}
	return f.stat()
}

func (f fakeFile) Read(_ []byte) (int, error) { return 0, io.EOF }
func (f fakeFile) Close() error               { return nil }

type mockLatestRepo struct {
	tags    []string
	tagsErr error
}

var _ vcs.Repo = (*mockLatestRepo)(nil)

func (m *mockLatestRepo) Tags(context.Context) ([]string, error) { return m.tags, m.tagsErr }
func (m *mockLatestRepo) Latest(context.Context) (string, error) { return "", nil }
func (m *mockLatestRepo) At(ref, localDir string) fs.FS          { return os.DirFS(localDir) }
func (m *mockLatestRepo) Sync(ctx context.Context, ref, path, localDir string) error {
	return nil
}

func testReqs(main module.Version, roots []module.Version, onLoad func(module.Version) ([]module.Version, error)) *mvsReqs {
	return &mvsReqs{
		roots: roots,
		isMain: func(v module.Version) bool {
			return v.Path == main.Path && v.Version == main.Version
		},
		cmp: func(_ string, v1, v2 string) int {
			if v1 == v2 {
				return 0
			}
			if v1 == "none" {
				return -1
			}
			if v2 == "none" {
				return 1
			}
			if v1 < v2 {
				return -1
			}
			return 1
		},
		onLoad: onLoad,
	}
}

func TestLatestVersion_SelectsMaxByComparator(t *testing.T) {
	repo := &mockLatestRepo{
		tags: []string{"v2", "v10", "v3"},
	}

	cmp := func(v1, v2 module.Version) int {
		n1, _ := strconv.Atoi(strings.TrimPrefix(v1.Version, "v"))
		n2, _ := strconv.Atoi(strings.TrimPrefix(v2.Version, "v"))
		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
		return 0
	}

	got, err := latestVersion(context.Background(), "towner/leafmod", repo, cmp)
	if err != nil {
		t.Fatalf("latestVersion failed: %v", err)
	}
	if got != "v10" {
		t.Fatalf("latestVersion = %q, want %q", got, "v10")
	}
}

func TestLatestVersion_NoTags(t *testing.T) {
	repo := &mockLatestRepo{tags: []string{}}

	cmp := func(v1, v2 module.Version) int { return strings.Compare(v1.Version, v2.Version) }

	_, err := latestVersion(context.Background(), "towner/leafmod", repo, cmp)
	if err == nil {
		t.Fatal("expected error for no tags")
	}
	if !strings.Contains(err.Error(), "no tags found") {
		t.Fatalf("error = %v, want contains %q", err, "no tags found")
	}
}

func TestLatestVersion_TagsError(t *testing.T) {
	repo := &mockLatestRepo{tagsErr: errors.New("forced tags error")}

	cmp := func(v1, v2 module.Version) int { return strings.Compare(v1.Version, v2.Version) }

	_, err := latestVersion(context.Background(), "towner/leafmod", repo, cmp)
	if err == nil {
		t.Fatal("expected tags error")
	}
	if !strings.Contains(err.Error(), "forced tags error") {
		t.Fatalf("error = %v, want contains %q", err, "forced tags error")
	}
}

func TestResolveDeps_InvalidModulePath(t *testing.T) {
	modFS := os.DirFS("testdata/load/towner/standalone").(fs.ReadFileFS)
	mod := module.Version{Path: "", Version: "1.0.0"}
	frla := &formula.Formula{ModPath: "", FromVer: "1.0.0"}

	_, err := resolveDeps(mod, modFS, frla)
	if err == nil {
		t.Fatal("expected error for invalid module path")
	}
}

func TestResolveDeps_MissingVersionsFile(t *testing.T) {
	modFS := os.DirFS("testdata/load/towner/badcmp").(fs.ReadFileFS)
	mod := module.Version{Path: "towner/badcmp", Version: "1.0.0"}
	frla := &formula.Formula{ModPath: "towner/badcmp", FromVer: "1.0.0"}

	_, err := resolveDeps(mod, modFS, frla)
	if err == nil {
		t.Fatal("expected error for missing versions.json")
	}
}

func TestLoad_EmptyVersion_ComparatorError(t *testing.T) {
	store := setupTestStore(t, "testdata/load")
	main := module.Version{Path: "towner/badcmp", Version: ""}

	_, err := Load(context.Background(), main, Options{FormulaStore: store})
	if err == nil {
		t.Fatal("expected comparator loading error")
	}
}

func TestLoad_EmptyVersion_NewRepoError(t *testing.T) {
	store := setupTestStore(t, "testdata/load")
	main := module.Version{Path: "bad", Version: ""}

	_, err := Load(context.Background(), main, Options{FormulaStore: store})
	if err == nil {
		t.Fatal("expected vcs.NewRepo error")
	}
}

func TestLoad_EmptyVersion_LatestVersionTagsError(t *testing.T) {
	store := setupTestStore(t, "testdata/load")
	main := module.Version{Path: "llar-nonexistent-owner-20260209/llar-nonexistent-repo-20260209", Version: ""}

	_, err := Load(context.Background(), main, Options{FormulaStore: store})
	if err == nil {
		t.Fatal("expected latestVersion tags error")
	}
}

func TestResolveDeps_OnRequireMkdirTempError(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "tmp-file")
	if err := os.WriteFile(tmpFile, []byte("not-a-dir"), 0644); err != nil {
		t.Fatalf("write tmp file: %v", err)
	}
	t.Setenv("TMPDIR", tmpFile)
	t.Setenv("TMP", tmpFile)
	t.Setenv("TEMP", tmpFile)

	frla := loadTestFormula(t, "testdata/load/towner/withreq", "towner/withreq", "1.0.0")
	modFS := os.DirFS("testdata/load/towner/withreq").(fs.ReadFileFS)
	mod := module.Version{Path: "towner/withreq", Version: "1.0.0"}

	_, err := resolveDeps(mod, modFS, frla)
	if err == nil {
		t.Fatal("expected MkdirTemp error")
	}
}

func TestLoad_NoneDepsComparisonBranches(t *testing.T) {
	store := setupTestStore(t, "testdata/load")
	ctx := context.Background()

	tests := []struct {
		name        string
		main        module.Version
		wantModules int
	}{
		{
			name:        "mix none and concrete version",
			main:        module.Version{Path: "towner/nonemix", Version: "1.0.0"},
			wantModules: 2,
		},
		{
			name:        "only none dependency",
			main:        module.Version{Path: "towner/noneonly", Version: "1.0.0"},
			wantModules: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mods, err := Load(ctx, tt.main, Options{FormulaStore: store})
			if err != nil {
				t.Fatalf("Load failed: %v", err)
			}
			if len(mods) != tt.wantModules {
				t.Fatalf("modules len = %d, want %d", len(mods), tt.wantModules)
			}
		})
	}
}
