package evaluator

import (
	"os"
	"strings"
	"testing"

	"github.com/goplus/llar/internal/trace"
)

func TestDebugBoostTraceB2Chain(t *testing.T) {
	if os.Getenv("LLAR_DEBUG_BOOST_TRACE") == "" {
		t.Skip("set LLAR_DEBUG_BOOST_TRACE to run")
	}

	data, err := os.ReadFile("/Users/haolan/project/llar/.llar-e2e-logs/TestE2E_Watch_RealOptionClassification_BoostProgramOptionsTimer-trace-1775292095588209761.log")
	if err != nil {
		t.Fatal(err)
	}
	combos, err := parseTraceCombosForTest(string(data))
	if err != nil {
		t.Fatal(err)
	}
	probe, ok := combos["program_options-on-timer-off"]
	if !ok {
		t.Fatal("missing program_options-on-timer-off")
	}

	graph := buildGraphForProbe(probe)
	targets := []string{
		normalizePath("/tmp/$$TMP/003/.trace-src/boostorg/boost@boost-1.90.0-program_options-on-timer-off/tools/build/src/engine/b2"),
		normalizePath("/tmp/$$TMP/003/.trace-src/boostorg/boost@boost-1.90.0-program_options-on-timer-off/b2"),
	}
	for _, path := range targets {
		facts, ok := graph.paths[path]
		if !ok {
			t.Fatalf("missing path facts for %s", path)
		}
		t.Logf("path=%s role=%v writers=%v readers=%v", path, facts.role, facts.writers, facts.readers)
		for _, idx := range facts.writers {
			t.Logf(" writer[%d] tooling=%v kind=%v argv=%v exec=%q writes=%v reads=%v", idx, graph.tooling[idx], graph.actions[idx].kind, graph.actions[idx].argv, graph.actions[idx].execPath, graph.actions[idx].writes, graph.actions[idx].reads)
		}
		for _, idx := range facts.readers {
			t.Logf(" reader[%d] tooling=%v kind=%v argv=%v exec=%q writes=%v reads=%v", idx, graph.tooling[idx], graph.actions[idx].kind, graph.actions[idx].argv, graph.actions[idx].execPath, graph.actions[idx].writes, graph.actions[idx].reads)
		}
	}

	for i, action := range graph.actions {
		if action.execPath == targets[1] || action.execPath == targets[0] {
			t.Logf("exec action[%d] tooling=%v kind=%v argv=%v exec=%q", i, graph.tooling[i], action.kind, action.argv, action.execPath)
		}
	}
}

func parseTraceCombosForTest(data string) (map[string]ProbeResult, error) {
	return parseTraceCombos(data)
}

func parseTraceCombos(data string) (map[string]ProbeResult, error) {
	combos := make(map[string]ProbeResult)
	current := ""
	var block []string
	flush := func() error {
		if current == "" {
			return nil
		}
		probe, err := parseTraceComboBlock(block)
		if err != nil {
			return err
		}
		combos[current] = probe
		return nil
	}

	lines := splitLines(data)
	for _, line := range lines {
		if len(line) > 6 && line[:6] == "COMBO " {
			if err := flush(); err != nil {
				return nil, err
			}
			current = line[6:]
			block = block[:0]
			continue
		}
		if current != "" {
			block = append(block, line)
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return combos, nil
}

func splitLines(data string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] != '\n' {
			continue
		}
		out = append(out, data[start:i])
		start = i + 1
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

func parseTraceComboBlock(lines []string) (ProbeResult, error) {
	var events []trace.Event
	var records []trace.Record
	var current trace.Record
	haveCurrent := false
	for _, line := range lines {
		if line == "" || line == "DIAGNOSTICS" || line == "INPUT_DIGESTS" {
			break
		}
		if isTraceRecordStart(line) {
			if haveCurrent {
				records = append(records, current)
			}
			current = trace.Record{}
			haveCurrent = true
			current.Argv = parseTraceArgv(line)
			continue
		}
		if !haveCurrent {
			continue
		}
		switch {
		case hasPrefix(line, "   cwd: "):
			current.Cwd = line[len("   cwd: "):]
		case hasPrefix(line, "   pid: "):
			current.PID = parseInt64(line[len("   pid: "):])
		case hasPrefix(line, "   ppid: "):
			current.ParentPID = parseInt64(line[len("   ppid: "):])
		case hasPrefix(line, "   inputs: "):
			current.Inputs = parseTracePaths(line[len("   inputs: "):])
		case hasPrefix(line, "   changes: "):
			current.Changes = parseTracePaths(line[len("   changes: "):])
		}
		events = events
	}
	if haveCurrent {
		records = append(records, current)
	}
	return ProbeResult{Records: records, Events: events}, nil
}

func isTraceRecordStart(line string) bool {
	dot := -1
	for i := 0; i < len(line); i++ {
		if line[i] == '.' {
			dot = i
			break
		}
		if line[i] < '0' || line[i] > '9' {
			return false
		}
	}
	if dot <= 0 {
		return false
	}
	return hasPrefix(line[dot:], ". argv: ")
}

func parseTraceArgv(line string) []string {
	idx := 0
	for idx < len(line) && line[idx] >= '0' && line[idx] <= '9' {
		idx++
	}
	idx += len(". argv: ")
	return splitTraceFields(line[idx:])
}

func splitTraceFields(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

func hasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

func parseTracePaths(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for start < len(s) {
		next := start
		for next < len(s) {
			if next+1 < len(s) && s[next] == ',' && s[next+1] == ' ' {
				break
			}
			next++
		}
		out = append(out, s[start:next])
		start = next + 2
	}
	return out
}

func parseInt64(s string) int64 {
	var n int64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			break
		}
		n = n*10 + int64(s[i]-'0')
	}
	return n
}
