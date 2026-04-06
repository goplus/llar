package evaluator

import (
	"strings"
	"testing"

	"github.com/goplus/llar/internal/trace"
)

func TestActionGraphStringMatchesDefaultSummary(t *testing.T) {
	records := []trace.Record{
		record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/core.c"},
			[]string{"/tmp/work/build/core.o"}),
		record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o"}, "/tmp/work",
			[]string{"/tmp/work/build/core.o"},
			[]string{"/tmp/work/out/lib/libfoo.a"}),
	}

	graph := buildGraph(records)
	got := graph.String()
	want := DebugSummary(records, DebugSummaryOptions{})
	if got != want {
		t.Fatalf("graph.String() mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestDebugReportFormatsSections(t *testing.T) {
	base := ProbeResult{
		Records: []trace.Record{
			record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
		},
		Scope: trace.Scope{SourceRoot: "/tmp/work", BuildRoot: "/tmp/work/build", InstallRoot: "/tmp/work/install"},
	}
	probe := ProbeResult{
		Records: []trace.Record{
			record([]string{"cc", "-DDEBUG", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
		},
		Scope: base.Scope,
	}

	var report DebugReport
	report.AddCombo("debug-on", probe, DebugSummaryOptions{})
	report.AddDiff(base, probe, DebugDiffSummaryOptions{BaseLabel: "base", ProbeLabel: "debug-on"})
	report.AddCollision(base, base, probe, DebugCollisionSummaryOptions{BaseLabel: "base", LeftLabel: "base", RightLabel: "debug-on"})
	out := report.String()

	for _, token := range []string{
		"COMBO debug-on",
		"match base -> debug-on:",
		"seed-states=",
		"need-states=",
		"flow-states=",
		"collision base vs debug-on (base=base):",
		"strict-hazards",
		"merge-aware-hazards",
		"selected-hazards",
		"seed-state-overlap",
		"left-flow/right-need-states",
		"right-flow/left-need-states",
	} {
		if !strings.Contains(out, token) {
			t.Fatalf("report output missing %q:\n%s", token, out)
		}
	}
}

func TestDebugSummaryProbePrefersEvents(t *testing.T) {
	probe := ProbeResult{
		Records: []trace.Record{
			record([]string{"cc", "-c", "old.c", "-o", "build/old.o"}, "/tmp/work",
				[]string{"/tmp/work/old.c"},
				[]string{"/tmp/work/build/old.o"}),
		},
		Events: []trace.Event{
			{Seq: 1, PID: 1234, Cwd: "/tmp/work", Kind: trace.EventExec, Argv: []string{"cc", "-c", "new.c", "-o", "build/new.o"}},
			{Seq: 2, PID: 1234, Cwd: "/tmp/work", Kind: trace.EventRead, Path: "/tmp/work/new.c"},
			{Seq: 3, PID: 1234, Cwd: "/tmp/work", Kind: trace.EventWrite, Path: "/tmp/work/build/new.o"},
		},
	}

	out := debugSummaryProbe(probe, DebugSummaryOptions{})
	for _, token := range []string{"source=events", "/tmp/$$TMP/build/new.o"} {
		if !strings.Contains(out, token) {
			t.Fatalf("debugSummaryProbe() missing %q:\n%s", token, out)
		}
	}
	if strings.Contains(out, "/tmp/work/build/old.o") {
		t.Fatalf("debugSummaryProbe() unexpectedly used record-only path:\n%s", out)
	}
}

func TestDebugMergedPairSummaryIncludesConflictSides(t *testing.T) {
	out := DebugMergedPairSummary(MergedPairObservation{
		Combo: "json-on-xml-on",
		MergeResult: OutputMergeResult{
			Status: OutputMergeStatusNeedsRebuild,
			Issues: []OutputMergeIssue{{
				Kind:   OutputMergeIssueKindMetadataUnmergeable,
				Path:   "<metadata>",
				Reason: "metadata requires real pair build",
				Detail: "both sides changed metadata, and the shared base flags are not a common prefix; a real combined build is required",
				Base:   "-lPocoFoundation",
				Left:   "-lPocoFoundation -lPocoJSON",
				Right:  "-lPocoFoundation -lPocoXML",
			}},
		},
	})

	for _, token := range []string{
		"merged pair json-on-xml-on:",
		"status=needs-rebuild",
		"<metadata> :: metadata requires real pair build [metadata-unmergeable]",
		"detail: both sides changed metadata, and the shared base flags are not a common prefix; a real combined build is required",
		"base: -lPocoFoundation",
		"left: -lPocoFoundation -lPocoJSON",
		"right: -lPocoFoundation -lPocoXML",
	} {
		if !strings.Contains(out, token) {
			t.Fatalf("DebugMergedPairSummary() missing %q:\n%s", token, out)
		}
	}
}

func TestDebugMergedPairSummaryFormatsMultilineDetail(t *testing.T) {
	out := DebugMergedPairSummary(MergedPairObservation{
		Combo: "poco-pair",
		MergeResult: OutputMergeResult{
			Status: OutputMergeStatusNeedsRebuild,
			Issues: []OutputMergeIssue{{
				Kind:   OutputMergeIssueKindArchiveUnmergeable,
				Path:   "lib/libPocoFoundation.a",
				Reason: "path changed on both sides; automatic merge unavailable",
				Detail: "both sides changed this archive relative to base, so automatic archive merge cannot materialize a combined output\nconflicting members (2):\nfoo.o\nbar.o",
			}},
		},
	})

	for _, token := range []string{
		"detail: both sides changed this archive relative to base, so automatic archive merge cannot materialize a combined output",
		"conflicting members (2):",
		"foo.o",
		"bar.o",
	} {
		if !strings.Contains(out, token) {
			t.Fatalf("DebugMergedPairSummary() missing %q:\n%s", token, out)
		}
	}
}

func TestDebugSynthesizedPairSummaryIncludesReplaySummary(t *testing.T) {
	out := DebugSynthesizedPairSummary(SynthesizedPairObservation{
		Combo: "proto-on-log-on",
		SynthesisResult: OutputSynthesisResult{
			Mode:   OutputSynthesisModeRootReplay,
			Status: OutputMergeStatusNeedsRebuild,
			Replay: &RootReplaySummary{
				CandidateRoots: 3,
				EligibleRoots:  2,
				SelectedWrites: 4,
				ChangedRoots: []string{
					"gen_config --out=$INSTALL/share/config.txt [--proto, --log] @ $SRC",
				},
				SelectedRoots: []string{
					"gen_config --out=$INSTALL/share/config.txt [--proto, --log] @ $SRC",
					"pack --in=$BUILD/generated.txt --out=$INSTALL/share/final.txt @ $SRC",
				},
				Unavailable: "shell command wrapper at replay root \"sh -c ...\" is unsupported",
			},
			Issues: []OutputSynthesisIssue{{
				Kind:   OutputMergeIssueKindRootReplayUnavailable,
				Path:   "<root-replay>",
				Reason: "root replay is unavailable",
				Detail: "shell command wrapper at replay root \"sh -c ...\" is unsupported",
			}},
		},
	})

	for _, token := range []string{
		"synthesized pair proto-on-log-on:",
		"mode=root-replay",
		"replay: candidates=3, eligible=2, changed=1, selected=2, selected-writes=4",
		"replay-unavailable: shell command wrapper at replay root \"sh -c ...\" is unsupported",
		"replay-changed-roots:",
		"replay-selected-roots:",
	} {
		if !strings.Contains(out, token) {
			t.Fatalf("DebugSynthesizedPairSummary() missing %q:\n%s", token, out)
		}
	}
}
