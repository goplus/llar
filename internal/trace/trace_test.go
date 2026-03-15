package trace

import (
	"reflect"
	"testing"
)

func TestParseStraceRecords(t *testing.T) {
	content := `
1234 1741260000.000001 chdir("/tmp/work") = 0
1234 1741260000.000002 execve("/usr/bin/cc", ["cc", "-c", "core.c", "-o", "core.o"], 0x0) = 0
1234 1741260000.000003 openat(AT_FDCWD, "core.c", O_RDONLY|O_CLOEXEC) = 3
1234 1741260000.000004 openat(AT_FDCWD, "core.o", O_WRONLY|O_CREAT|O_TRUNC, 0666) = 4
1234 1741260000.000005 execve("/usr/bin/ar", ["ar", "rcs", "libfoo.a", "core.o"], 0x0) = 0
1234 1741260000.000006 openat(AT_FDCWD, "core.o", O_RDONLY|O_CLOEXEC) = 3
1234 1741260000.000007 openat(AT_FDCWD, "libfoo.a", O_WRONLY|O_CREAT|O_TRUNC, 0666) = 4
`

	got := parseStraceRecords(content, parseOptions{rootCwd: "/repo"})
	want := []Record{
		{
			PID:     1234,
			Argv:    []string{"cc", "-c", "core.c", "-o", "core.o"},
			Cwd:     "/tmp/work",
			Inputs:  []string{"/tmp/work/core.c"},
			Changes: []string{"/tmp/work/core.o"},
		},
		{
			PID:     1234,
			Argv:    []string{"ar", "rcs", "libfoo.a", "core.o"},
			Cwd:     "/tmp/work",
			Inputs:  []string{"/tmp/work/core.o"},
			Changes: []string{"/tmp/work/libfoo.a"},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseStraceRecords() = %#v, want %#v", got, want)
	}
}

func TestParseStraceRecords_IgnoresFailedSyscalls(t *testing.T) {
	content := `
1234 1741260000.000001 chdir("/tmp/work") = 0
1234 1741260000.000002 execve("/usr/bin/cc", ["cc", "-c", "core.c", "-o", "core.o"], 0x0) = -1 ENOENT (No such file or directory)
1234 1741260000.000003 execve("/usr/bin/cc", ["cc", "-c", "core.c", "-o", "core.o"], 0x0) = 0
1234 1741260000.000004 openat(AT_FDCWD, "missing.h", O_RDONLY|O_CLOEXEC) = -1 ENOENT (No such file or directory)
1234 1741260000.000005 openat(AT_FDCWD, "core.c", O_RDONLY|O_CLOEXEC) = 3
1234 1741260000.000006 openat(AT_FDCWD, "core.o", O_WRONLY|O_CREAT|O_TRUNC, 0666) = 4
1234 1741260000.000007 rename("tmp.o", "core.o") = -1 ENOENT (No such file or directory)
1234 1741260000.000008 unlink("ghost.o") = -1 ENOENT (No such file or directory)
`

	got := parseStraceRecords(content, parseOptions{rootCwd: "/repo"})
	want := []Record{
		{
			PID:     1234,
			Argv:    []string{"cc", "-c", "core.c", "-o", "core.o"},
			Cwd:     "/tmp/work",
			Inputs:  []string{"/tmp/work/core.c"},
			Changes: []string{"/tmp/work/core.o"},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseStraceRecords() = %#v, want %#v", got, want)
	}
}

func TestParseStraceRecords_MergesUnfinishedExecve(t *testing.T) {
	content := `
1234 1741260000.000001 chdir("/tmp/work") = 0
1234 1741260000.000002 execve("/usr/bin/cmake", ["cmake", "-S", "/src", "-B", "/tmp/work/_build"], 0x0 <unfinished ...>
1234 1741260000.000003 <... execve resumed>) = 0
1234 1741260000.000004 openat(AT_FDCWD, "CMakeLists.txt", O_RDONLY|O_CLOEXEC) = 3
1234 1741260000.000005 openat(AT_FDCWD, "_build/CMakeCache.txt", O_WRONLY|O_CREAT|O_TRUNC, 0666) = 4
`

	got := parseStraceRecords(content, parseOptions{rootCwd: "/repo"})
	want := []Record{
		{
			PID:     1234,
			Argv:    []string{"cmake", "-S", "/src", "-B", "/tmp/work/_build"},
			Cwd:     "/tmp/work",
			Inputs:  []string{"/tmp/work/CMakeLists.txt"},
			Changes: []string{"/tmp/work/_build/CMakeCache.txt"},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseStraceRecords() = %#v, want %#v", got, want)
	}
}

func TestParseStraceRecords_KeepRootsFilter(t *testing.T) {
	content := `
1234 1741260000.000001 chdir("/tmp/work") = 0
1234 1741260000.000002 execve("/usr/bin/cc", ["cc", "-c", "core.c", "-o", "core.o"], 0x0) = 0
1234 1741260000.000003 openat(AT_FDCWD, "/usr/share/cmake-3.25/Modules/CMakeCCompiler.cmake.in", O_RDONLY|O_CLOEXEC) = 3
1234 1741260000.000004 openat(AT_FDCWD, "/opt/deps/include/dep.h", O_RDONLY|O_CLOEXEC) = 3
1234 1741260000.000005 openat(AT_FDCWD, "core.c", O_RDONLY|O_CLOEXEC) = 3
1234 1741260000.000006 openat(AT_FDCWD, "CMakeLists.txt", O_RDONLY|O_CLOEXEC) = 3
1234 1741260000.000007 openat(AT_FDCWD, "notes.txt", O_RDONLY|O_CLOEXEC) = 3
1234 1741260000.000008 openat(AT_FDCWD, "/tmp/cc-temp.s", O_WRONLY|O_CREAT|O_TRUNC, 0666) = 4
1234 1741260000.000009 openat(AT_FDCWD, "core.o", O_WRONLY|O_CREAT|O_TRUNC, 0666) = 4
1234 1741260000.000010 openat(AT_FDCWD, "libfoo.so.1.2.3", O_WRONLY|O_CREAT|O_TRUNC, 0666) = 4
`

	got := parseStraceRecords(content, parseOptions{
		rootCwd:   "/repo",
		keepRoots: []string{"/tmp/work", "/opt/deps"},
	})
	want := []Record{
		{
			PID:     1234,
			Argv:    []string{"cc", "-c", "core.c", "-o", "core.o"},
			Cwd:     "/tmp/work",
			Inputs:  []string{"/opt/deps/include/dep.h", "/tmp/work/core.c", "/tmp/work/CMakeLists.txt", "/tmp/work/notes.txt"},
			Changes: []string{"/tmp/work/core.o", "/tmp/work/libfoo.so.1.2.3"},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseStraceRecords() = %#v, want %#v", got, want)
	}
}

func TestParseStraceRecords_ResolvesOpenatDirfdPath(t *testing.T) {
	content := `
1234 1741260000.000001 chdir("/tmp/work") = 0
1234 1741260000.000002 execve("/usr/lib/gcc/aarch64-linux-gnu/12/cc1", ["cc1", "xmlparse.c"], 0x0) = 0
1234 1741260000.000003 openat(AT_FDCWD, "lib/xmlparse.c", O_RDONLY|O_CLOEXEC) = 3</tmp/work/lib/xmlparse.c>
1234 1741260000.000004 openat(3</tmp/work/_build>, "expat_config.h", O_RDONLY|O_CLOEXEC) = 4</tmp/work/_build/expat_config.h>
`

	got := parseStraceRecords(content, parseOptions{
		rootCwd:   "/repo",
		keepRoots: []string{"/tmp/work"},
	})
	want := []Record{
		{
			PID:    1234,
			Argv:   []string{"cc1", "xmlparse.c"},
			Cwd:    "/tmp/work",
			Inputs: []string{"/tmp/work/lib/xmlparse.c", "/tmp/work/_build/expat_config.h"},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseStraceRecords() = %#v, want %#v", got, want)
	}
}

func TestParseStraceRecords_ResolvesRenameatDirfdPaths(t *testing.T) {
	content := `
1234 1741260000.000001 chdir("/tmp/work") = 0
1234 1741260000.000002 execve("/usr/bin/cmake", ["cmake", "--install", "."], 0x0) = 0
1234 1741260000.000003 renameat(3</tmp/work/_build/stage>, "pkg/libfoo.a", 4</tmp/work/install/lib>, "libfoo.a") = 0
`

	got := parseStraceRecords(content, parseOptions{
		rootCwd:   "/repo",
		keepRoots: []string{"/tmp/work"},
	})
	want := []Record{
		{
			PID:     1234,
			Argv:    []string{"cmake", "--install", "."},
			Cwd:     "/tmp/work",
			Changes: []string{"/tmp/work/_build/stage/pkg/libfoo.a", "/tmp/work/install/lib/libfoo.a"},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseStraceRecords() = %#v, want %#v", got, want)
	}
}

func TestParseStraceRecords_TreatsCreateReadOnlyOpenAsInput(t *testing.T) {
	content := `
1234 1741260000.000001 chdir("/tmp/work") = 0
1234 1741260000.000002 execve("/usr/bin/cmake", ["cmake", "-P", "probe.cmake"], 0x0) = 0
1234 1741260000.000003 openat(AT_FDCWD, "_build/probe.cache", O_RDONLY|O_CREAT|O_CLOEXEC, 0666) = 3
`

	got := parseStraceRecords(content, parseOptions{
		rootCwd:   "/repo",
		keepRoots: []string{"/tmp/work"},
	})
	want := []Record{
		{
			PID:    1234,
			Argv:   []string{"cmake", "-P", "probe.cmake"},
			Cwd:    "/tmp/work",
			Inputs: []string{"/tmp/work/_build/probe.cache"},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseStraceRecords() = %#v, want %#v", got, want)
	}
}

func TestParseStraceRecords_PrefersReturnedFDPathForSymlinkTargets(t *testing.T) {
	content := `
1234 1741260000.000001 chdir("/tmp/work") = 0
1234 1741260000.000002 execve("/usr/bin/cc", ["cc", "-c", "core.c"], 0x0) = 0
1234 1741260000.000003 openat(AT_FDCWD, "include/config.h", O_RDONLY|O_CLOEXEC) = 3</tmp/work/_build/generated/config.h>
`

	got := parseStraceRecords(content, parseOptions{
		rootCwd:   "/repo",
		keepRoots: []string{"/tmp/work"},
	})
	want := []Record{
		{
			PID:    1234,
			Argv:   []string{"cc", "-c", "core.c"},
			Cwd:    "/tmp/work",
			Inputs: []string{"/tmp/work/_build/generated/config.h"},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseStraceRecords() = %#v, want %#v", got, want)
	}
}

func TestParseStraceRecordsDetailed_ReportsParseDiagnostics(t *testing.T) {
	content := `
execve("/usr/bin/cc", ["cc"], 0x0) = 0
1234 1741260000.000002 <... openat resumed>) = 3
1234 1741260000.000003 this is not a syscall
`

	got, diagnostics := parseStraceRecordsDetailed(content, parseOptions{rootCwd: "/repo"})
	if len(got) != 0 {
		t.Fatalf("parseStraceRecordsDetailed() records = %#v, want none", got)
	}
	if diagnostics.MissingPIDLines != 1 {
		t.Fatalf("MissingPIDLines = %d, want 1", diagnostics.MissingPIDLines)
	}
	if diagnostics.ResumedMismatches != 1 {
		t.Fatalf("ResumedMismatches = %d, want 1", diagnostics.ResumedMismatches)
	}
	if diagnostics.InvalidCalls != 1 {
		t.Fatalf("InvalidCalls = %d, want 1", diagnostics.InvalidCalls)
	}
	if diagnostics.Trusted() {
		t.Fatalf("Trusted() = true, want false")
	}
}

func TestParseStraceRecordsDetailed_ResetsReusedChildPIDState(t *testing.T) {
	content := `
5678 1741260000.000001 chdir("/stale") = 0
5678 1741260000.000002 execve("/bin/true", ["true"], 0x0) = 0
1234 1741260000.000003 chdir("/fresh") = 0
1234 1741260000.000004 clone(child_stack=NULL, flags=CLONE_VM|CLONE_VFORK|SIGCHLD) = 5678
5678 1741260000.000005 execve("/usr/bin/cc", ["cc", "-c", "core.c"], 0x0) = 0
5678 1741260000.000006 openat(AT_FDCWD, "core.c", O_RDONLY|O_CLOEXEC) = 3
`

	got, diagnostics := parseStraceRecordsDetailed(content, parseOptions{rootCwd: "/repo"})
	if len(got) != 2 {
		t.Fatalf("parseStraceRecordsDetailed() len = %d, want 2", len(got))
	}
	if got[1].ParentPID != 1234 {
		t.Fatalf("reused pid ParentPID = %d, want %d", got[1].ParentPID, 1234)
	}
	if got[1].Cwd != "/fresh" {
		t.Fatalf("reused pid Cwd = %q, want %q", got[1].Cwd, "/fresh")
	}
	if !reflect.DeepEqual(got[1].Inputs, []string{"/fresh/core.c"}) {
		t.Fatalf("reused pid Inputs = %#v, want %#v", got[1].Inputs, []string{"/fresh/core.c"})
	}
	if diagnostics.PIDStateResets != 1 {
		t.Fatalf("PIDStateResets = %d, want 1", diagnostics.PIDStateResets)
	}
}
