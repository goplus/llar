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
