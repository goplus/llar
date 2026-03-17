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

	for _, token := range []string{"COMBO debug-on", "match base -> debug-on:", "collision base vs debug-on (base=base):"} {
		if !strings.Contains(out, token) {
			t.Fatalf("report output missing %q:\n%s", token, out)
		}
	}
}

func TestDebugSummaryIncludesBusinessGenericActionsWhenRequested(t *testing.T) {
	records := []trace.Record{
		record([]string{"python3", "emit.py"}, "/tmp/work",
			[]string{"/tmp/work/input.cfg"},
			[]string{"/tmp/work/output.bin"}),
		record([]string{"install", "-m644", "/tmp/work/output.bin", "/tmp/work/install/output.bin"}, "/tmp/work",
			[]string{"/tmp/work/output.bin"},
			[]string{"/tmp/work/install/output.bin"}),
	}

	out := DebugSummary(records, DebugSummaryOptions{
		IncludeBusinessGenericLines: true,
	})

	for _, token := range []string{
		"business generic actions (1):",
		"argv: python3 emit.py",
		"reads: " + normalizePath("/tmp/work/input.cfg"),
		"writes: " + normalizePath("/tmp/work/output.bin"),
	} {
		if !strings.Contains(out, token) {
			t.Fatalf("debug summary missing %q:\n%s", token, out)
		}
	}
}

func TestDebugSummaryIncludesInterestingPathFactsWhenRequested(t *testing.T) {
	records := []trace.Record{
		record([]string{"python3", "emit.py"}, "/tmp/work",
			[]string{"/tmp/work/input.cfg"},
			[]string{"/tmp/work/generated.c"}),
		record([]string{"cc", "-c", "/tmp/work/generated.c", "-o", "/tmp/work/generated.o"}, "/tmp/work",
			[]string{"/tmp/work/generated.c"},
			[]string{"/tmp/work/generated.o"}),
	}

	out := DebugSummary(records, DebugSummaryOptions{
		InterestingTokens:           []string{"/generated."},
		IncludeInterestingPathFacts: true,
	})

	for _, token := range []string{
		"match /generated.:",
		"path facts /generated.:",
		normalizePath("/tmp/work/generated.c") + " => propagating",
		"writers (1):",
		"readers (1):",
	} {
		if !strings.Contains(out, token) {
			t.Fatalf("debug summary missing %q:\n%s", token, out)
		}
	}
}
