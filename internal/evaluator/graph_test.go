package evaluator

import (
	"slices"
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
}

func TestBuildGraphWithEventsTracksWriterReaderEdges(t *testing.T) {
	events := []trace.Event{
		{Seq: 1, PID: 100, Cwd: "/tmp/work", Kind: trace.EventExec, Argv: []string{"cc", "-c", "core.c", "-o", "build/core.o"}},
		{Seq: 2, PID: 100, Cwd: "/tmp/work", Kind: trace.EventRead, Path: "/tmp/work/core.c"},
		{Seq: 3, PID: 100, Cwd: "/tmp/work", Kind: trace.EventWrite, Path: "/tmp/work/build/core.o"},
		{Seq: 4, PID: 101, Cwd: "/tmp/work", Kind: trace.EventExec, Argv: []string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o"}},
		{Seq: 5, PID: 101, Cwd: "/tmp/work", Kind: trace.EventRead, Path: "/tmp/work/build/core.o"},
		{Seq: 6, PID: 101, Cwd: "/tmp/work", Kind: trace.EventWrite, Path: "/tmp/work/out/lib/libfoo.a"},
	}

	graph := buildGraphWithEvents(events)

	if graph.source != graphSourceEvents {
		t.Fatalf("graph.source = %v, want %v", graph.source, graphSourceEvents)
	}
	if graph.events != len(events) {
		t.Fatalf("graph.events = %d, want %d", graph.events, len(events))
	}
	if len(graph.actions) != 2 {
		t.Fatalf("len(graph.actions) = %d, want 2", len(graph.actions))
	}
	found := false
	for _, edge := range graph.out[0] {
		if edge.to == 1 && edge.path == normalizePath("/tmp/work/build/core.o") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing event-derived edge 0 -> 1 via %s", normalizePath("/tmp/work/build/core.o"))
	}
}

func TestBuildRawGraphSeparatesRawPathsFromRoleProjection(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/_build",
	}
	records := []trace.Record{
		record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{"/tmp/work/_build/CMakeCache.txt"}),
		record([]string{"cc", "-c", "core.c", "-o", "_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/core.c", "/tmp/work/_build/CMakeCache.txt"},
			[]string{"/tmp/work/_build/core.o"}),
	}

	observation := buildObservationFromRecords(records, nil)
	raw := buildRawGraphFromObservation(observation, scope)
	cachePath := normalizePath("/tmp/work/_build/CMakeCache.txt")
	if raw.paths != nil {
		t.Fatalf("raw.paths = %v, want nil before role projection", raw.paths)
	}
	rawFacts, ok := raw.rawPaths[cachePath]
	if !ok {
		t.Fatalf("raw.rawPaths missing %q", cachePath)
	}
	if len(rawFacts.writers) != 1 || len(rawFacts.readers) != 1 {
		t.Fatalf("raw.rawPaths[%q] = %+v, want one writer and one reader", cachePath, rawFacts)
	}

	projected := classifyGraphRoles(raw)
	projectedFacts, ok := projected.paths[cachePath]
	if !ok {
		t.Fatalf("projected.paths missing %q", cachePath)
	}
	projection := deriveGraphRoleProjection(raw)
	projectionFacts, ok := projection.paths[cachePath]
	if !ok {
		t.Fatalf("projection.paths missing %q", cachePath)
	}
	if !slices.Equal(projectionFacts.writers, rawFacts.writers) || !slices.Equal(projectionFacts.readers, rawFacts.readers) {
		t.Fatalf("projection facts = %+v, want same writer/reader topology as raw %+v", projectionFacts, rawFacts)
	}
	if !slices.Equal(projectedFacts.writers, rawFacts.writers) || !slices.Equal(projectedFacts.readers, rawFacts.readers) {
		t.Fatalf("projected facts = %+v, want same writer/reader topology as raw %+v", projectedFacts, rawFacts)
	}
	if projectedFacts.role != projectionFacts.role {
		t.Fatalf("projected.paths[%q].role = %v, want %v from deriveGraphRoleProjection()", cachePath, projectedFacts.role, projectionFacts.role)
	}
	built := buildGraphWithScope(records, scope)
	builtFacts, ok := built.paths[cachePath]
	if !ok {
		t.Fatalf("buildGraphWithScope().paths missing %q", cachePath)
	}
	if projectedFacts.role != builtFacts.role {
		t.Fatalf("projected.paths[%q].role = %v, want %v from buildGraphWithScope()", cachePath, projectedFacts.role, builtFacts.role)
	}
}

func TestBuildPathSSATreatsEventUnlinkAsTombstoneDef(t *testing.T) {
	events := []trace.Event{
		{Seq: 1, PID: 100, Cwd: "/tmp/work", Kind: trace.EventExec, Argv: []string{"rm", "-f", "build/api.h"}},
		{Seq: 2, PID: 100, Cwd: "/tmp/work", Kind: trace.EventUnlink, Path: "/tmp/work/build/api.h"},
		{Seq: 3, PID: 101, Cwd: "/tmp/work", Kind: trace.EventExec, Argv: []string{"cc", "-c", "server.c", "-o", "build/server.o"}},
		{Seq: 4, PID: 101, Cwd: "/tmp/work", Kind: trace.EventRead, Path: "/tmp/work/build/api.h"},
		{Seq: 5, PID: 101, Cwd: "/tmp/work", Kind: trace.EventWrite, Path: "/tmp/work/build/server.o"},
	}

	graph := buildGraphWithEvents(events)
	ssa := buildPathSSA(graph)
	if len(ssa.actionWrites) < 2 {
		t.Fatalf("len(ssa.actionWrites) = %d, want >= 2", len(ssa.actionWrites))
	}
	if len(ssa.actionWrites[0]) != 1 {
		t.Fatalf("ssa.actionWrites[0] = %v, want single tombstone write", ssa.actionWrites[0])
	}
	tombstone := ssa.actionWrites[0][0]
	if !tombstone.tombstone {
		t.Fatalf("ssa.actionWrites[0][0].tombstone = false, want true")
	}
	if tombstone.path != normalizePath("/tmp/work/build/api.h") {
		t.Fatalf("ssa.actionWrites[0][0].path = %q, want %q", tombstone.path, normalizePath("/tmp/work/build/api.h"))
	}
	if len(ssa.actionReads[1]) != 1 {
		t.Fatalf("ssa.actionReads[1] = %v, want single tombstone-backed read", ssa.actionReads[1])
	}
	if len(ssa.actionReads[1][0].defs) != 1 || ssa.actionReads[1][0].defs[0] != tombstone {
		t.Fatalf("ssa.actionReads[1][0].defs = %v, want %v", ssa.actionReads[1][0].defs, tombstone)
	}
}

func TestBuildPathSSATreatsRenameSourceAsTombstoneDef(t *testing.T) {
	events := []trace.Event{
		{Seq: 1, PID: 100, Cwd: "/tmp/work", Kind: trace.EventExec, Argv: []string{"mv", "build/api.h", "build/api-renamed.h"}},
		{Seq: 2, PID: 100, Cwd: "/tmp/work", Kind: trace.EventRename, RelatedPath: "/tmp/work/build/api.h", Path: "/tmp/work/build/api-renamed.h"},
		{Seq: 3, PID: 101, Cwd: "/tmp/work", Kind: trace.EventExec, Argv: []string{"cc", "-c", "server.c", "-o", "build/server.o"}},
		{Seq: 4, PID: 101, Cwd: "/tmp/work", Kind: trace.EventRead, Path: "/tmp/work/build/api.h"},
		{Seq: 5, PID: 101, Cwd: "/tmp/work", Kind: trace.EventWrite, Path: "/tmp/work/build/server.o"},
	}

	graph := buildGraphWithEvents(events)
	ssa := buildPathSSA(graph)
	if len(ssa.actionWrites[0]) != 2 {
		t.Fatalf("ssa.actionWrites[0] = %v, want rename source tombstone plus destination write", ssa.actionWrites[0])
	}
	var sourceDef pathSSADef
	foundSource := false
	foundDest := false
	for _, def := range ssa.actionWrites[0] {
		switch def.path {
		case normalizePath("/tmp/work/build/api.h"):
			sourceDef = def
			foundSource = true
			if !def.tombstone {
				t.Fatalf("rename source def.tombstone = false, want true")
			}
		case normalizePath("/tmp/work/build/api-renamed.h"):
			foundDest = true
			if def.tombstone {
				t.Fatalf("rename destination def.tombstone = true, want false")
			}
		}
	}
	if !foundSource || !foundDest {
		t.Fatalf("ssa.actionWrites[0] = %v, want both source and destination defs", ssa.actionWrites[0])
	}
	if len(ssa.actionReads[1]) != 1 || len(ssa.actionReads[1][0].defs) != 1 || ssa.actionReads[1][0].defs[0] != sourceDef {
		t.Fatalf("ssa.actionReads[1] = %v, want rename-source tombstone binding", ssa.actionReads[1])
	}
}

func TestBuildGraphClassifiesNoiseActionsAndKeys(t *testing.T) {
	records := []trace.Record{
		record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
		record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work", []string{"/tmp/work/CMakeLists.txt"}, []string{"/tmp/work/_build/CMakeCache.txt"}),
		record([]string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work", []string{"/tmp/work/_build/libfoo.a"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		record([]string{"cp", "libfoo.a", "out/lib/libfoo.a"}, "/tmp/work", []string{"/tmp/work/libfoo.a"}, []string{"/tmp/work/out/lib/libfoo.a"}),
	}

	graph := buildGraph(records)

	if graph.actions[0].kind != kindGeneric {
		t.Fatalf("generic action kind = %v, want %v", graph.actions[0].kind, kindGeneric)
	}
	wantGenericKey := "generic|cc|cwd=" + normalizePath("/tmp/work") + "|argv=cc -c core.c -o build/core.o"
	if got := graph.actions[0].actionKey; got != wantGenericKey {
		t.Fatalf("generic actionKey = %q, want %q", got, wantGenericKey)
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

func TestBuildGraphClassifiesPerlConfigureAndShellInstall(t *testing.T) {
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/out",
	}
	records := []trace.Record{
		record(
			[]string{"perl", "configdata.pm"},
			"/tmp/work",
			[]string{"/tmp/work/configdata.pm", "/tmp/work/Makefile.in"},
			[]string{"/tmp/work/Makefile.new", "/tmp/work/Makefile"},
		),
		record(
			[]string{"sh", "-c", "cp apps/openssl /tmp/work/out/bin/openssl.new && mv -f /tmp/work/out/bin/openssl.new /tmp/work/out/bin/openssl"},
			"/tmp/work",
			[]string{"/tmp/work/apps/openssl"},
			[]string{"/tmp/work/out/bin/openssl.new", "/tmp/work/out/bin/openssl"},
		),
	}

	graph := buildGraphWithScope(records, scope)

	if graph.actions[0].kind != kindConfigure {
		t.Fatalf("perl configure kind = %v, want %v", graph.actions[0].kind, kindConfigure)
	}
	if graph.actions[1].kind != kindInstall {
		t.Fatalf("shell install kind = %v, want %v", graph.actions[1].kind, kindInstall)
	}
}

func TestBuildGraphKeepsArtifactWritingShellGeneric(t *testing.T) {
	graph := buildGraph([]trace.Record{
		record(
			[]string{"sh", "-c", "cc -c foo.c -o build/foo.o"},
			"/tmp/work",
			[]string{"/tmp/work/foo.c"},
			[]string{"/tmp/work/build/foo.o"},
		),
	})

	if graph.actions[0].kind != kindGeneric {
		t.Fatalf("shell artifact writer kind = %v, want %v", graph.actions[0].kind, kindGeneric)
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

	for _, dir := range []string{"/tmp/work/include", "/tmp/work/build", "/tmp/work/_build"} {
		if _, ok := graph.paths[normalizePath(dir)]; ok {
			t.Fatalf("directory path %q unexpectedly retained", normalizePath(dir))
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
			[]string{"/tmp/work/_build/meta/status.txt", "/tmp/work/_build/meta/cache.db"}),
	}

	graph := buildGraph(records)

	for i := range graph.actions {
		if !graph.tooling[i] {
			t.Fatalf("graph.tooling[%d] = false, want true; tooling=%v action=%+v b2=%+v", i, graph.tooling, graph.actions[i], graph.paths[normalizePath("/tmp/work/b2")])
		}
	}
	for _, path := range []string{"/tmp/work/tools/b2", "/tmp/work/_build/meta/status.txt", "/tmp/work/_build/meta/cache.db"} {
		if got := graph.paths[normalizePath(path)].role; got != roleTooling {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, roleTooling)
		}
	}
}

func TestBuildGraphClassifiesCopiedProducedExecutableChainAsTooling(t *testing.T) {
	records := []trace.Record{
		record([]string{"sh", "bootstrap.sh"}, "/tmp/work",
			[]string{"/tmp/work/bootstrap.sh"},
			[]string{"/tmp/work/tools/build/src/engine/b2"}),
		record([]string{"cp", "./tools/build/src/engine/b2", "./b2"}, "/tmp/work",
			[]string{"/tmp/work/tools/build/src/engine/b2"},
			[]string{"/tmp/work/b2"}),
		record([]string{"./b2", "headers"}, "/tmp/work",
			[]string{"/tmp/work/project-config.jam"},
			[]string{"/tmp/work/_build/meta/status.txt", "/tmp/work/_build/meta/cache.db"}),
	}

	graph := buildGraph(records)

	for i := range graph.actions {
		if !graph.tooling[i] {
			t.Fatalf("graph.tooling[%d] = false, want true", i)
		}
	}
	for _, path := range []string{
		"/tmp/work/tools/build/src/engine/b2",
		"/tmp/work/b2",
		"/tmp/work/_build/meta/status.txt",
		"/tmp/work/_build/meta/cache.db",
	} {
		if got := graph.paths[normalizePath(path)].role; got != roleTooling {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, roleTooling)
		}
	}
}

func TestBuildGraphClassifiesCopiedProducedExecutableLeafAsTooling(t *testing.T) {
	records := []trace.Record{
		record([]string{"sh", "bootstrap.sh"}, "/tmp/work",
			[]string{"/tmp/work/bootstrap.sh"},
			[]string{"/tmp/work/tools/build/src/engine/b2"}),
		record([]string{"cp", "./tools/build/src/engine/b2", "./b2"}, "/tmp/work",
			[]string{"/tmp/work/tools/build/src/engine/b2"},
			[]string{"/tmp/work/b2"}),
	}

	graph := buildGraph(records)

	if !graph.tooling[0] {
		t.Fatalf("graph.tooling[0] = false, want true")
	}
	if got := graph.paths[normalizePath("/tmp/work/tools/build/src/engine/b2")].role; got != roleTooling {
		t.Fatalf("role(%s) = %v, want %v", normalizePath("/tmp/work/tools/build/src/engine/b2"), got, roleTooling)
	}
}

func TestBuildGraphKeepsConfigureControlPlanePathsAsTooling(t *testing.T) {
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{"/tmp/work/_build/meta/config.state", "/tmp/work/_build/meta/progress.count"}),
		recordWithProc(101, 100, []string{"cmake", "-E", "echo", "progress"}, "/tmp/work",
			[]string{"/tmp/work/_build/meta/progress.count"},
			[]string{"/tmp/work/_build/meta/progress.1"}),
	}

	graph := buildGraph(records)

	for _, path := range []string{"/tmp/work/_build/meta/config.state", "/tmp/work/_build/meta/progress.count", "/tmp/work/_build/meta/progress.1"} {
		if got := graph.paths[normalizePath(path)].role; got != roleTooling {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, roleTooling)
		}
	}
}

func TestBuildGraphPromotesProbeIslandChildrenToTooling(t *testing.T) {
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{"/tmp/work/_build/probe-checks/CheckFeature.c"}),
		recordWithProc(101, 100, []string{"cc", "-c", "CheckFeature.c", "-o", "CheckFeature.c.o"}, "/tmp/work/_build/probe-checks",
			[]string{"/tmp/work/_build/probe-checks/CheckFeature.c", "/usr/include/stdio.h"},
			[]string{"/tmp/work/_build/probe-checks/CheckFeature.c.o"}),
		recordWithProc(102, 101, []string{"cc", "CheckFeature.c.o", "-o", "probe-check"}, "/tmp/work/_build/probe-checks",
			[]string{"/tmp/work/_build/probe-checks/CheckFeature.c.o"},
			[]string{"/tmp/work/_build/probe-checks/probe-check"}),
		recordWithProc(200, 2, []string{"cc", "-c", "/tmp/work/src/core.c", "-o", "/tmp/work/_build/core.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/src/core.c", "/usr/include/stdio.h"},
			[]string{"/tmp/work/_build/core.o"}),
	}

	graph := buildGraph(records)

	for _, path := range []string{
		"/tmp/work/_build/probe-checks/CheckFeature.c",
		"/tmp/work/_build/probe-checks/CheckFeature.c.o",
		"/tmp/work/_build/probe-checks/probe-check",
	} {
		if got := graph.paths[normalizePath(path)].role; got != roleTooling {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, roleTooling)
		}
	}
	if got := graph.paths[normalizePath("/tmp/work/_build/core.o")].role; got != rolePropagating {
		t.Fatalf("role(core.o) = %v, want %v", got, rolePropagating)
	}
}

func TestBuildGraphPromotesProbeIslandChildrenWrappedByGenericMake(t *testing.T) {
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{"/tmp/work/_build/probe-checks/CheckFeature.c", "/tmp/work/_build/probe-checks/Makefile"}),
		recordWithProc(101, 100, []string{"gmake", "-f", "Makefile"}, "/tmp/work/_build/probe-checks",
			[]string{"/tmp/work/_build/probe-checks/Makefile"},
			nil),
		recordWithProc(102, 101, []string{"cc", "-c", "CheckFeature.c", "-o", "CheckFeature.c.o"}, "/tmp/work/_build/probe-checks",
			[]string{"/tmp/work/_build/probe-checks/CheckFeature.c", "/usr/include/stdio.h"},
			[]string{"/tmp/work/_build/probe-checks/CheckFeature.c.o"}),
		recordWithProc(103, 101, []string{"cc", "CheckFeature.c.o", "-o", "probe-check"}, "/tmp/work/_build/probe-checks",
			[]string{"/tmp/work/_build/probe-checks/CheckFeature.c.o"},
			[]string{"/tmp/work/_build/probe-checks/probe-check"}),
		recordWithProc(200, 2, []string{"cc", "-c", "/tmp/work/src/core.c", "-o", "/tmp/work/_build/core.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/src/core.c", "/usr/include/stdio.h"},
			[]string{"/tmp/work/_build/core.o"}),
	}

	graph := buildGraph(records)

	for _, path := range []string{
		"/tmp/work/_build/probe-checks/CheckFeature.c",
		"/tmp/work/_build/probe-checks/Makefile",
		"/tmp/work/_build/probe-checks/CheckFeature.c.o",
		"/tmp/work/_build/probe-checks/probe-check",
	} {
		if got := graph.paths[normalizePath(path)].role; got != roleTooling {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, roleTooling)
		}
	}
	if got := graph.paths[normalizePath("/tmp/work/_build/core.o")].role; got != rolePropagating {
		t.Fatalf("role(core.o) = %v, want %v", got, rolePropagating)
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

	graph := buildGraphWithScope(records, trace.Scope{InstallRoot: "/tmp/work/install"})
	if got := graph.paths[normalizePath("/tmp/work/install/lib/libfoo.a")].role; got != roleDelivery {
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
	if got := graph.paths[normalizePath("/tmp/work/stage/libfoo.a")].role; got != roleDelivery {
		t.Fatalf("role(stage libfoo.a) = %v, want %v", got, roleDelivery)
	}
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
