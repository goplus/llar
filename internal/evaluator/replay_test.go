package evaluator

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/goplus/llar/internal/trace"
)

func TestReplayTraceDumpSummary(t *testing.T) {
	path := strings.TrimSpace(os.Getenv("LLAR_TRACE_REPLAY"))
	if path == "" {
		t.Skip("set LLAR_TRACE_REPLAY to a trace dump file to inspect graph roles")
	}

	records := readTraceDumpFile(t, path)
	graph := buildGraph(records)

	t.Logf("trace file: %s", path)
	t.Logf("records=%d actions=%d", len(records), len(graph.actions))
	t.Logf("action counts: %s", formatActionSummary(graph))
	t.Logf("path role counts: %s", formatPathRoleSummary(graph))

	for _, role := range []pathRole{roleTooling, rolePropagating, roleDelivery} {
		paths := samplePathsByRole(graph, role, 8)
		t.Logf("%s sample (%d):", role.String(), len(paths))
		for _, path := range paths {
			t.Logf("  %s", path)
		}
	}

	for _, want := range []string{
		"/_build/zconf.h",
		"/TryCompile-",
		"/libz.so.1.3.1",
		"/include/zlib.h",
		"/zlib.map",
	} {
		paths := sampleInterestingPaths(graph, want, 6)
		if len(paths) == 0 {
			t.Logf("match %q: absent", want)
			continue
		}
		t.Logf("match %q:", want)
		for _, path := range paths {
			t.Logf("  %s", path)
		}
	}
}

func readTraceDumpFile(t *testing.T, path string) []trace.Record {
	t.Helper()

	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatalf("open trace dump: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	records := make([]trace.Record, 0, 256)
	var current *trace.Record
	flush := func() {
		if current == nil {
			return
		}
		records = append(records, *current)
		current = nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.Contains(trimmed, ". argv: "):
			flush()
			argvLine := trimmed[strings.Index(trimmed, "argv: ")+len("argv: "):]
			current = &trace.Record{Argv: strings.Fields(argvLine)}
		case strings.HasPrefix(trimmed, "pid: "):
			if current != nil {
				current.PID = parseInt64DumpField(t, strings.TrimSpace(strings.TrimPrefix(trimmed, "pid: ")))
			}
		case strings.HasPrefix(trimmed, "ppid: "):
			if current != nil {
				current.ParentPID = parseInt64DumpField(t, strings.TrimSpace(strings.TrimPrefix(trimmed, "ppid: ")))
			}
		case strings.HasPrefix(trimmed, "cwd: "):
			if current != nil {
				current.Cwd = strings.TrimSpace(strings.TrimPrefix(trimmed, "cwd: "))
			}
		case strings.HasPrefix(trimmed, "env: "):
			if current != nil {
				current.Env = splitDumpPaths(strings.TrimPrefix(trimmed, "env: "))
			}
		case strings.HasPrefix(trimmed, "inputs: "):
			if current != nil {
				current.Inputs = splitDumpPaths(strings.TrimPrefix(trimmed, "inputs: "))
			}
		case strings.HasPrefix(trimmed, "changes: "):
			if current != nil {
				current.Changes = splitDumpPaths(strings.TrimPrefix(trimmed, "changes: "))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trace dump: %v", err)
	}
	flush()

	if len(records) == 0 {
		t.Fatalf("no trace records parsed from %s", path)
	}
	return records
}

func splitDumpPaths(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return slices.DeleteFunc(parts, func(part string) bool { return part == "" })
}

func parseInt64DumpField(t *testing.T, raw string) int64 {
	t.Helper()

	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		t.Fatalf("parse int64 dump field %q: %v", raw, err)
	}
	return value
}

func TestReplayStepInitializesBuildRoot(t *testing.T) {
	if !replayStepInitializesBuildRoot(replayRoot{
		reads:  []string{"$SRC/CMakeLists.txt"},
		writes: []string{"$BUILD/CMakeCache.txt", "$BUILD/config.h"},
	}) {
		t.Fatal("configure-style replay root should initialize build root")
	}
	if replayStepInitializesBuildRoot(replayRoot{
		reads:  []string{"$BUILD/config.h", "$SRC/lib/xmlparse.c"},
		writes: []string{"$BUILD/CMakeFiles/expat.dir/lib/xmlparse.c.o"},
	}) {
		t.Fatal("build-style replay root should not initialize build root")
	}
}

func TestPrepareReplayBuildRootClearsInitializerState(t *testing.T) {
	buildRoot := filepath.Join(t.TempDir(), "_build")
	if err := os.MkdirAll(filepath.Join(buildRoot, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(buildRoot): %v", err)
	}
	stale := filepath.Join(buildRoot, "CMakeCache.txt")
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatalf("WriteFile(stale): %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildRoot, "nested", "keep.txt"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("WriteFile(nested stale): %v", err)
	}

	err := prepareReplayBuildRoot(buildRoot, []replayRoot{{
		reads:  []string{"$SRC/CMakeLists.txt"},
		writes: []string{"$BUILD/CMakeCache.txt"},
	}})
	if err != nil {
		t.Fatalf("prepareReplayBuildRoot() error: %v", err)
	}
	if _, err := os.Stat(buildRoot); err != nil {
		t.Fatalf("build root missing after prepare: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale CMakeCache.txt still exists, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(buildRoot, "nested", "keep.txt")); !os.IsNotExist(err) {
		t.Fatalf("nested stale file still exists, err=%v", err)
	}
}
