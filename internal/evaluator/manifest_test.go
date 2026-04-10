package evaluator

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildOutputManifest_BasicEntries(t *testing.T) {
	root := t.TempDir()

	toolPath := filepath.Join(root, "bin", "tool")
	if err := os.MkdirAll(filepath.Dir(toolPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolPath, []byte("hello"), 0o755); err != nil {
		t.Fatal(err)
	}

	headerPath := filepath.Join(root, "include", "foo.h")
	if err := os.MkdirAll(filepath.Dir(headerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(headerPath, []byte("#define FOO 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	linkPath := filepath.Join(root, "lib", "libfoo.so")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../bin/tool", linkPath); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	manifest, err := BuildOutputManifest(root, "-lfoo")
	if err != nil {
		t.Fatalf("BuildOutputManifest() error: %v", err)
	}

	if got, want := manifest.Metadata, "-lfoo"; got != want {
		t.Fatalf("manifest.Metadata = %q, want %q", got, want)
	}
	if got := manifest.Entries["bin/tool"]; got.Kind != "file" || got.Digest == "" || !got.Executable {
		t.Fatalf("manifest entry bin/tool = %+v", got)
	}
	if got := manifest.Entries["include/foo.h"]; got.Kind != "file" || got.Digest == "" || got.Executable {
		t.Fatalf("manifest entry include/foo.h = %+v", got)
	}
	if got := manifest.Entries["lib/libfoo.so"]; got.Kind != "symlink" || got.Target != "../bin/tool" {
		t.Fatalf("manifest entry lib/libfoo.so = %+v", got)
	}
}

func TestBuildOutputManifest_ArchiveDigestIgnoresHeaderNoise(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	rootC := t.TempDir()

	membersA := []testArchiveMember{
		{Name: "foo.o", Data: []byte("foo-object"), Mtime: 111, UID: 1, GID: 2, Mode: 0o644},
		{Name: "bar.o", Data: []byte("bar-object"), Mtime: 222, UID: 3, GID: 4, Mode: 0o644},
	}
	membersB := []testArchiveMember{
		{Name: "foo.o", Data: []byte("foo-object"), Mtime: 9999, UID: 42, GID: 24, Mode: 0o600},
		{Name: "bar.o", Data: []byte("bar-object"), Mtime: 8888, UID: 7, GID: 8, Mode: 0o777},
	}
	membersC := []testArchiveMember{
		{Name: "foo.o", Data: []byte("foo-object-changed"), Mtime: 111, UID: 1, GID: 2, Mode: 0o644},
		{Name: "bar.o", Data: []byte("bar-object"), Mtime: 222, UID: 3, GID: 4, Mode: 0o644},
	}

	pathA := filepath.Join(rootA, "lib", "libfoo.a")
	pathB := filepath.Join(rootB, "lib", "libfoo.a")
	pathC := filepath.Join(rootC, "lib", "libfoo.a")
	for _, path := range []string{pathA, pathB, pathC} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeTestArchive(pathA, membersA); err != nil {
		t.Fatal(err)
	}
	if err := writeTestArchive(pathB, membersB); err != nil {
		t.Fatal(err)
	}
	if err := writeTestArchive(pathC, membersC); err != nil {
		t.Fatal(err)
	}

	manifestA, err := BuildOutputManifest(rootA, "")
	if err != nil {
		t.Fatalf("BuildOutputManifest(rootA) error: %v", err)
	}
	manifestB, err := BuildOutputManifest(rootB, "")
	if err != nil {
		t.Fatalf("BuildOutputManifest(rootB) error: %v", err)
	}
	manifestC, err := BuildOutputManifest(rootC, "")
	if err != nil {
		t.Fatalf("BuildOutputManifest(rootC) error: %v", err)
	}

	digestA := manifestA.Entries["lib/libfoo.a"].Digest
	digestB := manifestB.Entries["lib/libfoo.a"].Digest
	digestC := manifestC.Entries["lib/libfoo.a"].Digest
	if digestA == "" || digestB == "" || digestC == "" {
		t.Fatalf("archive digests missing: A=%q B=%q C=%q", digestA, digestB, digestC)
	}
	if digestA != digestB {
		t.Fatalf("archive digest should ignore header noise: A=%q B=%q", digestA, digestB)
	}
	if digestA == digestC {
		t.Fatalf("archive digest should change when member content changes: A=%q C=%q", digestA, digestC)
	}
}

type testArchiveMember struct {
	Name  string
	Data  []byte
	Mtime int64
	UID   int
	GID   int
	Mode  int
}

func writeTestArchive(path string, members []testArchiveMember) error {
	var buf bytes.Buffer
	buf.WriteString("!<arch>\n")
	for _, member := range members {
		name := member.Name
		if !strings.HasSuffix(name, "/") {
			name += "/"
		}
		size := len(member.Data)
		header := fmt.Sprintf("%-16s%-12d%-6d%-6d%-8o%-10d`\n", name, member.Mtime, member.UID, member.GID, member.Mode, size)
		if len(header) != 60 {
			return fmt.Errorf("unexpected archive header length %d", len(header))
		}
		buf.WriteString(header)
		buf.Write(member.Data)
		if size%2 != 0 {
			buf.WriteByte('\n')
		}
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
