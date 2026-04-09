package evaluator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goplus/llar/internal/trace"
)

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

func TestNormalizeReplayEnvMatchesTraceSSARules(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/src",
		BuildRoot:  "/tmp/src/_build",
	}
	got := normalizeReplayEnv([]string{
		"PWD=/tmp/src",
		"SHLVL=2",
		"TERM=xterm-256color",
		"CFLAGS=-O2",
		"TMPDIR=/tmp/src/_tmp",
	}, scope)
	want := []string{
		"CFLAGS=-O2",
		"TMPDIR=$SRC/_tmp",
	}
	if len(got) != len(want) {
		t.Fatalf("normalizeReplayEnv() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeReplayEnv()[%d] = %q, want %q (full=%v)", i, got[i], want[i], got)
		}
	}
}

func TestPlanRootReplayIgnoresNoiseOnlyEnvChanges(t *testing.T) {
	makeProbe := func(sourceRoot, outputDir string, pid int64, pwd string) ProbeResult {
		return ProbeResult{
			Records: []trace.Record{{
				PID:       pid,
				ParentPID: 0,
				Argv: []string{
					filepath.Join(sourceRoot, "emit.sh"),
					"--out=" + filepath.Join(outputDir, "share", "config.txt"),
				},
				Cwd: sourceRoot,
				Env: []string{
					"PWD=" + pwd,
					"SHLVL=2",
					"TERM=xterm-256color",
				},
				Changes: []string{
					filepath.Join(outputDir, "share", "config.txt"),
				},
			}},
			OutputDir:   outputDir,
			ReplayReady: true,
			Scope: trace.Scope{
				SourceRoot:  sourceRoot,
				BuildRoot:   filepath.Join(sourceRoot, "_build"),
				InstallRoot: outputDir,
			},
		}
	}

	baseSource := t.TempDir()
	leftSource := t.TempDir()
	rightSource := t.TempDir()
	baseOut := t.TempDir()
	leftOut := t.TempDir()
	rightOut := t.TempDir()

	plan, unavailable := planRootReplay(
		makeProbe(baseSource, baseOut, 100, filepath.Join(baseSource, "work")),
		makeProbe(leftSource, leftOut, 200, filepath.Join(leftSource, "nested", "cwd")),
		makeProbe(rightSource, rightOut, 300, filepath.Join(rightSource, "other", "cwd")),
	)
	if unavailable != "no replay root parameters changed across probes" {
		t.Fatalf("planRootReplay() unavailable = %q, want noise-only env differences to be ignored", unavailable)
	}
	if plan.summary == nil || len(plan.summary.ChangedRoots) != 0 {
		t.Fatalf("planRootReplay() changed roots = %v, want none", plan.summary)
	}
}

func TestSelectReplayJoinFrontierPrunesPreJoinSideBranch(t *testing.T) {
	steps := []replayRoot{
		{writes: []string{"$BUILD/a.txt"}},
		{reads: []string{"$BUILD/a.txt"}, writes: []string{"$BUILD/side.txt"}},
		{writes: []string{"$BUILD/b.txt"}},
		{reads: []string{"$BUILD/a.txt", "$BUILD/b.txt"}, writes: []string{"$INSTALL/out.txt"}},
	}
	changed := map[int]struct{}{0: {}, 2: {}}
	join := map[int]struct{}{3: {}}

	got := selectReplayJoinFrontier(steps, changed, join)
	want := []int{0, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("selectReplayJoinFrontier() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selectReplayJoinFrontier()[%d] = %d, want %d (full=%v)", i, got[i], want[i], got)
		}
	}
}

func TestReplayJoinRootIndexesMapsJoinSetToReplayRoots(t *testing.T) {
	makeProbe := func(sourceRoot string, variant string) ProbeResult {
		buildRoot := filepath.Join(sourceRoot, "build")
		installRoot := filepath.Join(sourceRoot, "out")
		compileAArgv := []string{"cc", "-c", filepath.Join(sourceRoot, "a.c"), "-o", filepath.Join(buildRoot, "a.o")}
		if variant != "" {
			compileAArgv = []string{"cc", "-DFEATURE", "-c", filepath.Join(sourceRoot, "a.c"), "-o", filepath.Join(buildRoot, "a.o")}
		}
		return ProbeResult{
			Records: []trace.Record{
				{
					PID:       100,
					ParentPID: 0,
					Argv:      compileAArgv,
					Cwd:       sourceRoot,
					Inputs:    []string{filepath.Join(sourceRoot, "a.c")},
					Changes:   []string{filepath.Join(buildRoot, "a.o")},
				},
				{
					PID:       200,
					ParentPID: 0,
					Argv:      []string{"cc", "-c", filepath.Join(sourceRoot, "b.c"), "-o", filepath.Join(buildRoot, "b.o")},
					Cwd:       sourceRoot,
					Inputs:    []string{filepath.Join(sourceRoot, "b.c")},
					Changes:   []string{filepath.Join(buildRoot, "b.o")},
				},
				{
					PID:       300,
					ParentPID: 0,
					Argv: []string{
						"cc",
						filepath.Join(buildRoot, "a.o"),
						filepath.Join(buildRoot, "b.o"),
						"-o",
						filepath.Join(buildRoot, "app"),
					},
					Cwd: sourceRoot,
					Inputs: []string{
						filepath.Join(buildRoot, "a.o"),
						filepath.Join(buildRoot, "b.o"),
					},
					Changes: []string{
						filepath.Join(buildRoot, "app"),
					},
				},
			},
			Scope: trace.Scope{
				SourceRoot:  sourceRoot,
				BuildRoot:   buildRoot,
				InstallRoot: installRoot,
			},
		}
	}

	base := makeProbe(t.TempDir(), "")
	probe := makeProbe(t.TempDir(), "left")
	scan := replayRoots(probe)
	got := replayJoinRootIndexes(base, probe, scan)
	if len(got) != 1 {
		t.Fatalf("replayJoinRootIndexes() len = %d, want 1 (%v)", len(got), got)
	}
	if _, ok := got[2]; !ok {
		t.Fatalf("replayJoinRootIndexes() = %v, want root index 2", got)
	}
}

func TestReplayJoinRootIndexesUsesMinJoinRoots(t *testing.T) {
	makeProbe := func(sourceRoot string, feature bool) ProbeResult {
		buildRoot := filepath.Join(sourceRoot, "build")
		installRoot := filepath.Join(sourceRoot, "out")
		compileAArgv := []string{"cc", "-c", filepath.Join(sourceRoot, "a.c"), "-o", filepath.Join(buildRoot, "a.o")}
		if feature {
			compileAArgv = []string{"cc", "-DFEATURE", "-c", filepath.Join(sourceRoot, "a.c"), "-o", filepath.Join(buildRoot, "a.o")}
		}
		return ProbeResult{
			Records: []trace.Record{
				{
					PID:       100,
					ParentPID: 0,
					Argv:      compileAArgv,
					Cwd:       sourceRoot,
					Inputs:    []string{filepath.Join(sourceRoot, "a.c")},
					Changes:   []string{filepath.Join(buildRoot, "a.o")},
				},
				{
					PID:       200,
					ParentPID: 0,
					Argv:      []string{"cc", "-c", filepath.Join(sourceRoot, "b.c"), "-o", filepath.Join(buildRoot, "b.o")},
					Cwd:       sourceRoot,
					Inputs:    []string{filepath.Join(sourceRoot, "b.c")},
					Changes:   []string{filepath.Join(buildRoot, "b.o")},
				},
				{
					PID:       300,
					ParentPID: 0,
					Argv: []string{
						"cc",
						filepath.Join(buildRoot, "a.o"),
						filepath.Join(buildRoot, "b.o"),
						"-o",
						filepath.Join(buildRoot, "app"),
					},
					Cwd: sourceRoot,
					Inputs: []string{
						filepath.Join(buildRoot, "a.o"),
						filepath.Join(buildRoot, "b.o"),
					},
					Changes: []string{
						filepath.Join(buildRoot, "app"),
					},
				},
				{
					PID:       400,
					ParentPID: 0,
					Argv: []string{
						"pkg",
						filepath.Join(buildRoot, "app"),
						filepath.Join(sourceRoot, "manifest.txt"),
						"-o",
						filepath.Join(buildRoot, "app.pkg"),
					},
					Cwd: sourceRoot,
					Inputs: []string{
						filepath.Join(buildRoot, "app"),
						filepath.Join(sourceRoot, "manifest.txt"),
					},
					Changes: []string{
						filepath.Join(buildRoot, "app.pkg"),
					},
				},
				{
					PID:       500,
					ParentPID: 0,
					Argv: []string{
						"/bin/cp",
						filepath.Join(buildRoot, "app.pkg"),
						filepath.Join(installRoot, "app.pkg"),
					},
					Cwd: sourceRoot,
					Inputs: []string{
						filepath.Join(buildRoot, "app.pkg"),
					},
					Changes: []string{
						filepath.Join(installRoot, "app.pkg"),
					},
				},
			},
			Scope: trace.Scope{
				SourceRoot:  sourceRoot,
				BuildRoot:   buildRoot,
				InstallRoot: installRoot,
			},
		}
	}

	base := makeProbe(t.TempDir(), false)
	probe := makeProbe(t.TempDir(), true)
	scan := replayRoots(probe)
	got := replayJoinRootIndexes(base, probe, scan)
	if len(got) != 1 {
		t.Fatalf("replayJoinRootIndexes() len = %d, want 1 (%v)", len(got), got)
	}
	if _, ok := got[2]; !ok {
		t.Fatalf("replayJoinRootIndexes() = %v, want only minimal join root index 2", got)
	}
}
