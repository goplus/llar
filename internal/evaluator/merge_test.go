package evaluator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeOutputTrees_TextFileThreeWayMerge(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()

	baseText := []byte("#define WITH_A 0\n#define KEEP 1\n#define WITH_B 0\n")
	leftText := []byte("#define WITH_A 1\n#define KEEP 1\n#define WITH_B 0\n")
	rightText := []byte("#define WITH_A 0\n#define KEEP 1\n#define WITH_B 1\n")

	writeMergeFile(t, filepath.Join(baseDir, "include", "config.h"), baseText, 0o644)
	writeMergeFile(t, filepath.Join(leftDir, "include", "config.h"), leftText, 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "include", "config.h"), rightText, 0o644)

	baseManifest, err := BuildOutputManifest(baseDir, "-lbase")
	if err != nil {
		t.Fatal(err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "-lbase")
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "-lbase")
	if err != nil {
		t.Fatal(err)
	}

	result, err := MergeOutputTrees(baseDir, baseManifest, leftDir, leftManifest, rightDir, rightManifest)
	if err != nil {
		t.Fatalf("MergeOutputTrees() error: %v", err)
	}
	if !result.Clean() {
		t.Fatalf("MergeOutputTrees() issues = %#v, want clean", result.Issues)
	}

	got, err := os.ReadFile(filepath.Join(result.Root, "include", "config.h"))
	if err != nil {
		t.Fatal(err)
	}
	want := "#define WITH_A 1\n#define KEEP 1\n#define WITH_B 1\n"
	if string(got) != want {
		t.Fatalf("merged config.h = %q, want %q", string(got), want)
	}
}

func TestMergeOutputTrees_ArchiveMemberMerge(t *testing.T) {
	oldPostProcess := postProcessMergedArchive
	postProcessMergedArchive = func(string) error { return nil }
	defer func() {
		postProcessMergedArchive = oldPostProcess
	}()

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()

	baseMembers := []testArchiveMember{
		{Name: "foo.o", Data: []byte("foo-base"), Mtime: 1, UID: 1, GID: 1, Mode: 0o644},
		{Name: "bar.o", Data: []byte("bar-base"), Mtime: 1, UID: 1, GID: 1, Mode: 0o644},
	}
	leftMembers := []testArchiveMember{
		{Name: "foo.o", Data: []byte("foo-left"), Mtime: 2, UID: 2, GID: 2, Mode: 0o644},
		{Name: "bar.o", Data: []byte("bar-base"), Mtime: 2, UID: 2, GID: 2, Mode: 0o644},
	}
	rightMembers := []testArchiveMember{
		{Name: "foo.o", Data: []byte("foo-base"), Mtime: 3, UID: 3, GID: 3, Mode: 0o644},
		{Name: "bar.o", Data: []byte("bar-right"), Mtime: 3, UID: 3, GID: 3, Mode: 0o644},
	}

	basePath := filepath.Join(baseDir, "lib", "libfoo.a")
	leftPath := filepath.Join(leftDir, "lib", "libfoo.a")
	rightPath := filepath.Join(rightDir, "lib", "libfoo.a")
	for _, path := range []string{basePath, leftPath, rightPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeTestArchive(basePath, baseMembers); err != nil {
		t.Fatal(err)
	}
	if err := writeTestArchive(leftPath, leftMembers); err != nil {
		t.Fatal(err)
	}
	if err := writeTestArchive(rightPath, rightMembers); err != nil {
		t.Fatal(err)
	}

	baseManifest, err := BuildOutputManifest(baseDir, "")
	if err != nil {
		t.Fatal(err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "")
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "")
	if err != nil {
		t.Fatal(err)
	}

	result, err := MergeOutputTrees(baseDir, baseManifest, leftDir, leftManifest, rightDir, rightManifest)
	if err != nil {
		t.Fatalf("MergeOutputTrees() error: %v", err)
	}
	if !result.Clean() {
		t.Fatalf("MergeOutputTrees() issues = %#v, want clean", result.Issues)
	}

	mergedMembers, err := readArchiveMembers(filepath.Join(result.Root, "lib", "libfoo.a"))
	if err != nil {
		t.Fatal(err)
	}
	memberMap := archiveMemberMap(mergedMembers)
	if got := string(memberMap["foo.o"].Body); got != "foo-left" {
		t.Fatalf("merged foo.o = %q, want %q", got, "foo-left")
	}
	if got := string(memberMap["bar.o"].Body); got != "bar-right" {
		t.Fatalf("merged bar.o = %q, want %q", got, "bar-right")
	}
}

func TestMergeOutputTrees_BinaryConflict(t *testing.T) {
	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()

	writeMergeFile(t, filepath.Join(baseDir, "lib", "blob.bin"), []byte{0x00, 0x02}, 0o644)
	writeMergeFile(t, filepath.Join(leftDir, "lib", "blob.bin"), []byte{0x00, 0x03}, 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "lib", "blob.bin"), []byte{0x00, 0x04}, 0o644)

	baseManifest, err := BuildOutputManifest(baseDir, "")
	if err != nil {
		t.Fatal(err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "")
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "")
	if err != nil {
		t.Fatal(err)
	}

	result, err := MergeOutputTrees(baseDir, baseManifest, leftDir, leftManifest, rightDir, rightManifest)
	if err != nil {
		t.Fatalf("MergeOutputTrees() error: %v", err)
	}
	if result.Clean() {
		t.Fatal("MergeOutputTrees() = clean, want rebuild issue")
	}
	if result.Status != OutputMergeStatusNeedsRebuild {
		t.Fatalf("status = %q, want %q", result.Status, OutputMergeStatusNeedsRebuild)
	}
	if len(result.Issues) != 1 || result.Issues[0].Path != "lib/blob.bin" {
		t.Fatalf("issues = %#v, want lib/blob.bin issue", result.Issues)
	}
	if result.Issues[0].Kind != OutputMergeIssueKindFileBinaryUnmergeable {
		t.Fatalf("issue kind = %q, want %q", result.Issues[0].Kind, OutputMergeIssueKindFileBinaryUnmergeable)
	}
}

func TestMergeOutputTrees_ArchiveRebuildIssueIncludesMemberNames(t *testing.T) {
	oldPostProcess := postProcessMergedArchive
	postProcessMergedArchive = func(string) error { return nil }
	defer func() {
		postProcessMergedArchive = oldPostProcess
	}()

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()

	baseMembers := []testArchiveMember{
		{Name: "shared.o", Data: []byte("shared-base"), Mtime: 1, UID: 1, GID: 1, Mode: 0o644},
	}
	leftMembers := []testArchiveMember{
		{Name: "shared.o", Data: []byte("shared-left"), Mtime: 2, UID: 2, GID: 2, Mode: 0o644},
	}
	rightMembers := []testArchiveMember{
		{Name: "shared.o", Data: []byte("shared-right"), Mtime: 3, UID: 3, GID: 3, Mode: 0o644},
	}

	basePath := filepath.Join(baseDir, "lib", "libfoo.a")
	leftPath := filepath.Join(leftDir, "lib", "libfoo.a")
	rightPath := filepath.Join(rightDir, "lib", "libfoo.a")
	for _, path := range []string{basePath, leftPath, rightPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeTestArchive(basePath, baseMembers); err != nil {
		t.Fatal(err)
	}
	if err := writeTestArchive(leftPath, leftMembers); err != nil {
		t.Fatal(err)
	}
	if err := writeTestArchive(rightPath, rightMembers); err != nil {
		t.Fatal(err)
	}

	baseManifest, err := BuildOutputManifest(baseDir, "")
	if err != nil {
		t.Fatal(err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "")
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "")
	if err != nil {
		t.Fatal(err)
	}

	result, err := MergeOutputTrees(baseDir, baseManifest, leftDir, leftManifest, rightDir, rightManifest)
	if err != nil {
		t.Fatalf("MergeOutputTrees() error: %v", err)
	}
	if result.Clean() {
		t.Fatal("MergeOutputTrees() = clean, want archive rebuild issue")
	}
	if result.Status != OutputMergeStatusNeedsRebuild {
		t.Fatalf("status = %q, want %q", result.Status, OutputMergeStatusNeedsRebuild)
	}
	if len(result.Issues) != 1 || result.Issues[0].Path != "lib/libfoo.a" {
		t.Fatalf("issues = %#v, want archive issue", result.Issues)
	}
	if result.Issues[0].Kind != OutputMergeIssueKindArchiveUnmergeable {
		t.Fatalf("issue kind = %q, want %q", result.Issues[0].Kind, OutputMergeIssueKindArchiveUnmergeable)
	}
	if !strings.Contains(result.Issues[0].Detail, "shared.o") {
		t.Fatalf("archive issue detail = %q, want member detail", result.Issues[0].Detail)
	}
	for _, token := range []string{
		"automatic archive merge cannot materialize a combined output",
		"conflicting members (1):",
		"shared.o",
	} {
		if !strings.Contains(result.Issues[0].Detail, token) {
			t.Fatalf("archive issue detail = %q, missing %q", result.Issues[0].Detail, token)
		}
	}
	if strings.Contains(result.Issues[0].Detail, "digest=") {
		t.Fatalf("archive issue detail = %q, unexpectedly contains digest details", result.Issues[0].Detail)
	}
}

func TestMergeOutputTrees_MetadataFlagAppendMerge(t *testing.T) {
	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()

	baseManifest, err := BuildOutputManifest(baseDir, "-lPocoFoundation")
	if err != nil {
		t.Fatal(err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "-lPocoFoundation -lPocoJSON")
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "-lPocoFoundation -lPocoXML")
	if err != nil {
		t.Fatal(err)
	}

	result, err := MergeOutputTrees(baseDir, baseManifest, leftDir, leftManifest, rightDir, rightManifest)
	if err != nil {
		t.Fatalf("MergeOutputTrees() error: %v", err)
	}
	if !result.Clean() {
		t.Fatalf("MergeOutputTrees() issues = %#v, want clean", result.Issues)
	}
	if result.Metadata != "-lPocoFoundation -lPocoJSON -lPocoXML" {
		t.Fatalf("merged metadata = %q, want %q", result.Metadata, "-lPocoFoundation -lPocoJSON -lPocoXML")
	}
}

func TestMergeOutputTrees_MetadataConflictIncludesProcess(t *testing.T) {
	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()

	baseManifest, err := BuildOutputManifest(baseDir, "-lbase")
	if err != nil {
		t.Fatal(err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "-ljson -lbase")
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "-lxml -lbase")
	if err != nil {
		t.Fatal(err)
	}

	result, err := MergeOutputTrees(baseDir, baseManifest, leftDir, leftManifest, rightDir, rightManifest)
	if err != nil {
		t.Fatalf("MergeOutputTrees() error: %v", err)
	}
	if result.Clean() {
		t.Fatal("MergeOutputTrees() = clean, want metadata rebuild issue")
	}
	if result.Status != OutputMergeStatusNeedsRebuild {
		t.Fatalf("status = %q, want %q", result.Status, OutputMergeStatusNeedsRebuild)
	}
	if len(result.Issues) != 1 || result.Issues[0].Path != "<metadata>" {
		t.Fatalf("issues = %#v, want metadata issue", result.Issues)
	}
	if result.Issues[0].Kind != OutputMergeIssueKindMetadataUnmergeable {
		t.Fatalf("issue kind = %q, want %q", result.Issues[0].Kind, OutputMergeIssueKindMetadataUnmergeable)
	}
	for _, token := range []string{
		"both sides changed metadata",
		"shared base flags are not a common prefix",
	} {
		if !strings.Contains(result.Issues[0].Detail, token) {
			t.Fatalf("metadata issue detail = %q, missing %q", result.Issues[0].Detail, token)
		}
	}
}

func TestMergeOutputTrees_MetadataNonFlagConflict(t *testing.T) {
	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()

	baseManifest, err := BuildOutputManifest(baseDir, "matrix-off")
	if err != nil {
		t.Fatal(err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "matrix-left")
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "matrix-right")
	if err != nil {
		t.Fatal(err)
	}

	result, err := MergeOutputTrees(baseDir, baseManifest, leftDir, leftManifest, rightDir, rightManifest)
	if err != nil {
		t.Fatalf("MergeOutputTrees() error: %v", err)
	}
	if result.Clean() {
		t.Fatal("MergeOutputTrees() = clean, want metadata rebuild issue")
	}
	if result.Status != OutputMergeStatusNeedsRebuild {
		t.Fatalf("status = %q, want %q", result.Status, OutputMergeStatusNeedsRebuild)
	}
	if len(result.Issues) != 1 || result.Issues[0].Path != "<metadata>" {
		t.Fatalf("issues = %#v, want metadata issue", result.Issues)
	}
	if result.Issues[0].Kind != OutputMergeIssueKindMetadataUnmergeable {
		t.Fatalf("issue kind = %q, want %q", result.Issues[0].Kind, OutputMergeIssueKindMetadataUnmergeable)
	}
}

func writeMergeFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}
