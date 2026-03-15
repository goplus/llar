package evaluator

import (
	"strconv"
	"strings"

	"github.com/goplus/llar/internal/trace"
)

type DebugReport struct {
	builder strings.Builder
}

func (report *DebugReport) AddCombo(combo string, probe ProbeResult, opts DebugSummaryOptions) {
	report.appendSection("COMBO " + combo + "\n" + debugSummaryProbe(probe, opts))
}

func (report *DebugReport) AddDiff(base, probe ProbeResult, opts DebugDiffSummaryOptions) {
	report.appendSection(DebugDiffSummary(base, probe, opts))
}

func (report *DebugReport) AddCollision(base, left, right ProbeResult, opts DebugCollisionSummaryOptions) {
	report.appendSection(DebugCollisionSummary(base, left, right, opts))
}

func (report *DebugReport) AddPathFacts(records []trace.Record, scope trace.Scope, token string) {
	report.appendSection(DebugPathFacts(records, scope, token))
}

func (report *DebugReport) AddTraceMatches(records []trace.Record, tokens []string, limit int) {
	report.appendSection(DebugTraceMatches(records, tokens, limit))
}

func (report *DebugReport) String() string {
	return report.builder.String()
}

func (report *DebugReport) appendSection(section string) {
	section = strings.TrimRight(section, "\n")
	if section == "" {
		return
	}
	if report.builder.Len() > 0 {
		report.builder.WriteString("\n\n")
	}
	report.builder.WriteString(section)
	report.builder.WriteByte('\n')
}

func DebugTraceMatches(records []trace.Record, tokens []string, limit int) string {
	if limit <= 0 {
		limit = 8
	}
	var b strings.Builder
	b.WriteString("trace matches:\n")

	matched := 0
	for _, rec := range records {
		found := false
		for _, token := range tokens {
			if token == "" {
				continue
			}
			for _, arg := range rec.Argv {
				if strings.Contains(arg, token) {
					found = true
					break
				}
			}
			if found {
				break
			}
			for _, path := range rec.Inputs {
				if strings.Contains(path, token) {
					found = true
					break
				}
			}
			if found {
				break
			}
			for _, path := range rec.Changes {
				if strings.Contains(path, token) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			continue
		}
		b.WriteString("  argv: ")
		b.WriteString(strings.Join(rec.Argv, " "))
		b.WriteByte('\n')
		if rec.PID != 0 {
			b.WriteString("    pid: ")
			b.WriteString(strconv.FormatInt(rec.PID, 10))
			b.WriteByte('\n')
		}
		if rec.ParentPID != 0 {
			b.WriteString("    ppid: ")
			b.WriteString(strconv.FormatInt(rec.ParentPID, 10))
			b.WriteByte('\n')
		}
		if len(rec.Inputs) > 0 {
			b.WriteString("    inputs: ")
			b.WriteString(strings.Join(rec.Inputs, ", "))
			b.WriteByte('\n')
		}
		if len(rec.Changes) > 0 {
			b.WriteString("    changes: ")
			b.WriteString(strings.Join(rec.Changes, ", "))
			b.WriteByte('\n')
		}
		matched++
		if matched >= limit {
			break
		}
	}
	if matched == 0 {
		b.WriteString("  absent\n")
	}
	return b.String()
}
