package evaluator

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/llar/internal/trace"
)

func recordWithProc(pid, parentPID int64, argv []string, cwd string, inputs, changes []string) trace.Record {
	rec := record(argv, cwd, inputs, changes)
	rec.PID = pid
	rec.ParentPID = parentPID
	return rec
}

func TestBuildGraphTracksWriterReaderEdges(t *testing.T) {
	records := []trace.Record{
		record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
		record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		record([]string{"cc", "-c", "cli.c", "-o", "build/cli.o"}, "/tmp/work", []string{"/tmp/work/cli.c"}, []string{"/tmp/work/build/cli.o"}),
		record([]string{"cc", "build/cli.o", "out/lib/libfoo.a", "-o", "out/bin/foo"}, "/tmp/work", []string{"/tmp/work/build/cli.o", "/tmp/work/out/lib/libfoo.a"}, []string{"/tmp/work/out/bin/foo"}),
	}

	graph := buildGraph(records)

	if len(graph.actions) != 4 {
		t.Fatalf("len(graph.actions) = %d, want 4", len(graph.actions))
	}
	wantKinds := []actionKind{kindCompile, kindArchive, kindCompile, kindLink}
	gotKinds := []actionKind{
		graph.actions[0].kind,
		graph.actions[1].kind,
		graph.actions[2].kind,
		graph.actions[3].kind,
	}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("action kinds = %v, want %v", gotKinds, wantKinds)
	}

	assertEdge := func(from, to int, path string) {
		t.Helper()
		for _, edge := range graph.out[from] {
			if edge.to == to && edge.path == normalizePath(path) {
				return
			}
		}
		t.Fatalf("missing edge %d -> %d via %s", from, to, normalizePath(path))
	}

	assertEdge(0, 1, "/tmp/work/build/core.o")
	assertEdge(1, 3, "/tmp/work/out/lib/libfoo.a")
	assertEdge(2, 3, "/tmp/work/build/cli.o")

	if graph.outdeg[3] != 0 {
		t.Fatalf("graph.outdeg[3] = %d, want 0", graph.outdeg[3])
	}
	if graph.indeg[3] != 2 {
		t.Fatalf("graph.indeg[3] = %d, want 2", graph.indeg[3])
	}
}

func TestBuildGraphClassifiesActionsAndKeys(t *testing.T) {
	records := []trace.Record{
		record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
		record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work", []string{"/tmp/work/CMakeLists.txt"}, []string{"/tmp/work/_build/CMakeCache.txt"}),
		record([]string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work", []string{"/tmp/work/_build/libfoo.a"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		record([]string{"cp", "libfoo.a", "out/lib/libfoo.a"}, "/tmp/work", []string{"/tmp/work/libfoo.a"}, []string{"/tmp/work/out/lib/libfoo.a"}),
	}

	graph := buildGraph(records)

	if graph.actions[0].kind != kindCompile {
		t.Fatalf("compile kind = %v, want %v", graph.actions[0].kind, kindCompile)
	}
	wantKey := "compile|cc|cwd=" + normalizePath("/tmp/work") +
		"|src=" + normalizePath("/tmp/work/core.c") +
		"|out=" + normalizePath("/tmp/work/build/core.o")
	if got := graph.actions[0].actionKey; got != wantKey {
		t.Fatalf("compile actionKey = %q", got)
	}

	if graph.actions[1].kind != kindConfigure {
		t.Fatalf("cmake configure kind = %v, want %v", graph.actions[1].kind, kindConfigure)
	}
	if graph.actions[2].kind != kindInstall {
		t.Fatalf("cmake --install kind = %v, want %v", graph.actions[2].kind, kindInstall)
	}
	if graph.actions[3].kind != kindCopy {
		t.Fatalf("cp kind = %v, want %v", graph.actions[3].kind, kindCopy)
	}
}

func TestBuildGraphDropsDirectoryPaths(t *testing.T) {
	records := []trace.Record{
		record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work", "/tmp/work/CMakeLists.txt"},
			[]string{"/tmp/work/_build", "/tmp/work/_build/CMakeCache.txt"}),
		record([]string{"cc", "-I", "/tmp/work/include", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/include", "/tmp/work/include/foo.h", "/tmp/work/core.c"},
			[]string{"/tmp/work/build", "/tmp/work/build/core.o"}),
	}

	graph := buildGraph(records)

	if _, ok := graph.paths[normalizePath("/tmp/work/include")]; ok {
		t.Fatalf("directory path %q unexpectedly retained", normalizePath("/tmp/work/include"))
	}
	if _, ok := graph.paths[normalizePath("/tmp/work/build")]; ok {
		t.Fatalf("directory path %q unexpectedly retained", normalizePath("/tmp/work/build"))
	}
	if _, ok := graph.paths[normalizePath("/tmp/work/_build")]; ok {
		t.Fatalf("directory path %q unexpectedly retained", normalizePath("/tmp/work/_build"))
	}
	if _, ok := graph.paths[normalizePath("/tmp/work/include/foo.h")]; !ok {
		t.Fatalf("file path %q missing", normalizePath("/tmp/work/include/foo.h"))
	}
	if _, ok := graph.paths[normalizePath("/tmp/work/build/core.o")]; !ok {
		t.Fatalf("file path %q missing", normalizePath("/tmp/work/build/core.o"))
	}
}

func TestBuildGraphMarksInstallLeafOutDegree(t *testing.T) {
	records := []trace.Record{
		record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		record([]string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work", []string{"/tmp/work/out/lib/libfoo.a"}, []string{"/tmp/work/install/lib/libfoo.a"}),
	}

	graph := buildGraph(records)
	if graph.actions[1].kind != kindInstall {
		t.Fatalf("install kind = %v, want %v", graph.actions[1].kind, kindInstall)
	}
	if graph.indeg[1] != 1 {
		t.Fatalf("graph.indeg[1] = %d, want 1", graph.indeg[1])
	}
	if graph.outdeg[1] != 0 {
		t.Fatalf("graph.outdeg[1] = %d, want 0", graph.outdeg[1])
	}
}

func TestBuildGraphClassifiesConfigureLaunchedProbeChainAsTooling(t *testing.T) {
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{"/tmp/work/_build/probes/alpha/probe.c"}),
		recordWithProc(101, 100, []string{"cc", "-c", "probe.c", "-o", "probe.o"}, "/tmp/work/_build/probes/alpha",
			[]string{"/tmp/work/_build/probes/alpha/probe.c"},
			[]string{"/tmp/work/_build/probes/alpha/probe.o"}),
		recordWithProc(102, 100, []string{"cc", "probe.o", "-o", "probe.bin"}, "/tmp/work/_build/probes/alpha",
			[]string{"/tmp/work/_build/probes/alpha/probe.o"},
			[]string{"/tmp/work/_build/probes/alpha/probe.bin"}),
	}

	graph := buildGraph(records)

	for i := range graph.actions {
		if !graph.tooling[i] {
			t.Fatalf("graph.tooling[%d] = false, want true", i)
		}
	}

	assertRole := func(path string, want pathRole) {
		t.Helper()
		got := graph.paths[normalizePath(path)].role
		if got != want {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, want)
		}
	}

	assertRole("/tmp/work/_build/probes/alpha/probe.c", roleTooling)
	assertRole("/tmp/work/_build/probes/alpha/probe.o", roleTooling)
	assertRole("/tmp/work/_build/probes/alpha/probe.bin", roleTooling)
}

func TestBuildGraphClassifiesGeneratedHeaderAsPropagating(t *testing.T) {
	records := []trace.Record{
		record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{"/tmp/work/_build/zconf.h"}),
		record([]string{"cc1", "/tmp/work/adler32.c"}, "/tmp/work/_build",
			[]string{"/tmp/work/_build/zconf.h", "/tmp/work/adler32.c"},
			nil),
		record([]string{"as", "-o", "CMakeFiles/zlib.dir/adler32.c.o", "/tmp/cc123.s"}, "/tmp/work/_build",
			nil,
			[]string{"/tmp/work/_build/CMakeFiles/zlib.dir/adler32.c.o"}),
	}

	graph := buildGraph(records)

	if len(graph.actions) != 2 {
		t.Fatalf("len(graph.actions) = %d, want 2", len(graph.actions))
	}
	if !graph.tooling[0] {
		t.Fatalf("graph.tooling[0] = false, want true")
	}
	if graph.business[0] {
		t.Fatalf("graph.business[0] = true, want false")
	}
	if graph.tooling[1] {
		t.Fatalf("graph.tooling[1] = true, want false")
	}
	if !graph.business[1] {
		t.Fatalf("graph.business[1] = false, want true")
	}
	if graph.actions[1].kind != kindCompile {
		t.Fatalf("graph.actions[1].kind = %v, want %v", graph.actions[1].kind, kindCompile)
	}
	if got := graph.actions[1].actionKey; !strings.Contains(got, "src="+normalizePath("/tmp/work/adler32.c")) {
		t.Fatalf("compile actionKey = %q, want source %q", got, normalizePath("/tmp/work/adler32.c"))
	}
	if got := graph.actions[1].actionKey; !strings.Contains(got, "out="+normalizePath("/tmp/work/_build/CMakeFiles/zlib.dir/adler32.c.o")) {
		t.Fatalf("compile actionKey = %q, want output %q", got, normalizePath("/tmp/work/_build/CMakeFiles/zlib.dir/adler32.c.o"))
	}

	got := graph.paths[normalizePath("/tmp/work/_build/zconf.h")].role
	if got != rolePropagating {
		t.Fatalf("role(zconf.h) = %v, want %v", got, rolePropagating)
	}
}

func TestBuildGraphDoesNotMergeAmbiguousDriverAssemblerSequence(t *testing.T) {
	records := []trace.Record{
		record([]string{"cc", "/tmp/work/core.c", "-o", "CMakeFiles/core.dir/core.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/core.c"},
			[]string{"/tmp/work/_build/CMakeFiles/core.dir/core.o.d"}),
		record([]string{"as", "-o", "CMakeFiles/core.dir/core.o", "/tmp/cc123.s"}, "/tmp/work/_build",
			nil,
			[]string{"/tmp/work/_build/CMakeFiles/core.dir/core.o"}),
	}

	graph := buildGraph(records)
	if len(graph.actions) != 2 {
		t.Fatalf("len(graph.actions) = %d, want 2", len(graph.actions))
	}
	if graph.actions[0].kind != kindGeneric {
		t.Fatalf("graph.actions[0].kind = %v, want %v", graph.actions[0].kind, kindGeneric)
	}
	if graph.actions[1].kind != kindCompile {
		t.Fatalf("graph.actions[1].kind = %v, want %v", graph.actions[1].kind, kindCompile)
	}
}

func TestBuildGraphCoalescesGccCc1plusAssemblerPipeline(t *testing.T) {
	records := []trace.Record{
		record([]string{"gcc", "-c", "/tmp/work/src/pugixml.cpp", "-o", "pugixml.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/src/pugixml.cpp", "/tmp/work/src/pugixml.hpp"},
			nil),
		record([]string{"/usr/lib/gcc/aarch64-linux-gnu/12/cc1plus", "-quiet", "/tmp/work/src/pugixml.cpp"}, "/tmp/work/_build",
			[]string{"/tmp/work/src/pugixml.cpp", "/tmp/work/src/pugixml.hpp"},
			nil),
		record([]string{"as", "-o", "pugixml.o", "/tmp/cc123.s"}, "/tmp/work/_build",
			nil,
			[]string{"/tmp/work/_build/pugixml.o"}),
	}

	graph := buildGraph(records)
	if len(graph.actions) != 1 {
		t.Fatalf("len(graph.actions) = %d, want 1", len(graph.actions))
	}
	if graph.actions[0].kind != kindCompile {
		t.Fatalf("graph.actions[0].kind = %v, want %v", graph.actions[0].kind, kindCompile)
	}
}

func TestBuildGraphCoalescesCompilePipeline(t *testing.T) {
	records := []trace.Record{
		record([]string{"cc", "-I", "/tmp/work/_build", "-c", "/tmp/work/core.c", "-o", "CMakeFiles/core.dir/core.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/core.c", "/tmp/work/_build/config.h"},
			[]string{"/tmp/work/_build/CMakeFiles/core.dir/core.o.d"}),
		record([]string{"cc1", "-I", "/tmp/work/_build", "/tmp/work/core.c"}, "/tmp/work/_build",
			[]string{"/tmp/work/core.c", "/tmp/work/_build/config.h"},
			nil),
		record([]string{"as", "-o", "CMakeFiles/core.dir/core.o", "/tmp/cc123.s"}, "/tmp/work/_build",
			nil,
			[]string{"/tmp/work/_build/CMakeFiles/core.dir/core.o"}),
	}

	graph := buildGraph(records)
	if len(graph.actions) != 1 {
		t.Fatalf("len(graph.actions) = %d, want 1", len(graph.actions))
	}
	action := graph.actions[0]
	if action.kind != kindCompile {
		t.Fatalf("action.kind = %v, want %v", action.kind, kindCompile)
	}
	if !slices.Contains(action.reads, normalizePath("/tmp/work/_build/config.h")) {
		t.Fatalf("merged reads = %v, want config header", action.reads)
	}
	if !slices.Contains(action.writes, normalizePath("/tmp/work/_build/CMakeFiles/core.dir/core.o")) {
		t.Fatalf("merged writes = %v, want object output", action.writes)
	}
	if !slices.Contains(action.writes, normalizePath("/tmp/work/_build/CMakeFiles/core.dir/core.o.d")) {
		t.Fatalf("merged writes = %v, want depfile output", action.writes)
	}
}

func TestBuildGraphCoalescesInterleavedCompilePipelines(t *testing.T) {
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cc", "-I", "/tmp/work/_build", "-c", "/tmp/work/a.c", "-o", "a.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/a.c", "/tmp/work/_build/config.h"},
			[]string{"/tmp/work/_build/a.o.d"}),
		recordWithProc(101, 100, []string{"cc1", "-I", "/tmp/work/_build", "/tmp/work/a.c"}, "/tmp/work/_build",
			[]string{"/tmp/work/a.c", "/tmp/work/_build/config.h"},
			nil),
		recordWithProc(200, 1, []string{"cc", "-I", "/tmp/work/_build", "-c", "/tmp/work/b.c", "-o", "b.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/b.c", "/tmp/work/_build/config.h"},
			[]string{"/tmp/work/_build/b.o.d"}),
		recordWithProc(201, 200, []string{"cc1", "-I", "/tmp/work/_build", "/tmp/work/b.c"}, "/tmp/work/_build",
			[]string{"/tmp/work/b.c", "/tmp/work/_build/config.h"},
			nil),
		recordWithProc(202, 200, []string{"as", "-o", "b.o", "/tmp/ccb.s"}, "/tmp/work/_build",
			nil,
			[]string{"/tmp/work/_build/b.o"}),
		recordWithProc(102, 100, []string{"as", "-o", "a.o", "/tmp/cca.s"}, "/tmp/work/_build",
			nil,
			[]string{"/tmp/work/_build/a.o"}),
		record([]string{"ar", "rcs", "libfoo.a", "a.o", "b.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/_build/a.o", "/tmp/work/_build/b.o"},
			[]string{"/tmp/work/_build/libfoo.a"}),
	}

	graph := buildGraph(records)
	if len(graph.actions) != 3 {
		t.Fatalf("len(graph.actions) = %d, want 3", len(graph.actions))
	}
	if graph.actions[0].kind != kindCompile {
		t.Fatalf("graph.actions[0].kind = %v, want %v", graph.actions[0].kind, kindCompile)
	}
	if graph.actions[1].kind != kindCompile {
		t.Fatalf("graph.actions[1].kind = %v, want %v", graph.actions[1].kind, kindCompile)
	}
	if graph.actions[2].kind != kindArchive {
		t.Fatalf("graph.actions[2].kind = %v, want %v", graph.actions[2].kind, kindArchive)
	}
	if !slices.Contains(graph.actions[0].reads, normalizePath("/tmp/work/_build/config.h")) {
		t.Fatalf("graph.actions[0].reads = %v, want config header", graph.actions[0].reads)
	}
	if !slices.Contains(graph.actions[1].reads, normalizePath("/tmp/work/_build/config.h")) {
		t.Fatalf("graph.actions[1].reads = %v, want config header", graph.actions[1].reads)
	}

	assertEdge := func(from, to int, path string) {
		t.Helper()
		for _, edge := range graph.out[from] {
			if edge.to == to && edge.path == normalizePath(path) {
				return
			}
		}
		t.Fatalf("missing edge %d -> %d via %s", from, to, normalizePath(path))
	}

	assertEdge(0, 2, "/tmp/work/_build/a.o")
	assertEdge(1, 2, "/tmp/work/_build/b.o")
}

func TestBuildGraphDoesNotMergeIndependentCompileDrivers(t *testing.T) {
	records := []trace.Record{
		record([]string{"cc", "-c", "/tmp/work/core.c", "-o", "CMakeFiles/core.dir/core.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/core.c"},
			[]string{"/tmp/work/_build/CMakeFiles/core.dir/core.o"}),
		record([]string{"cc", "-c", "/tmp/work/core.c", "-o", "CMakeFiles/core.dir/core.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/core.c"},
			[]string{"/tmp/work/_build/CMakeFiles/core.dir/core.o"}),
	}

	graph := buildGraph(records)
	if len(graph.actions) != 2 {
		t.Fatalf("len(graph.actions) = %d, want 2", len(graph.actions))
	}
}

func TestBuildGraphCoalescesImplicitObjectCompilePipeline(t *testing.T) {
	records := []trace.Record{
		record([]string{"gcc", "-c", "/tmp/work/core.c"}, "/tmp/work/_build",
			[]string{"/tmp/work/core.c"},
			nil),
		record([]string{"as", "-o", "core.o", "/tmp/cc123.s"}, "/tmp/work/_build",
			nil,
			[]string{"/tmp/work/_build/core.o"}),
	}

	graph := buildGraph(records)
	if len(graph.actions) != 1 {
		t.Fatalf("len(graph.actions) = %d, want 1", len(graph.actions))
	}
	if graph.actions[0].kind != kindCompile {
		t.Fatalf("graph.actions[0].kind = %v, want %v", graph.actions[0].kind, kindCompile)
	}
}

func TestBuildGraphClassifiesLinkWithoutCapturedArtifactInputsAsGeneric(t *testing.T) {
	records := []trace.Record{
		record([]string{"cc", "-shared", "-o", "/tmp/work/_build/libfoo.so"}, "/tmp/work",
			nil,
			[]string{"/tmp/work/_build/libfoo.so"}),
	}

	graph := buildGraph(records)
	if len(graph.actions) != 1 {
		t.Fatalf("len(graph.actions) = %d, want 1", len(graph.actions))
	}
	if graph.actions[0].kind != kindGeneric {
		t.Fatalf("graph.actions[0].kind = %v, want %v", graph.actions[0].kind, kindGeneric)
	}
}

func TestBuildGraphClassifiesSharedSourceLinkAsLink(t *testing.T) {
	records := []trace.Record{
		record([]string{"cc", "-shared", "-fPIC", "/tmp/work/foo.c", "-o", "/tmp/work/_build/libfoo.so"}, "/tmp/work",
			[]string{"/tmp/work/foo.c"},
			[]string{"/tmp/work/_build/libfoo.so"}),
	}

	graph := buildGraph(records)
	if len(graph.actions) != 1 {
		t.Fatalf("len(graph.actions) = %d, want 1", len(graph.actions))
	}
	if graph.actions[0].kind != kindLink {
		t.Fatalf("graph.actions[0].kind = %v, want %v", graph.actions[0].kind, kindLink)
	}
}

func TestBuildGraphClassifiesDirectCompileLinkExecutableAsLink(t *testing.T) {
	records := []trace.Record{
		record([]string{"cc", "/tmp/work/main.c", "-o", "/tmp/work/_build/main"}, "/tmp/work",
			[]string{"/tmp/work/main.c"},
			[]string{"/tmp/work/_build/main"}),
	}

	graph := buildGraph(records)
	if len(graph.actions) != 1 {
		t.Fatalf("len(graph.actions) = %d, want 1", len(graph.actions))
	}
	if graph.actions[0].kind != kindLink {
		t.Fatalf("graph.actions[0].kind = %v, want %v", graph.actions[0].kind, kindLink)
	}
}

func TestBuildGraphClassifiesCc1plusAsCompile(t *testing.T) {
	records := []trace.Record{
		record([]string{"/usr/lib/gcc/aarch64-linux-gnu/12/cc1plus", "-quiet", "/tmp/work/src/pugixml.cpp"}, "/tmp/work/_build",
			[]string{"/tmp/work/src/pugixml.cpp", "/tmp/work/src/pugixml.hpp"},
			nil),
	}

	graph := buildGraph(records)
	if len(graph.actions) != 1 {
		t.Fatalf("len(graph.actions) = %d, want 1", len(graph.actions))
	}
	if graph.actions[0].kind != kindCompile {
		t.Fatalf("graph.actions[0].kind = %v, want %v", graph.actions[0].kind, kindCompile)
	}
	if family := toolFamily(graph.actions[0].tool); family != "cxx" {
		t.Fatalf("toolFamily(%q) = %q, want %q", graph.actions[0].tool, family, "cxx")
	}
}

func TestScopedFingerprintIncludesGeneratedHeaderDigestWhenProvided(t *testing.T) {
	root := t.TempDir()
	buildRoot := filepath.Join(root, "_build")
	if err := os.MkdirAll(buildRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(buildRoot): %v", err)
	}
	header := filepath.Join(buildRoot, "config.h")
	source := filepath.Join(root, "core.c")
	object := filepath.Join(buildRoot, "core.o")
	for path, content := range map[string]string{
		header: "#define FLAG 0\n",
		source: "int main(void) { return 0; }\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}

	recordA := normalizeRecord(trace.Record{
		Argv:    []string{"cc", "-c", source, "-o", object},
		Cwd:     root,
		Inputs:  []string{header, source},
		Changes: []string{object},
	})
	scope := trace.Scope{BuildRoot: buildRoot, SourceRoot: root}
	fingerprintA := scopedFingerprint(kindCompile, recordA, scope, map[string]string{
		header: "aaaaaaaaaaaaaaaa",
	})

	if err := os.WriteFile(header, []byte("#define FLAG 1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(header): %v", err)
	}
	fingerprintB := scopedFingerprint(kindCompile, recordA, scope, map[string]string{
		header: "bbbbbbbbbbbbbbbb",
	})
	if fingerprintA == fingerprintB {
		t.Fatalf("fingerprint unchanged after provided generated header digest update: %q", fingerprintA)
	}
}

func TestScopedFingerprintIncludesGeneratedNonHeaderDigestWhenProvided(t *testing.T) {
	root := t.TempDir()
	buildRoot := filepath.Join(root, "_build")
	if err := os.MkdirAll(buildRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(buildRoot): %v", err)
	}
	def := filepath.Join(buildRoot, "exports.def")
	source := filepath.Join(root, "core.c")
	object := filepath.Join(buildRoot, "core.o")
	for path, content := range map[string]string{
		def:    "EXPORTS\nfoo\n",
		source: "int main(void) { return 0; }\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}

	recordA := normalizeRecord(trace.Record{
		Argv:    []string{"cc", "-c", source, "-o", object},
		Cwd:     root,
		Inputs:  []string{def, source},
		Changes: []string{object},
	})
	scope := trace.Scope{BuildRoot: buildRoot, SourceRoot: root}
	fingerprintA := scopedFingerprint(kindCompile, recordA, scope, map[string]string{
		def: "aaaaaaaaaaaaaaaa",
	})

	if err := os.WriteFile(def, []byte("EXPORTS\nbar\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(def): %v", err)
	}
	fingerprintB := scopedFingerprint(kindCompile, recordA, scope, map[string]string{
		def: "bbbbbbbbbbbbbbbb",
	})
	if fingerprintA == fingerprintB {
		t.Fatalf("fingerprint unchanged after provided generated non-header digest update: %q", fingerprintA)
	}
}

func TestScopedFingerprintDoesNotReadGeneratedInputsFromFilesystem(t *testing.T) {
	root := t.TempDir()
	buildRoot := filepath.Join(root, "_build")
	if err := os.MkdirAll(buildRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(buildRoot): %v", err)
	}
	header := filepath.Join(buildRoot, "config.h")
	source := filepath.Join(root, "core.c")
	object := filepath.Join(buildRoot, "core.o")
	for path, content := range map[string]string{
		header: "#define FLAG 0\n",
		source: "int main(void) { return 0; }\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}

	recordA := normalizeRecord(trace.Record{
		Argv:    []string{"cc", "-c", source, "-o", object},
		Cwd:     root,
		Inputs:  []string{header, source},
		Changes: []string{object},
	})
	scope := trace.Scope{BuildRoot: buildRoot, SourceRoot: root}
	fingerprintA := scopedFingerprint(kindCompile, recordA, scope, nil)

	if err := os.Remove(header); err != nil {
		t.Fatalf("Remove(header): %v", err)
	}
	fingerprintB := scopedFingerprint(kindCompile, recordA, scope, nil)
	if fingerprintA != fingerprintB {
		t.Fatalf("fingerprint changed after post-build filesystem mutation:\nA=%q\nB=%q", fingerprintA, fingerprintB)
	}
}

func TestBuildGraphClassifiesConfigureLaunchedCompileChainAsTooling(t *testing.T) {
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{"/tmp/work/_build/bootstrap/detect.c"}),
		recordWithProc(101, 100, []string{"cc1", "detect.c"}, "/tmp/work/_build/bootstrap",
			[]string{"/tmp/work/_build/bootstrap/detect.c"},
			nil),
		recordWithProc(102, 100, []string{"as", "-o", "detect.o", "/tmp/cc.s"}, "/tmp/work/_build/bootstrap",
			nil,
			[]string{"/tmp/work/_build/bootstrap/detect.o"}),
	}

	graph := buildGraph(records)
	for i := range graph.actions {
		if !graph.tooling[i] {
			t.Fatalf("graph.tooling[%d] = false, want true", i)
		}
	}
}

func TestBuildGraphClassifiesProducedExecutableChainAsTooling(t *testing.T) {
	records := []trace.Record{
		record([]string{"sh", "bootstrap.sh"}, "/tmp/work",
			[]string{"/tmp/work/bootstrap.sh"},
			[]string{"/tmp/work/tools/b2"}),
		record([]string{"./tools/b2", "headers"}, "/tmp/work",
			[]string{"/tmp/work/project-config.jam"},
			[]string{
				"/tmp/work/_build/meta/status.txt",
				"/tmp/work/_build/meta/cache.db",
			}),
	}

	graph := buildGraph(records)
	for i := range graph.actions {
		if !graph.tooling[i] {
			t.Fatalf("graph.tooling[%d] = false, want true", i)
		}
	}

	assertRole := func(path string, want pathRole) {
		t.Helper()
		got := graph.paths[normalizePath(path)].role
		if got != want {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, want)
		}
	}

	assertRole("/tmp/work/tools/b2", roleTooling)
	assertRole("/tmp/work/_build/meta/status.txt", roleTooling)
	assertRole("/tmp/work/_build/meta/cache.db", roleTooling)
}

func TestBuildGraphDoesNotClassifyBusinessCodegenAsTooling(t *testing.T) {
	records := []trace.Record{
		recordWithProc(100, 1, []string{"ninja", "-C", "_build"}, "/tmp/work",
			nil,
			nil),
		recordWithProc(101, 100, []string{"cc", "-c", "tools/gen.c", "-o", "_build/gen.o"}, "/tmp/work",
			[]string{"/tmp/work/tools/gen.c"},
			[]string{"/tmp/work/_build/gen.o"}),
		recordWithProc(102, 100, []string{"cc", "_build/gen.o", "-o", "_build/genhdr"}, "/tmp/work",
			[]string{"/tmp/work/_build/gen.o"},
			[]string{"/tmp/work/_build/genhdr"}),
		recordWithProc(103, 100, []string{"./_build/genhdr"}, "/tmp/work",
			nil,
			[]string{"/tmp/work/_build/generated.h"}),
		recordWithProc(104, 100, []string{"cc", "-c", "src/core.c", "-o", "_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/src/core.c", "/tmp/work/_build/generated.h"},
			[]string{"/tmp/work/_build/core.o"}),
		recordWithProc(105, 100, []string{"ar", "rcs", "_build/libfoo.a", "_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/_build/core.o"},
			[]string{"/tmp/work/_build/libfoo.a"}),
		recordWithProc(106, 100, []string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/_build/libfoo.a"},
			[]string{"/tmp/work/install/lib/libfoo.a"}),
	}

	graph := buildGraphWithScope(records, trace.Scope{InstallRoot: "/tmp/work/install"})

	if !graph.tooling[1] {
		t.Fatalf("graph.tooling[1] = false, want true")
	}
	if !graph.tooling[2] {
		t.Fatalf("graph.tooling[2] = false, want true")
	}
	if graph.tooling[3] {
		t.Fatalf("graph.tooling[3] = true, want false")
	}
	if !graph.business[3] {
		t.Fatalf("graph.business[3] = false, want true")
	}

	assertRole := func(path string, want pathRole) {
		t.Helper()
		got := graph.paths[normalizePath(path)].role
		if got != want {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, want)
		}
	}

	assertRole("/tmp/work/_build/generated.h", rolePropagating)
	assertRole("/tmp/work/_build/genhdr", roleTooling)
}

func TestBuildGraphDoesNotClassifyToolLaunchedActionReadingSidePathAsTooling(t *testing.T) {
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{"/tmp/work/_build/meta/config.state"}),
		recordWithProc(101, 1, []string{"python3", "emit.py"}, "/tmp/work",
			[]string{"/tmp/work/emit.py"},
			[]string{"/tmp/work/_build/side/data.txt"}),
		recordWithProc(102, 100, []string{"sh", "-c", "cat /tmp/work/_build/side/data.txt > /tmp/work/_build/probes/side/result.txt"}, "/tmp/work",
			[]string{"/tmp/work/_build/side/data.txt"},
			[]string{"/tmp/work/_build/probes/side/result.txt"}),
	}

	graph := buildGraph(records)

	if !graph.tooling[0] {
		t.Fatalf("graph.tooling[0] = false, want true")
	}
	if graph.tooling[2] {
		t.Fatalf("graph.tooling[2] = true, want false")
	}
	if graph.business[2] {
		t.Fatalf("graph.business[2] = true, want false")
	}

	assertRole := func(path string, want pathRole) {
		t.Helper()
		got := graph.paths[normalizePath(path)].role
		if got != want {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, want)
		}
	}

	assertRole("/tmp/work/_build/meta/config.state", roleTooling)
	assertRole("/tmp/work/_build/side/data.txt", roleUnknown)
	assertRole("/tmp/work/_build/probes/side/result.txt", roleUnknown)
}

func TestBuildGraphClassifiesConfigChecksChainAsTooling(t *testing.T) {
	records := []trace.Record{
		recordWithProc(100, 1, []string{"sh", "bootstrap.sh"}, "/tmp/work",
			[]string{"/tmp/work/bootstrap.sh"},
			[]string{"/tmp/work/tools/b2"}),
		recordWithProc(101, 1, []string{"./tools/b2", "headers"}, "/tmp/work",
			[]string{"/tmp/work/project-config.jam"},
			[]string{
				"/tmp/work/_build/meta/status.txt",
				"/tmp/work/_build/meta/cache.db",
			}),
		recordWithProc(102, 101, []string{"c++", "-c", "/tmp/work/probes/feature/alpha.cpp", "-o", "/tmp/work/_build/probes/feature/alpha.o"}, "/tmp/work",
			[]string{"/tmp/work/probes/feature/alpha.cpp"},
			[]string{"/tmp/work/_build/probes/feature/alpha.o"}),
		recordWithProc(103, 1, []string{"cc", "-c", "/tmp/work/src/core.c", "-o", "/tmp/work/_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/src/core.c"},
			[]string{"/tmp/work/_build/core.o"}),
		recordWithProc(104, 1, []string{"ar", "rcs", "/tmp/work/_build/libfoo.a", "/tmp/work/_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/_build/core.o"},
			[]string{"/tmp/work/_build/libfoo.a"}),
		recordWithProc(105, 1, []string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/_build/libfoo.a"},
			[]string{"/tmp/work/install/lib/libfoo.a"}),
	}

	graph := buildGraphWithScope(records, trace.Scope{InstallRoot: "/tmp/work/install"})

	if !graph.tooling[2] {
		t.Fatalf("graph.tooling[2] = false, want true")
	}
	if graph.business[2] {
		t.Fatalf("graph.business[2] = true, want false")
	}
	if !graph.business[3] || !graph.business[4] || !graph.business[5] {
		t.Fatalf("main delivery chain = business %v, want true true true", graph.business[3:6])
	}

	assertRole := func(path string, want pathRole) {
		t.Helper()
		got := graph.paths[normalizePath(path)].role
		if got != want {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, want)
		}
	}

	assertRole("/tmp/work/probes/feature/alpha.cpp", roleTooling)
	assertRole("/tmp/work/_build/probes/feature/alpha.o", roleTooling)
	assertRole("/tmp/work/_build/core.o", rolePropagating)
	assertRole("/tmp/work/_build/libfoo.a", roleDelivery)
}

func TestBuildGraphKeepsConfigureControlPlanePathsAsTooling(t *testing.T) {
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{
				"/tmp/work/_build/meta/config.state",
				"/tmp/work/_build/meta/compiler.state",
				"/tmp/work/_build/probes/alpha/link.meta",
				"/tmp/work/_build/probes/alpha/test.o",
				"/tmp/work/_build/probes/alpha/test.bin",
				"/tmp/work/_build/meta/progress.count",
				"/tmp/work/_build/config.h",
			}),
		recordWithProc(101, 100, []string{"cmake", "-E", "echo", "progress"}, "/tmp/work",
			[]string{
				"/tmp/work/_build/meta/progress.count",
				"/tmp/work/_build/meta/progress",
				"/tmp/work/_build/probes/alpha/link.meta",
			},
			[]string{"/tmp/work/_build/meta/progress.1"}),
		recordWithProc(102, 1, []string{"cc", "-c", "src/core.c", "-o", "_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/src/core.c", "/tmp/work/_build/config.h"},
			[]string{"/tmp/work/_build/core.o"}),
		recordWithProc(103, 1, []string{"ar", "rcs", "_build/libfoo.a", "_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/_build/core.o"},
			[]string{"/tmp/work/_build/libfoo.a"}),
		recordWithProc(104, 1, []string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/_build/libfoo.a"},
			[]string{"/tmp/work/install/lib/libfoo.a"}),
	}

	graph := buildGraphWithScope(records, trace.Scope{InstallRoot: "/tmp/work/install"})

	assertRole := func(path string, want pathRole) {
		t.Helper()
		got := graph.paths[normalizePath(path)].role
		if got != want {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, want)
		}
	}

	assertRole("/tmp/work/_build/meta/config.state", roleTooling)
	assertRole("/tmp/work/_build/meta/compiler.state", roleTooling)
	assertRole("/tmp/work/_build/probes/alpha/link.meta", roleTooling)
	assertRole("/tmp/work/_build/probes/alpha/test.o", roleTooling)
	assertRole("/tmp/work/_build/probes/alpha/test.bin", roleTooling)
	assertRole("/tmp/work/_build/meta/progress.count", roleTooling)
	assertRole("/tmp/work/_build/meta/progress.1", roleTooling)
	assertRole("/tmp/work/_build/config.h", rolePropagating)
}

func TestBuildGraphDoesNotClassifyConfigureScannedSourceLeafAsTooling(t *testing.T) {
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{
				"/tmp/work/CMakeLists.txt",
				"/tmp/work/Foundation/src/Event_WIN32.cpp",
				"/tmp/work/Foundation/src/Environment_VX.cpp",
			},
			[]string{"/tmp/work/_build/meta/config.state"}),
		recordWithProc(101, 1, []string{"cc", "-c", "src/core.c", "-o", "_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/src/core.c"},
			[]string{"/tmp/work/_build/core.o"}),
		recordWithProc(102, 1, []string{"ar", "rcs", "_build/libfoo.a", "_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/_build/core.o"},
			[]string{"/tmp/work/_build/libfoo.a"}),
		recordWithProc(103, 1, []string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/_build/libfoo.a"},
			[]string{"/tmp/work/install/lib/libfoo.a"}),
	}

	graph := buildGraphWithScope(records, trace.Scope{InstallRoot: "/tmp/work/install"})

	assertRole := func(path string, want pathRole) {
		t.Helper()
		got := graph.paths[normalizePath(path)].role
		if got != want {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, want)
		}
	}

	assertRole("/tmp/work/CMakeLists.txt", roleTooling)
	assertRole("/tmp/work/_build/meta/config.state", roleTooling)
	assertRole("/tmp/work/Foundation/src/Event_WIN32.cpp", roleUnknown)
	assertRole("/tmp/work/Foundation/src/Environment_VX.cpp", roleUnknown)
}

func TestBuildGraphKeepsBusinessDriverProgressPathsAsTooling(t *testing.T) {
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{"/tmp/work/_build/meta/progress.count"}),
		recordWithProc(101, 1, []string{"gmake", "core"}, "/tmp/work/_build",
			[]string{"/tmp/work/_build/meta/progress.count", "/tmp/work/src/core.c"},
			[]string{"/tmp/work/_build/meta/progress.1", "/tmp/work/_build/core.o"}),
		recordWithProc(102, 1, []string{"ar", "rcs", "/tmp/work/_build/libfoo.a", "/tmp/work/_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/_build/core.o"},
			[]string{"/tmp/work/_build/libfoo.a"}),
		recordWithProc(103, 1, []string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/_build/libfoo.a"},
			[]string{"/tmp/work/install/lib/libfoo.a"}),
	}

	graph := buildGraphWithScope(records, trace.Scope{InstallRoot: "/tmp/work/install"})

	assertRole := func(path string, want pathRole) {
		t.Helper()
		got := graph.paths[normalizePath(path)].role
		if got != want {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, want)
		}
	}

	assertRole("/tmp/work/_build/meta/progress.count", roleTooling)
	assertRole("/tmp/work/_build/meta/progress.1", roleTooling)
	assertRole("/tmp/work/_build/core.o", rolePropagating)
	assertRole("/tmp/work/_build/libfoo.a", roleDelivery)
}

func TestPathTouchesBusinessDataIgnoresGenericBusinessActions(t *testing.T) {
	actions := []actionNode{
		{kind: kindGeneric},
		{kind: kindArchive},
	}
	business := []bool{true, true}

	progressFacts := pathFacts{
		path:    normalizePath("/tmp/work/_build/CMakeFiles/Progress/1"),
		writers: []int{0},
	}
	if pathTouchesBusinessData(actions, business, progressFacts) {
		t.Fatalf("pathTouchesBusinessData(progress) = true, want false")
	}

	objectFacts := pathFacts{
		path:    normalizePath("/tmp/work/_build/core.o"),
		writers: []int{0},
		readers: []int{1},
	}
	if !pathTouchesBusinessData(actions, business, objectFacts) {
		t.Fatalf("pathTouchesBusinessData(core.o) = false, want true")
	}
}

func TestIsToolingPathAllowsControlPlaneBusinessDriverActions(t *testing.T) {
	actions := []actionNode{
		{kind: kindGeneric},
		{kind: kindConfigure},
	}
	tooling := []bool{false, true}
	facts := pathFacts{
		path:    normalizePath("/tmp/work/_build/CMakeFiles/Progress/1"),
		writers: []int{0},
		readers: []int{1},
	}
	controlPlane := map[string]struct{}{
		normalizePath("/tmp/work/_build/CMakeFiles/Progress/1"): {},
	}
	if !isToolingPath(actions, tooling, facts, controlPlane, nil) {
		t.Fatalf("isToolingPath(progress) = false, want true")
	}
}

func TestBuildGraphClassifiesInstallLeafPathAsDelivery(t *testing.T) {
	records := []trace.Record{
		record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/build/core.o"},
			[]string{"/tmp/work/out/lib/libfoo.a"}),
		record([]string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/out/lib/libfoo.a"},
			[]string{"/tmp/work/install/lib/libfoo.a"}),
	}

	graph := buildGraph(records)
	got := graph.paths[normalizePath("/tmp/work/install/lib/libfoo.a")].role
	if got != roleDelivery {
		t.Fatalf("role(install libfoo.a) = %v, want %v", got, roleDelivery)
	}
}

func TestBuildGraphClassifiesLeafCopyAsDelivery(t *testing.T) {
	records := []trace.Record{
		record([]string{"ar", "rcs", "_build/libfoo.a", "_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/_build/core.o"},
			[]string{"/tmp/work/_build/libfoo.a"}),
		record([]string{"cp", "_build/libfoo.a", "stage/libfoo.a"}, "/tmp/work",
			[]string{"/tmp/work/_build/libfoo.a"},
			[]string{"/tmp/work/stage/libfoo.a"}),
	}

	graph := buildGraph(records)
	got := graph.paths[normalizePath("/tmp/work/stage/libfoo.a")].role
	if got != roleDelivery {
		t.Fatalf("role(stage libfoo.a) = %v, want %v", got, roleDelivery)
	}
}

func TestBuildGraphClassifiesNonDeliveredSemanticChainAsUnknown(t *testing.T) {
	records := []trace.Record{
		record([]string{"cc", "-c", "checks/arch.c", "-o", "_build/checks/arch.o"}, "/tmp/work",
			[]string{"/tmp/work/checks/arch.c"},
			[]string{"/tmp/work/_build/checks/arch.o"}),
		record([]string{"ar", "rcs", "_build/checks/libarch.a", "_build/checks/arch.o"}, "/tmp/work",
			[]string{"/tmp/work/_build/checks/arch.o"},
			[]string{"/tmp/work/_build/checks/libarch.a"}),
		record([]string{"cc", "-c", "src/core.c", "-o", "_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/src/core.c"},
			[]string{"/tmp/work/_build/core.o"}),
		record([]string{"ar", "rcs", "_build/libfoo.a", "_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/_build/core.o"},
			[]string{"/tmp/work/_build/libfoo.a"}),
		record([]string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/_build/libfoo.a"},
			[]string{"/tmp/work/install/lib/libfoo.a"}),
	}

	graph := buildGraphWithScope(records, trace.Scope{InstallRoot: "/tmp/work/install"})

	if graph.tooling[0] || graph.tooling[1] {
		t.Fatalf("side-chain actions = tooling, want false false")
	}
	if graph.business[0] || graph.business[1] {
		t.Fatalf("side-chain actions = business, want false false")
	}
	if !graph.business[2] || !graph.business[3] || !graph.business[4] {
		t.Fatalf("main delivery chain = business %v, want true true true", graph.business[2:5])
	}

	assertRole := func(path string, want pathRole) {
		t.Helper()
		got := graph.paths[normalizePath(path)].role
		if got != want {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, want)
		}
	}

	assertRole("/tmp/work/_build/checks/arch.o", roleUnknown)
	assertRole("/tmp/work/_build/checks/libarch.a", roleUnknown)
	assertRole("/tmp/work/_build/core.o", rolePropagating)
	assertRole("/tmp/work/_build/libfoo.a", roleDelivery)
	assertRole("/tmp/work/install/lib/libfoo.a", roleDelivery)
}

func TestBuildGraphClassifiesInstallStagingPathAsDelivery(t *testing.T) {
	records := []trace.Record{
		record([]string{"cc", "-c", "src/core.c", "-o", "_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/src/core.c"},
			[]string{"/tmp/work/_build/core.o"}),
		record([]string{"ar", "rcs", "_build/libfoo.a", "_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/_build/core.o"},
			[]string{"/tmp/work/_build/libfoo.a"}),
		record([]string{"python", "emit.py"}, "/tmp/work",
			[]string{"/tmp/work/_build/libfoo.a"},
			[]string{"/tmp/work/_build/install/libfoo-config.cmake"}),
		record([]string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/_build/install/libfoo-config.cmake"},
			[]string{"/tmp/work/install/lib/cmake/libfoo-config.cmake"}),
	}

	graph := buildGraphWithScope(records, trace.Scope{InstallRoot: "/tmp/work/install"})

	assertRole := func(path string, want pathRole) {
		t.Helper()
		got := graph.paths[normalizePath(path)].role
		if got != want {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, want)
		}
	}

	assertRole("/tmp/work/_build/libfoo.a", rolePropagating)
	assertRole("/tmp/work/_build/install/libfoo-config.cmake", roleDelivery)
	assertRole("/tmp/work/install/lib/cmake/libfoo-config.cmake", roleDelivery)
}

func TestBuildGraphStabilizesInstallRootInFingerprint(t *testing.T) {
	recA := record(
		[]string{"sh", "./bootstrap.sh", "--prefix=/tmp/work/out-a"},
		"/tmp/work",
		[]string{"/tmp/work/bootstrap.sh"},
		[]string{"/tmp/work/b2"},
	)
	recB := record(
		[]string{"sh", "./bootstrap.sh", "--prefix=/tmp/work/out-b"},
		"/tmp/work",
		[]string{"/tmp/work/bootstrap.sh"},
		[]string{"/tmp/work/b2"},
	)

	graphA := buildGraphWithScope([]trace.Record{recA}, trace.Scope{
		SourceRoot:  "/tmp/work",
		InstallRoot: "/tmp/work/out-a",
	})
	graphB := buildGraphWithScope([]trace.Record{recB}, trace.Scope{
		SourceRoot:  "/tmp/work",
		InstallRoot: "/tmp/work/out-b",
	})

	if got, want := graphA.actions[0].fingerprint, graphB.actions[0].fingerprint; got != want {
		t.Fatalf("fingerprint mismatch:\nA=%q\nB=%q", got, want)
	}
	if got, want := graphA.actions[0].actionKey, graphB.actions[0].actionKey; got != want {
		t.Fatalf("actionKey mismatch:\nA=%q\nB=%q", got, want)
	}
}

func TestBuildGraphTreatsTrailingSlashInstallRootAsExplicitDelivery(t *testing.T) {
	records := []trace.Record{
		record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/build/core.o"},
			[]string{"/tmp/work/out/lib/libfoo.a"}),
		record([]string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/out/lib/libfoo.a"},
			[]string{"/tmp/work/install/lib/libfoo.a"}),
	}

	graph := buildGraphWithScope(records, trace.Scope{InstallRoot: "/tmp/work/install/"})
	if got := graph.paths[normalizePath("/tmp/work/install/lib/libfoo.a")].role; got != roleDelivery {
		t.Fatalf("role(install/libfoo.a) = %v, want %v", got, roleDelivery)
	}
}

func TestBuildGraphIgnoresArchiveTempWritesInFingerprint(t *testing.T) {
	recA := record(
		[]string{"ar", "qc", "libfoo.a", "core.o"},
		"/tmp/work/_build",
		[]string{"/tmp/work/_build/core.o"},
		[]string{"/tmp/work/_build/libfoo.a", "/tmp/work/_build/stAbCd12"},
	)
	recB := record(
		[]string{"ar", "qc", "libfoo.a", "core.o"},
		"/tmp/work/_build",
		[]string{"/tmp/work/_build/core.o"},
		[]string{"/tmp/work/_build/libfoo.a", "/tmp/work/_build/stXyZ987"},
	)

	graphA := buildGraph([]trace.Record{recA})
	graphB := buildGraph([]trace.Record{recB})

	if got, want := graphA.actions[0].fingerprint, graphB.actions[0].fingerprint; got != want {
		t.Fatalf("archive fingerprint mismatch:\nA=%q\nB=%q", got, want)
	}
}

func TestBuildGraphUsesProvidedInputDigestsForCompileFingerprint(t *testing.T) {
	rec := record(
		[]string{"cc", "-I/tmp/work/_build", "-c", "/tmp/work/core.c", "-o", "/tmp/work/_build/core.o"},
		"/tmp/work",
		[]string{"/tmp/work/core.c", "/tmp/work/_build/generated.h"},
		[]string{"/tmp/work/_build/core.o"},
	)
	scope := trace.Scope{BuildRoot: "/tmp/work/_build"}

	graphA := buildGraphWithScopeAndDigests([]trace.Record{rec}, scope, map[string]string{
		"/tmp/work/_build/generated.h": "aaaaaaaaaaaaaaaa",
	})
	graphB := buildGraphWithScopeAndDigests([]trace.Record{rec}, scope, map[string]string{
		"/tmp/work/_build/generated.h": "bbbbbbbbbbbbbbbb",
	})

	if got, want := graphA.actions[0].actionKey, graphB.actions[0].actionKey; got != want {
		t.Fatalf("actionKey mismatch:\nA=%q\nB=%q", got, want)
	}
	if got, want := graphA.actions[0].fingerprint, graphB.actions[0].fingerprint; got == want {
		t.Fatalf("fingerprint unexpectedly matched:\nA=%q\nB=%q", got, want)
	}
}
