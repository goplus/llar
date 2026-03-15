package evaluator

import (
	"os"
	"strings"
	"testing"
)

// TestE2E_ReplayTraceDumpGraphSummary replays a captured build trace, rebuilds
// the evaluator graph, and writes a human-readable classification summary.
func TestE2E_ReplayTraceDumpGraphSummary(t *testing.T) {
	path := strings.TrimSpace(os.Getenv("LLAR_TRACE_REPLAY"))
	if path == "" {
		t.Skip("set LLAR_TRACE_REPLAY to a trace dump file")
	}

	records := readTraceDumpFile(t, path)
	summary := DebugSummary(records, DebugSummaryOptions{
		RoleSampleLimit:  12,
		InterestingLimit: 12,
		InterestingTokens: []string{
			"/_build/zconf.h",
			"/TryCompile-",
			"/libz.so.1.3.1",
			"/include/zlib.h",
			"/zlib.map",
		},
	})

	var b strings.Builder
	b.WriteString("trace file: ")
	b.WriteString(path)
	b.WriteByte('\n')
	b.WriteString(summary)

	logPath := writeEvaluatorLogForTest(t, b.String())
	t.Logf("graph summary written to %s", logPath)
}

func writeEvaluatorLogForTest(t *testing.T, dump string) string {
	t.Helper()

	path := os.Getenv("LLAR_EVALUATOR_LOG")
	if path == "" {
		f, err := os.CreateTemp("", "llar-evaluator-*.log")
		if err != nil {
			t.Fatalf("create evaluator log: %v", err)
		}
		path = f.Name()
		if err := f.Close(); err != nil {
			t.Fatalf("close evaluator log: %v", err)
		}
	}

	if err := os.WriteFile(path, []byte(dump), 0o644); err != nil {
		t.Fatalf("write evaluator log %s: %v", path, err)
	}
	return path
}
