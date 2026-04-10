package ssa

import (
	"testing"

	"github.com/goplus/llar/internal/trace"
)

func TestNormalizeScopeTokenHeuristicBuildNoise(t *testing.T) {
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{
			name:  "scratch workspace child",
			token: "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects",
			want:  "$BUILD/CMakeFiles/$TMPDIR/TryCompile-$ID/CMakeFiles/pkgRedirects",
		},
		{
			name:  "generated scratch artifact",
			token: "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/cmTC_deadbeef",
			want:  "$BUILD/CMakeFiles/$TMPDIR/TryCompile-$ID/cmTC_$ID",
		},
		{
			name:  "generic temp subtree",
			token: "/tmp/work/_build/probe/tmp/job-doc/result_4f3e2d1c.dir",
			want:  "$BUILD/probe/$TMPDIR/job-$ID/result_$ID.dir",
		},
		{
			name:  "tmp pid suffix",
			token: "/tmp/work/_build/cache/output.tmp.12345",
			want:  "$BUILD/cache/output.tmp.$ID",
		},
		{
			name:  "stable build artifact",
			token: "/tmp/work/_build/libtracecore.a",
			want:  "$BUILD/libtracecore.a",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeScopeToken(tc.token, scope); got != tc.want {
				t.Fatalf("normalizeScopeToken(%q) = %q, want %q", tc.token, got, tc.want)
			}
		})
	}
}

func TestPathLooksDeliveryExcludesTransientWorkspaceCopy(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/_build",
	}
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{"/tmp/work/_build/probe-checks/input.txt"}),
		recordWithProc(101, 100, []string{"cp", "input.txt", "status.txt"}, "/tmp/work/_build/probe-checks",
			[]string{"/tmp/work/_build/probe-checks/input.txt"},
			[]string{"/tmp/work/_build/probe-checks/status.txt"}),
	}

	graph := BuildGraph(BuildInput{Records: records, Scope: scope})
	if got := pathLooksDelivery(graph, "/tmp/work/_build/probe-checks/status.txt"); got {
		t.Fatal("probe workspace leaf copy should not be classified as delivery")
	}
}

func TestInferMainlineVisibleDefsUsesObservableConsumers(t *testing.T) {
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{"/tmp/work/_build/control-plane"}),
		recordWithProc(110, 100, []string{"gmake", "-f", "/tmp/work/_build/control-plane"}, "/tmp/work/_build",
			[]string{"/tmp/work/_build/control-plane"},
			nil),
		recordWithProc(200, 1, []string{"cc", "/tmp/work/cli.c", "-o", "/tmp/work/_build/tracecli"}, "/tmp/work/_build",
			[]string{"/tmp/work/cli.c"},
			[]string{"/tmp/work/_build/tracecli"}),
		recordWithProc(300, 1, []string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/_build/tracecli"},
			[]string{"/tmp/work/install/bin/tracecli"}),
	}

	graph := BuildGraph(BuildInput{Records: records, Scope: scope})
	deliveryOnly := make([]bool, len(graph.Actions))
	for idx := range graph.Actions {
		deliveryOnly[idx] = isDeliveryOnlyAction(graph, idx)
	}
	toolingFamily := classifyToolingFamily(graph)
	toolingFamily = expandToolingFamily(graph, toolingFamily, deliveryOnly)
	toolingWorkspaceRoots := inferToolingWorkspaceRoots(graph, toolingFamily, deliveryOnly)
	nonEscapingToolingDefs := classifyNonEscapingToolingDefs(graph, toolingFamily, deliveryOnly, toolingWorkspaceRoots)
	mainlineVisibleDefs := inferMainlineVisibleDefs(graph, toolingFamily, toolingWorkspaceRoots, nonEscapingToolingDefs)

	controlPlaneVisible := false
	for _, def := range graph.DefsByPath[normalizePath("/tmp/work/_build/control-plane")] {
		if defBelongsToMainlineVisibleClosure(mainlineVisibleDefs, def) {
			controlPlaneVisible = true
		}
	}
	if controlPlaneVisible {
		t.Fatal("configure control-plane leaf should stay outside the mainline-visible closure")
	}

	traceCLIVisible := false
	for _, def := range graph.DefsByPath[normalizePath("/tmp/work/_build/tracecli")] {
		if defBelongsToMainlineVisibleClosure(mainlineVisibleDefs, def) {
			traceCLIVisible = true
		}
	}
	if !traceCLIVisible {
		t.Fatal("installed top-level build leaf should enter the mainline-visible closure")
	}
}

func TestInferToolingWorkspaceRootsUsesActionEvidence(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/_build",
	}
	records := []trace.Record{
		recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
			[]string{"/tmp/work/CMakeLists.txt"},
			[]string{
				"/tmp/work/_build/probe-checks/CheckFeature.c",
				"/tmp/work/_build/config.h",
			}),
		recordWithProc(110, 100, []string{"cc", "-c", "CheckFeature.c", "-o", "CheckFeature.c.o"}, "/tmp/work/_build/probe-checks",
			[]string{"/tmp/work/_build/probe-checks/CheckFeature.c", "/usr/include/stdio.h"},
			[]string{"/tmp/work/_build/probe-checks/CheckFeature.c.o"}),
		recordWithProc(200, 100, []string{"cc", "/tmp/work/main.c", "/tmp/work/_build/config.h", "-o", "/tmp/work/_build/app"}, "/tmp/work/_build",
			[]string{
				"/tmp/work/main.c",
				"/tmp/work/_build/config.h",
			},
			[]string{"/tmp/work/_build/app"}),
	}

	graph := BuildGraph(BuildInput{Records: records, Scope: scope})
	deliveryOnly := make([]bool, len(graph.Actions))
	for idx := range graph.Actions {
		deliveryOnly[idx] = isDeliveryOnlyAction(graph, idx)
	}
	toolingFamily := classifyToolingFamily(graph)
	toolingFamily = expandToolingFamily(graph, toolingFamily, deliveryOnly)
	roots := inferToolingWorkspaceRoots(graph, toolingFamily, deliveryOnly)

	if !pathBelongsToToolingWorkspace(roots, "/tmp/work/_build/probe-checks/CheckFeature.c.o") {
		t.Fatal("probe-checks object should belong to an inferred tooling workspace root")
	}
	if pathBelongsToToolingWorkspace(roots, "/tmp/work/_build/app") {
		t.Fatal("mainline app should not belong to an inferred tooling workspace root")
	}
	if pathBelongsToToolingWorkspace(roots, "/tmp/work/_build/config.h") {
		t.Fatal("shared configure output at build root should not become a tooling workspace root member")
	}
}
