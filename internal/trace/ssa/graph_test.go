package ssa

import (
	"testing"

	"github.com/goplus/llar/internal/trace"
)

func record(argv []string, cwd string, inputs, changes []string) trace.Record {
	return trace.Record{
		Argv:    argv,
		Cwd:     cwd,
		Inputs:  inputs,
		Changes: changes,
	}
}

func recordWithProc(pid, parentPID int64, argv []string, cwd string, inputs, changes []string) trace.Record {
	rec := record(argv, cwd, inputs, changes)
	rec.PID = pid
	rec.ParentPID = parentPID
	return rec
}

func traceoptionsEventTrace(apiOn bool) []trace.Event {
	return traceoptionsMatrixEventTrace(apiOn, false, false)
}

func traceoptionsTryCompileProbeEventTrace(apiOn bool, trySuffix, cmTC string) []trace.Event {
	configureArgv := []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}
	if apiOn {
		configureArgv = append(configureArgv, "-DTRACE_FEATURE_API=ON")
	}
	compileArgv := []string{
		"/usr/bin/cc",
		"-I/tmp/work",
		"-I/tmp/work/_build",
		"-o", "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o",
		"-c", "/tmp/work/core.c",
	}
	if apiOn {
		compileArgv = []string{
			"/usr/bin/cc",
			"-DTRACE_FEATURE_API",
			"-I/tmp/work",
			"-I/tmp/work/_build",
			"-o", "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o",
			"-c", "/tmp/work/core.c",
		}
	}
	tryDir := "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-" + trySuffix
	objectPath := tryDir + "/CMakeFiles/" + cmTC + ".dir/CheckIncludeFile.c.o"
	execPath := tryDir + "/" + cmTC
	replyPath := tryDir + "/.cmake/api/v1/reply"

	events := make([]trace.Event, 0, 32)
	seq := int64(1)
	addExec := func(pid, parent int64, cwd string, argv ...string) {
		events = append(events, trace.Event{Seq: seq, PID: pid, ParentPID: parent, Cwd: cwd, Kind: trace.EventExec, Argv: argv})
		seq++
	}
	addRead := func(pid int64, cwd, path string) {
		events = append(events, trace.Event{Seq: seq, PID: pid, Cwd: cwd, Kind: trace.EventRead, Path: path})
		seq++
	}
	addWrite := func(pid int64, cwd, path string) {
		events = append(events, trace.Event{Seq: seq, PID: pid, Cwd: cwd, Kind: trace.EventWrite, Path: path})
		seq++
	}

	addExec(100, 1, "/tmp/work", configureArgv...)
	addRead(100, "/tmp/work", "/tmp/work/CMakeLists.txt")
	addWrite(100, "/tmp/work", tryDir+"/CheckIncludeFile.c")

	addExec(110, 100, tryDir, "/usr/bin/cc", "-o", "CMakeFiles/"+cmTC+".dir/CheckIncludeFile.c.o", "-c", tryDir+"/CheckIncludeFile.c")
	addRead(110, tryDir, tryDir+"/CheckIncludeFile.c")
	addWrite(110, tryDir, objectPath)

	addExec(111, 110, tryDir, "/usr/bin/ld", "-o", cmTC, "CMakeFiles/"+cmTC+".dir/CheckIncludeFile.c.o")
	addRead(111, tryDir, objectPath)
	addWrite(111, tryDir, execPath)

	addExec(112, 110, tryDir, "/usr/bin/cmake", "-E", "echo", "reply")
	addRead(112, tryDir, execPath)
	addWrite(112, tryDir, replyPath)

	addRead(100, "/tmp/work", execPath)
	addRead(100, "/tmp/work", replyPath)
	addRead(100, "/tmp/work", "/tmp/work/trace_options.h.in")
	addWrite(100, "/tmp/work", "/tmp/work/_build/trace_options.h")
	addWrite(100, "/tmp/work", "/tmp/work/_build/cmake_install.cmake")

	addExec(200, 1, "/tmp/work", "cmake", "--build", "/tmp/work/_build", "--config", "Release")
	addExec(201, 200, "/tmp/work/_build", compileArgv...)
	addRead(201, "/tmp/work/_build", "/tmp/work/core.c")
	addRead(201, "/tmp/work/_build", "/tmp/work/trace.h")
	addRead(201, "/tmp/work/_build", "/tmp/work/_build/trace_options.h")
	addWrite(201, "/tmp/work/_build", "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o")

	addExec(202, 200, "/tmp/work/_build", "/usr/bin/ar", "qc", "/tmp/work/_build/libtracecore.a", "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o")
	addRead(202, "/tmp/work/_build", "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o")
	addWrite(202, "/tmp/work/_build", "/tmp/work/_build/libtracecore.a")

	addExec(300, 1, "/tmp/work", "cmake", "--install", "/tmp/work/_build")
	addRead(300, "/tmp/work", "/tmp/work/_build/cmake_install.cmake")
	addRead(300, "/tmp/work", "/tmp/work/_build/libtracecore.a")
	addRead(300, "/tmp/work", "/tmp/work/_build/trace_options.h")
	addWrite(300, "/tmp/work", "/tmp/work/install/lib/libtracecore.a")
	addWrite(300, "/tmp/work", "/tmp/work/install/include/trace_options.h")

	return events
}

func traceoptionsMatrixEventTrace(apiOn, cliOn, shipOn bool) []trace.Event {
	configureArgv := []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}
	if apiOn {
		configureArgv = append(configureArgv, "-DTRACE_FEATURE_API=ON")
	}
	if cliOn {
		configureArgv = append(configureArgv, "-DTRACE_BUILD_CLI=ON")
	}
	if shipOn {
		configureArgv = append(configureArgv, "-DTRACE_INSTALL_ALIAS=ON")
	}
	compileArgv := []string{
		"/usr/bin/cc",
		"-I/tmp/work",
		"-I/tmp/work/_build",
		"-o", "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o",
		"-c", "/tmp/work/core.c",
	}
	if apiOn {
		compileArgv = []string{
			"/usr/bin/cc",
			"-DTRACE_FEATURE_API",
			"-I/tmp/work",
			"-I/tmp/work/_build",
			"-o", "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o",
			"-c", "/tmp/work/core.c",
		}
	}
	arTemp, ranlibTemp := traceoptionsArchiveTemps(apiOn, cliOn, shipOn)
	events := make([]trace.Event, 0, 32)
	seq := int64(1)
	addExec := func(pid, parent int64, cwd string, argv ...string) {
		events = append(events, trace.Event{Seq: seq, PID: pid, ParentPID: parent, Cwd: cwd, Kind: trace.EventExec, Argv: argv})
		seq++
	}
	addRead := func(pid int64, cwd, path string) {
		events = append(events, trace.Event{Seq: seq, PID: pid, Cwd: cwd, Kind: trace.EventRead, Path: path})
		seq++
	}
	addWrite := func(pid int64, cwd, path string) {
		events = append(events, trace.Event{Seq: seq, PID: pid, Cwd: cwd, Kind: trace.EventWrite, Path: path})
		seq++
	}

	addExec(100, 1, "/tmp/work", configureArgv...)
	addRead(100, "/tmp/work", "/tmp/work/CMakeLists.txt")
	addRead(100, "/tmp/work", "/tmp/work/trace_options.h.in")
	addRead(100, "/tmp/work", "/tmp/work/_build/CMakeFiles/pkgRedirects")
	addRead(100, "/tmp/work", "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects")
	addWrite(100, "/tmp/work", "/tmp/work/_build/trace_options.h")
	addWrite(100, "/tmp/work", "/tmp/work/_build/CMakeFiles/pkgRedirects")
	addWrite(100, "/tmp/work", "/tmp/work/_build/cmake_install.cmake")

	addExec(110, 100, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc", "/usr/bin/gmake", "-f", "Makefile", "cmTC_deadbeef/fast")
	addRead(110, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc", "/tmp/work/_build/CMakeFiles/pkgRedirects")
	addRead(110, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc", "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/Makefile")
	addExec(111, 110, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc", "/usr/bin/cc", "-o", "CMakeFiles/cmTC_deadbeef.dir/CheckIncludeFile.c.o", "-c", "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CheckIncludeFile.c")
	addRead(111, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc", "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CheckIncludeFile.c")
	addWrite(111, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc", "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects")
	addWrite(111, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc", "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/cmTC_deadbeef")

	addExec(200, 1, "/tmp/work", "cmake", "--build", "/tmp/work/_build", "--config", "Release")
	addRead(200, "/tmp/work", "/tmp/work/_build/CMakeCache.txt")
	addExec(201, 200, "/tmp/work/_build", "/usr/bin/gmake", "-f", "Makefile")
	addRead(201, "/tmp/work/_build", "/tmp/work/_build/Makefile")
	addExec(202, 201, "/tmp/work/_build", "/usr/bin/gmake", "-s", "-f", "CMakeFiles/tracecore.dir/build.make", "CMakeFiles/tracecore.dir/build")
	addRead(202, "/tmp/work/_build", "/tmp/work/_build/CMakeFiles/tracecore.dir/build.make")
	addExec(203, 202, "/tmp/work/_build", compileArgv...)
	addRead(203, "/tmp/work/_build", "/tmp/work/core.c")
	addRead(203, "/tmp/work/_build", "/tmp/work/trace.h")
	addRead(203, "/tmp/work/_build", "/tmp/work/_build/trace_options.h")
	addWrite(203, "/tmp/work/_build", "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o")
	addExec(204, 202, "/tmp/work/_build", "/usr/bin/ar", "qc", "/tmp/work/_build/libtracecore.a", "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o")
	addRead(204, "/tmp/work/_build", "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o")
	addWrite(204, "/tmp/work/_build", "/tmp/work/_build/libtracecore.a")
	addWrite(204, "/tmp/work/_build", "/tmp/work/_build/"+arTemp)
	addExec(205, 202, "/tmp/work/_build", "/usr/bin/ranlib", "/tmp/work/_build/libtracecore.a")
	addRead(205, "/tmp/work/_build", "/tmp/work/_build/libtracecore.a")
	addWrite(205, "/tmp/work/_build", "/tmp/work/_build/"+ranlibTemp)
	addWrite(205, "/tmp/work/_build", "/tmp/work/_build/libtracecore.a")

	if cliOn {
		addExec(206, 201, "/tmp/work/_build", "/usr/bin/gmake", "-s", "-f", "CMakeFiles/tracecli.dir/build.make", "CMakeFiles/tracecli.dir/build")
		addRead(206, "/tmp/work/_build", "/tmp/work/_build/CMakeFiles/tracecli.dir/build.make")
		addExec(207, 206, "/tmp/work/_build", "/usr/bin/cc", "/tmp/work/cli.c", "/tmp/work/_build/libtracecore.a", "-o", "/tmp/work/_build/tracecli")
		addRead(207, "/tmp/work/_build", "/tmp/work/cli.c")
		addRead(207, "/tmp/work/_build", "/tmp/work/_build/libtracecore.a")
		addWrite(207, "/tmp/work/_build", "/tmp/work/_build/tracecli")
	}

	addExec(300, 1, "/tmp/work", "cmake", "--install", "/tmp/work/_build", "--prefix", "/tmp/work/install")
	addRead(300, "/tmp/work", "/tmp/work/_build/cmake_install.cmake")
	addRead(300, "/tmp/work", "/tmp/work/_build/libtracecore.a")
	addRead(300, "/tmp/work", "/tmp/work/trace.h")
	addRead(300, "/tmp/work", "/tmp/work/_build/trace_options.h")
	if cliOn {
		addRead(300, "/tmp/work", "/tmp/work/_build/tracecli")
	}
	addWrite(300, "/tmp/work", "/tmp/work/install/lib/libtracecore.a")
	addWrite(300, "/tmp/work", "/tmp/work/install/include/trace.h")
	addWrite(300, "/tmp/work", "/tmp/work/install/include/trace_options.h")
	addWrite(300, "/tmp/work", "/tmp/work/_build/install_manifest.txt")
	if cliOn {
		addWrite(300, "/tmp/work", "/tmp/work/install/bin/tracecli")
	}
	if shipOn {
		addWrite(300, "/tmp/work", "/tmp/work/install/include/trace_alias.h")
	}
	return events
}

func traceoptionsArchiveTemps(apiOn, cliOn, shipOn bool) (string, string) {
	switch {
	case apiOn:
		return "stCXhzz0", "stQugPSt"
	case cliOn:
		return "stizVeGp", "stiZkO0K"
	case shipOn:
		return "stNMeD5X", "stdwbVyX"
	default:
		return "stNjnHgT", "stvgaB7q"
	}
}

func projectedPathRole(graph Graph, roles RoleProjection, path string) PathRole {
	path = normalizePath(path)
	if PathLooksDelivery(graph, path) {
		return RoleDelivery
	}
	if !ImpactPathAllowed(graph, roles, path) {
		return RoleTooling
	}
	return RolePropagating
}

func TestBuildGraphTracksWriterReaderEdges(t *testing.T) {
	records := []trace.Record{
		record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
		record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		record([]string{"cc", "-c", "cli.c", "-o", "build/cli.o"}, "/tmp/work", []string{"/tmp/work/cli.c"}, []string{"/tmp/work/build/cli.o"}),
		record([]string{"cc", "build/cli.o", "out/lib/libfoo.a", "-o", "out/bin/foo"}, "/tmp/work", []string{"/tmp/work/build/cli.o", "/tmp/work/out/lib/libfoo.a"}, []string{"/tmp/work/out/bin/foo"}),
	}

	graph := BuildGraph(BuildInput{Records: records})

	assertEdge := func(from, to int, path string) {
		t.Helper()
		for _, edge := range graph.Out[from] {
			if edge.To == to && edge.Path == normalizePath(path) {
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

	graph := BuildGraph(BuildInput{Events: events})

	if graph.Source != SourceEvents {
		t.Fatalf("graph.Source = %v, want %v", graph.Source, SourceEvents)
	}
	if graph.Events != len(events) {
		t.Fatalf("graph.Events = %d, want %d", graph.Events, len(events))
	}
	if len(graph.Actions) != 2 {
		t.Fatalf("len(graph.Actions) = %d, want 2", len(graph.Actions))
	}
	found := false
	for _, edge := range graph.Out[0] {
		if edge.To == 1 && edge.Path == normalizePath("/tmp/work/build/core.o") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing event-derived edge 0 -> 1 via %s", normalizePath("/tmp/work/build/core.o"))
	}
}

func TestBuildGraphTreatsEventUnlinkAsTombstoneDef(t *testing.T) {
	events := []trace.Event{
		{Seq: 1, PID: 100, Cwd: "/tmp/work", Kind: trace.EventExec, Argv: []string{"rm", "-f", "build/api.h"}},
		{Seq: 2, PID: 100, Cwd: "/tmp/work", Kind: trace.EventUnlink, Path: "/tmp/work/build/api.h"},
		{Seq: 3, PID: 101, Cwd: "/tmp/work", Kind: trace.EventExec, Argv: []string{"cc", "-c", "server.c", "-o", "build/server.o"}},
		{Seq: 4, PID: 101, Cwd: "/tmp/work", Kind: trace.EventRead, Path: "/tmp/work/build/api.h"},
		{Seq: 5, PID: 101, Cwd: "/tmp/work", Kind: trace.EventWrite, Path: "/tmp/work/build/server.o"},
	}

	graph := BuildGraph(BuildInput{Events: events})
	if len(graph.ActionWrites) < 2 {
		t.Fatalf("len(graph.ActionWrites) = %d, want >= 2", len(graph.ActionWrites))
	}
	if len(graph.ActionWrites[0]) != 1 {
		t.Fatalf("graph.ActionWrites[0] = %v, want single tombstone write", graph.ActionWrites[0])
	}
	tombstone := graph.ActionWrites[0][0]
	if !tombstone.Tombstone {
		t.Fatalf("graph.ActionWrites[0][0].Tombstone = false, want true")
	}
	if tombstone.Path != normalizePath("/tmp/work/build/api.h") {
		t.Fatalf("graph.ActionWrites[0][0].Path = %q, want %q", tombstone.Path, normalizePath("/tmp/work/build/api.h"))
	}
	if len(graph.ActionReads[1]) != 1 {
		t.Fatalf("graph.ActionReads[1] = %v, want single tombstone-backed read", graph.ActionReads[1])
	}
	if len(graph.ActionReads[1][0].Defs) != 1 || graph.ActionReads[1][0].Defs[0] != tombstone {
		t.Fatalf("graph.ActionReads[1][0].Defs = %v, want %v", graph.ActionReads[1][0].Defs, tombstone)
	}
}

func TestBuildGraphTreatsRenameSourceAsTombstoneDef(t *testing.T) {
	events := []trace.Event{
		{Seq: 1, PID: 100, Cwd: "/tmp/work", Kind: trace.EventExec, Argv: []string{"mv", "build/api.h", "build/api-renamed.h"}},
		{Seq: 2, PID: 100, Cwd: "/tmp/work", Kind: trace.EventRename, RelatedPath: "/tmp/work/build/api.h", Path: "/tmp/work/build/api-renamed.h"},
		{Seq: 3, PID: 101, Cwd: "/tmp/work", Kind: trace.EventExec, Argv: []string{"cc", "-c", "server.c", "-o", "build/server.o"}},
		{Seq: 4, PID: 101, Cwd: "/tmp/work", Kind: trace.EventRead, Path: "/tmp/work/build/api.h"},
		{Seq: 5, PID: 101, Cwd: "/tmp/work", Kind: trace.EventWrite, Path: "/tmp/work/build/server.o"},
	}

	graph := BuildGraph(BuildInput{Events: events})
	if len(graph.ActionWrites[0]) != 2 {
		t.Fatalf("graph.ActionWrites[0] = %v, want rename source tombstone plus destination write", graph.ActionWrites[0])
	}
	var sourceDef PathState
	foundSource := false
	foundDest := false
	for _, def := range graph.ActionWrites[0] {
		switch def.Path {
		case normalizePath("/tmp/work/build/api.h"):
			sourceDef = def
			foundSource = true
			if !def.Tombstone {
				t.Fatalf("rename source Tombstone = false, want true")
			}
		case normalizePath("/tmp/work/build/api-renamed.h"):
			foundDest = true
			if def.Tombstone {
				t.Fatalf("rename destination Tombstone = true, want false")
			}
		}
	}
	if !foundSource || !foundDest {
		t.Fatalf("graph.ActionWrites[0] = %v, want both source and destination defs", graph.ActionWrites[0])
	}
	if len(graph.ActionReads[1]) != 1 || len(graph.ActionReads[1][0].Defs) != 1 || graph.ActionReads[1][0].Defs[0] != sourceDef {
		t.Fatalf("graph.ActionReads[1] = %v, want rename-source tombstone binding", graph.ActionReads[1])
	}
}

func TestBuildGraphBindsReadMissToMissingState(t *testing.T) {
	events := []trace.Event{
		{Seq: 1, PID: 100, Cwd: "/tmp/work", Kind: trace.EventExec, Argv: []string{"cc", "-c", "server.c", "-o", "build/server.o"}},
		{Seq: 2, PID: 100, Cwd: "/tmp/work", Kind: trace.EventReadMiss, Path: "/tmp/work/build/api.h"},
		{Seq: 3, PID: 100, Cwd: "/tmp/work", Kind: trace.EventRead, Path: "/tmp/work/server.c"},
		{Seq: 4, PID: 100, Cwd: "/tmp/work", Kind: trace.EventWrite, Path: "/tmp/work/build/server.o"},
	}

	graph := BuildGraph(BuildInput{Events: events})
	if len(graph.Actions) != 1 {
		t.Fatalf("len(graph.Actions) = %d, want 1", len(graph.Actions))
	}
	if got := graph.Actions[0].ReadMisses; len(got) != 1 || got[0] != normalizePath("/tmp/work/build/api.h") {
		t.Fatalf("ReadMisses = %v, want [%q]", got, normalizePath("/tmp/work/build/api.h"))
	}
	if len(graph.ActionReads[0]) != 2 {
		t.Fatalf("graph.ActionReads[0] = %v, want 2 bindings", graph.ActionReads[0])
	}
	var missing Read
	found := false
	for _, binding := range graph.ActionReads[0] {
		if binding.Path != normalizePath("/tmp/work/build/api.h") {
			continue
		}
		missing = binding
		found = true
	}
	if !found {
		t.Fatalf("missing binding for read miss path: %v", graph.ActionReads[0])
	}
	if len(missing.Defs) != 1 || !missing.Defs[0].Missing {
		t.Fatalf("missing.Defs = %v, want single missing state", missing.Defs)
	}
}

func TestBuildGraphClassifiesNoiseActionsAndKeys(t *testing.T) {
	records := []trace.Record{
		record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
		record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work", []string{"/tmp/work/CMakeLists.txt"}, []string{"/tmp/work/_build/CMakeCache.txt"}),
		record([]string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work", []string{"/tmp/work/_build/libfoo.a"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		record([]string{"cp", "libfoo.a", "out/lib/libfoo.a"}, "/tmp/work", []string{"/tmp/work/libfoo.a"}, []string{"/tmp/work/out/lib/libfoo.a"}),
	}

	graph := BuildGraph(BuildInput{Records: records})

	if graph.Actions[0].Kind != KindGeneric {
		t.Fatalf("generic action kind = %v, want %v", graph.Actions[0].Kind, KindGeneric)
	}
	wantGenericKey := "generic|cc|cwd=" + normalizePath("/tmp/work") + "|argv=cc -c core.c -o build/core.o"
	if got := graph.Actions[0].ActionKey; got != wantGenericKey {
		t.Fatalf("generic ActionKey = %q, want %q", got, wantGenericKey)
	}
	if graph.Actions[1].Kind != KindConfigure {
		t.Fatalf("cmake configure kind = %v, want %v", graph.Actions[1].Kind, KindConfigure)
	}
	if graph.Actions[2].Kind != KindInstall {
		t.Fatalf("cmake --install kind = %v, want %v", graph.Actions[2].Kind, KindInstall)
	}
	if graph.Actions[3].Kind != KindCopy {
		t.Fatalf("cp kind = %v, want %v", graph.Actions[3].Kind, KindCopy)
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

	graph := BuildGraph(BuildInput{Records: records, Scope: scope})

	if graph.Actions[0].Kind != KindConfigure {
		t.Fatalf("perl configure kind = %v, want %v", graph.Actions[0].Kind, KindConfigure)
	}
	if graph.Actions[1].Kind != KindInstall {
		t.Fatalf("shell install kind = %v, want %v", graph.Actions[1].Kind, KindInstall)
	}
}

func TestBuildGraphKeepsArtifactWritingShellGeneric(t *testing.T) {
	graph := BuildGraph(BuildInput{
		Records: []trace.Record{
			record(
				[]string{"sh", "-c", "cc -c foo.c -o build/foo.o"},
				"/tmp/work",
				[]string{"/tmp/work/foo.c"},
				[]string{"/tmp/work/build/foo.o"},
			),
		},
	})

	if graph.Actions[0].Kind != KindGeneric {
		t.Fatalf("shell artifact writer kind = %v, want %v", graph.Actions[0].Kind, KindGeneric)
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

	graph := BuildGraph(BuildInput{Records: records})

	for _, dir := range []string{"/tmp/work/include", "/tmp/work/build", "/tmp/work/_build"} {
		if _, ok := graph.Paths[normalizePath(dir)]; ok {
			t.Fatalf("directory path %q unexpectedly retained", normalizePath(dir))
		}
	}
}

func TestProjectRolesClassifiesProducedExecutableChainAsTooling(t *testing.T) {
	records := []trace.Record{
		record([]string{"sh", "bootstrap.sh"}, "/tmp/work",
			[]string{"/tmp/work/bootstrap.sh"},
			[]string{"/tmp/work/tools/b2"}),
		record([]string{"./tools/b2", "headers"}, "/tmp/work",
			[]string{"/tmp/work/project-config.jam"},
			[]string{"/tmp/work/_build/meta/status.txt", "/tmp/work/_build/meta/cache.db"}),
	}

	graph := BuildGraph(BuildInput{Records: records})
	roles := ProjectRoles(graph)

	for i := range graph.Actions {
		if RoleActionClass(roles, i) != ActionRoleTooling {
			t.Fatalf("RoleActionClass(%d) = %v, want %v", i, RoleActionClass(roles, i), ActionRoleTooling)
		}
	}
	for _, path := range []string{"/tmp/work/tools/b2", "/tmp/work/_build/meta/status.txt", "/tmp/work/_build/meta/cache.db"} {
		if got := projectedPathRole(graph, roles, path); got != RoleTooling {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, RoleTooling)
		}
	}
}

func TestProjectRolesClassifiesCopiedProducedExecutableChainAsTooling(t *testing.T) {
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

	graph := BuildGraph(BuildInput{Records: records})
	roles := ProjectRoles(graph)

	for i := range graph.Actions {
		if RoleActionClass(roles, i) != ActionRoleTooling {
			t.Fatalf("RoleActionClass(%d) = %v, want %v", i, RoleActionClass(roles, i), ActionRoleTooling)
		}
	}
	for _, path := range []string{
		"/tmp/work/tools/build/src/engine/b2",
		"/tmp/work/b2",
		"/tmp/work/_build/meta/status.txt",
		"/tmp/work/_build/meta/cache.db",
	} {
		if got := projectedPathRole(graph, roles, path); got != RoleTooling {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, RoleTooling)
		}
	}
}

func TestProjectRolesClassifiesCopiedProducedExecutableLeafAsTooling(t *testing.T) {
	records := []trace.Record{
		record([]string{"sh", "bootstrap.sh"}, "/tmp/work",
			[]string{"/tmp/work/bootstrap.sh"},
			[]string{"/tmp/work/tools/build/src/engine/b2"}),
		record([]string{"cp", "./tools/build/src/engine/b2", "./b2"}, "/tmp/work",
			[]string{"/tmp/work/tools/build/src/engine/b2"},
			[]string{"/tmp/work/b2"}),
	}

	graph := BuildGraph(BuildInput{Records: records})
	roles := ProjectRoles(graph)

	if RoleActionClass(roles, 0) != ActionRoleTooling {
		t.Fatalf("RoleActionClass(0) = %v, want %v", RoleActionClass(roles, 0), ActionRoleTooling)
	}
	if got := projectedPathRole(graph, roles, "/tmp/work/tools/build/src/engine/b2"); got != RoleTooling {
		t.Fatalf("role(%s) = %v, want %v", normalizePath("/tmp/work/tools/build/src/engine/b2"), got, RoleTooling)
	}
}

func TestProjectRolesKeepsConfigureControlPlanePathsAsTooling(t *testing.T) {
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{"/tmp/work/_build/meta/config.state", "/tmp/work/_build/meta/progress.count"}),
		recordWithProc(101, 100, []string{"cmake", "-E", "echo", "progress"}, "/tmp/work",
			[]string{"/tmp/work/_build/meta/progress.count"},
			[]string{"/tmp/work/_build/meta/progress.1"}),
	}

	graph := BuildGraph(BuildInput{Records: records})
	roles := ProjectRoles(graph)

	for _, path := range []string{"/tmp/work/_build/meta/config.state", "/tmp/work/_build/meta/progress.count", "/tmp/work/_build/meta/progress.1"} {
		if got := projectedPathRole(graph, roles, path); got != RoleTooling {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, RoleTooling)
		}
	}
}

func TestProjectRolesPromotesProbeIslandChildrenToTooling(t *testing.T) {
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

	graph := BuildGraph(BuildInput{Records: records})
	roles := ProjectRoles(graph)

	for _, path := range []string{
		"/tmp/work/_build/probe-checks/CheckFeature.c",
		"/tmp/work/_build/probe-checks/CheckFeature.c.o",
		"/tmp/work/_build/probe-checks/probe-check",
	} {
		if got := projectedPathRole(graph, roles, path); got != RoleTooling {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, RoleTooling)
		}
	}
	if got := projectedPathRole(graph, roles, "/tmp/work/_build/core.o"); got != RolePropagating {
		t.Fatalf("role(core.o) = %v, want %v", got, RolePropagating)
	}
}

func TestProjectRolesPromotesProbeIslandChildrenWrappedByGenericMake(t *testing.T) {
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

	graph := BuildGraph(BuildInput{Records: records})
	roles := ProjectRoles(graph)

	for _, path := range []string{
		"/tmp/work/_build/probe-checks/CheckFeature.c",
		"/tmp/work/_build/probe-checks/Makefile",
		"/tmp/work/_build/probe-checks/CheckFeature.c.o",
		"/tmp/work/_build/probe-checks/probe-check",
	} {
		if got := projectedPathRole(graph, roles, path); got != RoleTooling {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, RoleTooling)
		}
	}
	if got := projectedPathRole(graph, roles, "/tmp/work/_build/core.o"); got != RolePropagating {
		t.Fatalf("role(core.o) = %v, want %v", got, RolePropagating)
	}
}

func TestProjectRolesClassifiesInstallLeafPathAsDelivery(t *testing.T) {
	records := []trace.Record{
		record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/build/core.o"},
			[]string{"/tmp/work/out/lib/libfoo.a"}),
		record([]string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/out/lib/libfoo.a"},
			[]string{"/tmp/work/install/lib/libfoo.a"}),
	}

	graph := BuildGraph(BuildInput{
		Records: records,
		Scope:   trace.Scope{InstallRoot: "/tmp/work/install"},
	})
	roles := ProjectRoles(graph)
	if got := projectedPathRole(graph, roles, "/tmp/work/install/lib/libfoo.a"); got != RoleDelivery {
		t.Fatalf("role(install libfoo.a) = %v, want %v", got, RoleDelivery)
	}
}

func TestProjectRolesClassifiesLeafCopyAsDelivery(t *testing.T) {
	records := []trace.Record{
		record([]string{"ar", "rcs", "_build/libfoo.a", "_build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/_build/core.o"},
			[]string{"/tmp/work/_build/libfoo.a"}),
		record([]string{"cp", "_build/libfoo.a", "stage/libfoo.a"}, "/tmp/work",
			[]string{"/tmp/work/_build/libfoo.a"},
			[]string{"/tmp/work/stage/libfoo.a"}),
	}

	graph := BuildGraph(BuildInput{Records: records})
	roles := ProjectRoles(graph)
	if got := projectedPathRole(graph, roles, "/tmp/work/stage/libfoo.a"); got != RoleDelivery {
		t.Fatalf("role(stage libfoo.a) = %v, want %v", got, RoleDelivery)
	}
}

func TestProjectRolesClassifiesLeafHeaderCopyAsDelivery(t *testing.T) {
	records := []trace.Record{
		record([]string{"cp", "_build/generated_config.h", "stage/generated_config.h"}, "/tmp/work",
			[]string{"/tmp/work/_build/generated_config.h"},
			[]string{"/tmp/work/stage/generated_config.h"}),
	}

	graph := BuildGraph(BuildInput{Records: records})
	roles := ProjectRoles(graph)
	if got := projectedPathRole(graph, roles, "/tmp/work/stage/generated_config.h"); got != RoleDelivery {
		t.Fatalf("role(stage generated_config.h) = %v, want %v", got, RoleDelivery)
	}
}

func TestProjectRolesTreatsConfigureSidecarsAsTooling(t *testing.T) {
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{
				"/tmp/work/_build/trace_options.h",
				"/tmp/work/_build/CMakeFiles/pkgRedirects",
				"/tmp/work/_build/cmake_install.cmake",
			}),
		recordWithProc(101, 100, []string{"cmake", "-E", "echo", "probe"}, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc",
			[]string{"/tmp/work/_build/CMakeFiles/pkgRedirects"},
			[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects"}),
		recordWithProc(200, 2, []string{"cc", "-c", "/tmp/work/src/core.c", "-o", "/tmp/work/_build/core.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/src/core.c", "/tmp/work/_build/trace_options.h"},
			[]string{"/tmp/work/_build/core.o"}),
		recordWithProc(201, 200, []string{"ar", "rcs", "/tmp/work/_build/libtracecore.a", "/tmp/work/_build/core.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/_build/core.o"},
			[]string{"/tmp/work/_build/libtracecore.a"}),
		recordWithProc(300, 3, []string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{
				"/tmp/work/_build/cmake_install.cmake",
				"/tmp/work/_build/libtracecore.a",
				"/tmp/work/_build/trace_options.h",
			},
			[]string{
				"/tmp/work/install/lib/libtracecore.a",
				"/tmp/work/install/include/trace_alias.h",
			}),
	}

	graph := BuildGraph(BuildInput{Records: records, Scope: scope})
	roles := ProjectRoles(graph)

	for _, path := range []string{
		"/tmp/work/_build/CMakeFiles/pkgRedirects",
		"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects",
		"/tmp/work/_build/cmake_install.cmake",
	} {
		if got := projectedPathRole(graph, roles, path); got != RoleTooling {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, RoleTooling)
		}
	}
	if got := projectedPathRole(graph, roles, "/tmp/work/_build/trace_options.h"); got != RolePropagating {
		t.Fatalf("role(trace_options.h) = %v, want %v", got, RolePropagating)
	}
	if got := projectedPathRole(graph, roles, "/tmp/work/install/lib/libtracecore.a"); got != RoleDelivery {
		t.Fatalf("role(install/libtracecore.a) = %v, want %v", got, RoleDelivery)
	}
}

func TestProjectRolesTreatsEventConfigureSidecarsAsTooling(t *testing.T) {
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	graph := BuildGraph(BuildInput{Events: traceoptionsEventTrace(false), Scope: scope})
	roles := ProjectRoles(graph)

	for _, path := range []string{
		"/tmp/work/_build/CMakeFiles/pkgRedirects",
		"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects",
		"/tmp/work/_build/cmake_install.cmake",
	} {
		if got := projectedPathRole(graph, roles, path); got != RoleTooling {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, RoleTooling)
		}
	}
	if got := projectedPathRole(graph, roles, "/tmp/work/_build/trace_options.h"); got != RolePropagating {
		t.Fatalf("role(trace_options.h) = %v, want %v", got, RolePropagating)
	}
}

func TestProjectRolesTreatsTryCompileProbeArtifactsAsTooling(t *testing.T) {
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	tryDir := "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-ProbeKeep"
	graph := BuildGraph(BuildInput{
		Events: traceoptionsTryCompileProbeEventTrace(false, "ProbeKeep", "cmTC_probe"),
		Scope:  scope,
	})
	roles := ProjectRoles(graph)

	for _, path := range []string{
		tryDir + "/CheckIncludeFile.c",
		tryDir + "/CMakeFiles/cmTC_probe.dir/CheckIncludeFile.c.o",
		tryDir + "/cmTC_probe",
		tryDir + "/.cmake/api/v1/reply",
	} {
		if got := projectedPathRole(graph, roles, path); got != RoleTooling {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, RoleTooling)
		}
	}
	if got := projectedPathRole(graph, roles, "/tmp/work/_build/trace_options.h"); got != RolePropagating {
		t.Fatalf("role(trace_options.h) = %v, want %v", got, RolePropagating)
	}
}

func TestProjectRolesTreatsInstallManifestAsDelivery(t *testing.T) {
	scope := trace.Scope{
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	records := []trace.Record{
		recordWithProc(200, 1, []string{"ar", "rcs", "/tmp/work/_build/libtracecore.a", "/tmp/work/_build/core.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/_build/core.o"},
			[]string{"/tmp/work/_build/libtracecore.a"}),
		recordWithProc(300, 2, []string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{
				"/tmp/work/_build/cmake_install.cmake",
				"/tmp/work/_build/libtracecore.a",
			},
			[]string{
				"/tmp/work/install/lib/libtracecore.a",
				"/tmp/work/_build/install_manifest.txt",
			}),
	}

	graph := BuildGraph(BuildInput{Records: records, Scope: scope})
	roles := ProjectRoles(graph)

	if got := projectedPathRole(graph, roles, "/tmp/work/_build/install_manifest.txt"); got != RoleDelivery {
		t.Fatalf("role(install_manifest.txt) = %v, want %v", got, RoleDelivery)
	}
	if ImpactPathAllowed(graph, roles, "/tmp/work/_build/install_manifest.txt") {
		t.Fatal("install_manifest.txt should stay outside the Stage 2 impact domain")
	}
}

func TestProjectRolesTreatsArchiveTempSidecarsAsTooling(t *testing.T) {
	scope := trace.Scope{BuildRoot: "/tmp/work/_build"}
	records := []trace.Record{
		recordWithProc(200, 1, []string{"ar", "qc", "/tmp/work/_build/libtracecore.a", "/tmp/work/_build/core.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/_build/core.o"},
			[]string{"/tmp/work/_build/libtracecore.a", "/tmp/work/_build/stNMeD5X"}),
		recordWithProc(201, 200, []string{"ranlib", "/tmp/work/_build/libtracecore.a"}, "/tmp/work/_build",
			[]string{"/tmp/work/_build/libtracecore.a"},
			[]string{"/tmp/work/_build/libtracecore.a", "/tmp/work/_build/stdwbVyX"}),
		recordWithProc(300, 2, []string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/_build/libtracecore.a"},
			[]string{"/tmp/work/install/lib/libtracecore.a"}),
	}

	graph := BuildGraph(BuildInput{Records: records, Scope: scope})
	roles := ProjectRoles(graph)

	for _, path := range []string{
		"/tmp/work/_build/stNMeD5X",
		"/tmp/work/_build/stdwbVyX",
	} {
		if got := projectedPathRole(graph, roles, path); got != RoleTooling {
			t.Fatalf("role(%s) = %v, want %v", normalizePath(path), got, RoleTooling)
		}
	}
	if got := projectedPathRole(graph, roles, "/tmp/work/_build/libtracecore.a"); got != RolePropagating {
		t.Fatalf("role(libtracecore.a) = %v, want %v", got, RolePropagating)
	}
}

func TestProjectRolesKeepsMainlineVisibleAcrossConfigureChildFrontier(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/_build",
	}
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{
				"/tmp/work/_build/config.cache",
				"/tmp/work/_build/config.h",
			}),
		recordWithProc(200, 100, []string{"cc", "/tmp/work/main.c", "/tmp/work/_build/config.h", "-o", "/tmp/work/_build/app"}, "/tmp/work/_build",
			[]string{
				"/tmp/work/main.c",
				"/tmp/work/_build/config.h",
			},
			[]string{"/tmp/work/_build/app"}),
	}

	graph := BuildGraph(BuildInput{Records: records, Scope: scope})
	roles := ProjectRoles(graph)

	if got := projectedPathRole(graph, roles, "/tmp/work/_build/config.cache"); got != RoleTooling {
		t.Fatalf("role(config.cache) = %v, want %v", got, RoleTooling)
	}
	if got := projectedPathRole(graph, roles, "/tmp/work/_build/config.h"); got != RolePropagating {
		t.Fatalf("role(config.h) = %v, want %v", got, RolePropagating)
	}
	if got := projectedPathRole(graph, roles, "/tmp/work/_build/app"); got != RolePropagating {
		t.Fatalf("role(app) = %v, want %v", got, RolePropagating)
	}
	if got := RoleActionClass(roles, 1); got != ActionRoleMainline {
		t.Fatalf("action role(cc) = %v, want %v", got, ActionRoleMainline)
	}
}

func TestInferMainlineVisibleDefsUsesDerivedSinkWithoutInstall(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/_build",
	}
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{
				"/tmp/work/_build/config.cache",
				"/tmp/work/_build/config.h",
			}),
		recordWithProc(200, 100, []string{"cc", "/tmp/work/main.c", "/tmp/work/_build/config.h", "-o", "/tmp/work/_build/app"}, "/tmp/work/_build",
			[]string{
				"/tmp/work/main.c",
				"/tmp/work/_build/config.h",
			},
			[]string{"/tmp/work/_build/app"}),
	}

	graph := BuildGraph(BuildInput{Records: records, Scope: scope})
	tooling := classifyToolingFamily(graph)
	deliveryOnly := make([]bool, len(graph.Actions))
	for idx := range graph.Actions {
		deliveryOnly[idx] = isDeliveryOnlyAction(graph, idx)
	}
	toolingWorkspaceRoots := inferToolingWorkspaceRoots(graph, tooling, deliveryOnly)
	nonEscaping := classifyNonEscapingToolingDefs(graph, tooling, deliveryOnly, toolingWorkspaceRoots)
	visible := inferMainlineVisibleDefs(graph, tooling, toolingWorkspaceRoots, nonEscaping)

	for _, def := range []PathState{
		graph.ActionWrites[0][1], // config.h
		graph.ActionWrites[1][0], // app
	} {
		if _, ok := visible[def]; !ok {
			t.Fatalf("fallback closure missing %v", def)
		}
	}
	if _, ok := visible[graph.ActionWrites[0][0]]; ok { // config.cache
		t.Fatalf("fallback closure unexpectedly retained %v", graph.ActionWrites[0][0])
	}
}

func TestInferMainlineVisibleDefsUsesHardSinksWithInstall(t *testing.T) {
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{
				"/tmp/work/_build/config.cache",
				"/tmp/work/_build/config.h",
			}),
		recordWithProc(200, 100, []string{"cc", "/tmp/work/main.c", "/tmp/work/_build/config.h", "-o", "/tmp/work/_build/app"}, "/tmp/work/_build",
			[]string{
				"/tmp/work/main.c",
				"/tmp/work/_build/config.h",
			},
			[]string{"/tmp/work/_build/app"}),
		recordWithProc(300, 1, []string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/_build/app"},
			[]string{"/tmp/work/install/bin/app"}),
	}

	graph := BuildGraph(BuildInput{Records: records, Scope: scope})
	tooling := classifyToolingFamily(graph)
	deliveryOnly := make([]bool, len(graph.Actions))
	for idx := range graph.Actions {
		deliveryOnly[idx] = isDeliveryOnlyAction(graph, idx)
	}
	toolingWorkspaceRoots := inferToolingWorkspaceRoots(graph, tooling, deliveryOnly)
	nonEscaping := classifyNonEscapingToolingDefs(graph, tooling, deliveryOnly, toolingWorkspaceRoots)
	visible := inferMainlineVisibleDefs(graph, tooling, toolingWorkspaceRoots, nonEscaping)

	for _, def := range []PathState{
		graph.ActionWrites[0][1], // config.h
		graph.ActionWrites[1][0], // app
		graph.ActionWrites[2][0], // install/bin/app
	} {
		if _, ok := visible[def]; !ok {
			t.Fatalf("hard-sink closure missing %v", def)
		}
	}
	if _, ok := visible[graph.ActionWrites[0][0]]; ok { // config.cache
		t.Fatalf("hard-sink closure unexpectedly retained %v", graph.ActionWrites[0][0])
	}
}

func TestProjectRolesDoNotPromoteUnusedHeaderWhenHardSinkClosureExists(t *testing.T) {
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{
				"/tmp/work/_build/config.cache",
				"/tmp/work/_build/config.h",
				"/tmp/work/_build/unused.h",
			}),
		recordWithProc(200, 100, []string{"cc", "/tmp/work/main.c", "/tmp/work/_build/config.h", "-o", "/tmp/work/_build/app"}, "/tmp/work/_build",
			[]string{
				"/tmp/work/main.c",
				"/tmp/work/_build/config.h",
			},
			[]string{"/tmp/work/_build/app"}),
		recordWithProc(300, 1, []string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/_build/app"},
			[]string{"/tmp/work/install/bin/app"}),
	}

	graph := BuildGraph(BuildInput{Records: records, Scope: scope})
	roles := ProjectRoles(graph)

	if got := projectedPathRole(graph, roles, "/tmp/work/_build/config.h"); got != RolePropagating {
		t.Fatalf("role(config.h) = %v, want %v", got, RolePropagating)
	}
	if got := projectedPathRole(graph, roles, "/tmp/work/_build/unused.h"); got != RoleTooling {
		t.Fatalf("role(unused.h) = %v, want %v", got, RoleTooling)
	}
	if got := projectedPathRole(graph, roles, "/tmp/work/_build/config.cache"); got != RoleTooling {
		t.Fatalf("role(config.cache) = %v, want %v", got, RoleTooling)
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

	graphA := BuildGraph(BuildInput{
		Records: []trace.Record{recA},
		Scope: trace.Scope{
			SourceRoot:  "/tmp/work",
			InstallRoot: "/tmp/work/out-a",
		},
	})
	graphB := BuildGraph(BuildInput{
		Records: []trace.Record{recB},
		Scope: trace.Scope{
			SourceRoot:  "/tmp/work",
			InstallRoot: "/tmp/work/out-b",
		},
	})

	if got, want := graphA.Actions[0].Fingerprint, graphB.Actions[0].Fingerprint; got != want {
		t.Fatalf("fingerprint mismatch:\nA=%q\nB=%q", got, want)
	}
	if got, want := graphA.Actions[0].ActionKey, graphB.Actions[0].ActionKey; got != want {
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

	graph := BuildGraph(BuildInput{
		Records: records,
		Scope:   trace.Scope{InstallRoot: "/tmp/work/install/"},
	})
	roles := ProjectRoles(graph)
	if got := projectedPathRole(graph, roles, "/tmp/work/install/lib/libfoo.a"); got != RoleDelivery {
		t.Fatalf("role(install/libfoo.a) = %v, want %v", got, RoleDelivery)
	}
}

func TestAnalyzeExposesJoinSetThroughAnalysisInput(t *testing.T) {
	base := []trace.Record{
		record([]string{"cc", "-c", "a.c", "-o", "build/a.o"}, "/tmp/work", []string{"/tmp/work/a.c"}, []string{"/tmp/work/build/a.o"}),
		record([]string{"cc", "-c", "b.c", "-o", "build/b.o"}, "/tmp/work", []string{"/tmp/work/b.c"}, []string{"/tmp/work/build/b.o"}),
		record([]string{"cc", "build/a.o", "build/b.o", "-o", "out/bin/app"}, "/tmp/work", []string{"/tmp/work/build/a.o", "/tmp/work/build/b.o"}, []string{"/tmp/work/out/bin/app"}),
	}
	probe := []trace.Record{
		record([]string{"cc", "-DFEATURE", "-c", "a.c", "-o", "build/a.o"}, "/tmp/work", []string{"/tmp/work/a.c"}, []string{"/tmp/work/build/a.o"}),
		record([]string{"cc", "-c", "b.c", "-o", "build/b.o"}, "/tmp/work", []string{"/tmp/work/b.c"}, []string{"/tmp/work/build/b.o"}),
		record([]string{"cc", "build/a.o", "build/b.o", "-o", "out/bin/app"}, "/tmp/work", []string{"/tmp/work/build/a.o", "/tmp/work/build/b.o"}, []string{"/tmp/work/out/bin/app"}),
	}

	result := Analyze(AnalysisInput{
		Base:  AnalysisSideInput{Records: base},
		Probe: AnalysisSideInput{Records: probe},
	})

	if len(result.Debug.BaseGraph.Actions) != 3 || len(result.Debug.ProbeGraph.Actions) != 3 {
		t.Fatalf("Analyze() graphs = %d/%d actions, want 3/3", len(result.Debug.BaseGraph.Actions), len(result.Debug.ProbeGraph.Actions))
	}
	if len(result.Profile.JoinSet) != 1 || result.Profile.JoinSet[0] != 2 {
		t.Fatalf("JoinSet = %v, want [2]", result.Profile.JoinSet)
	}
	if len(result.Debug.Flow.JoinActions) != 1 || result.Debug.Flow.JoinActions[0] != 2 {
		t.Fatalf("Flow.JoinActions = %v, want [2]", result.Debug.Flow.JoinActions)
	}
	if len(result.Debug.Flow.FrontierActions) != 1 || result.Debug.Flow.FrontierActions[0] != 2 {
		t.Fatalf("Flow.FrontierActions = %v, want [2]", result.Debug.Flow.FrontierActions)
	}
}
