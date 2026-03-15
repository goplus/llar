package evaluator

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/goplus/llar/internal/trace"
)

type DebugSummaryOptions struct {
	RoleSampleLimit   int
	InterestingLimit  int
	InterestingTokens []string
	Scope             trace.Scope
}

type DebugDiffSummaryOptions struct {
	BaseLabel         string
	ProbeLabel        string
	ActionSampleLimit int
}

type DebugCollisionSummaryOptions struct {
	BaseLabel       string
	LeftLabel       string
	RightLabel      string
	PathSampleLimit int
}

func DebugSummary(records []trace.Record, opts DebugSummaryOptions) string {
	graph := buildGraphWithScope(records, opts.Scope)
	return formatGraphSummary(graph, opts)
}

func debugSummaryProbe(probe ProbeResult, opts DebugSummaryOptions) string {
	merged := opts
	if merged.Scope.SourceRoot == "" && merged.Scope.BuildRoot == "" && merged.Scope.InstallRoot == "" && len(merged.Scope.KeepRoots) == 0 {
		merged.Scope = probe.Scope
	}
	graph := buildGraphWithScopeAndDigests(probe.Records, merged.Scope, probe.InputDigests)
	return formatGraphSummary(graph, merged)
}

func (graph actionGraph) String() string {
	return formatGraphSummary(graph, DebugSummaryOptions{})
}

func formatGraphSummary(graph actionGraph, opts DebugSummaryOptions) string {
	roleLimit := opts.RoleSampleLimit
	if roleLimit <= 0 {
		roleLimit = 12
	}
	interestingLimit := opts.InterestingLimit
	if interestingLimit <= 0 {
		interestingLimit = 12
	}

	var b strings.Builder
	b.WriteString("records=")
	b.WriteString(strconv.Itoa(graph.records))
	b.WriteString(" actions=")
	b.WriteString(strconv.Itoa(len(graph.actions)))
	b.WriteByte('\n')
	b.WriteString("action counts: ")
	b.WriteString(formatActionSummary(graph))
	b.WriteByte('\n')
	b.WriteString("path role counts: ")
	b.WriteString(formatPathRoleSummary(graph))
	b.WriteByte('\n')

	writeRoleSection(&b, graph, roleTooling, roleLimit)
	writeRoleSection(&b, graph, rolePropagating, roleLimit)
	writeRoleSection(&b, graph, roleDelivery, roleLimit)

	for _, token := range opts.InterestingTokens {
		writeInterestingSection(&b, graph, token, interestingLimit)
	}
	return b.String()
}

func DebugDiffSummary(base ProbeResult, probe ProbeResult, opts DebugDiffSummaryOptions) string {
	baseGraph := buildGraphForProbe(base)
	probeGraph := buildGraphForProbe(probe)

	sampleLimit := opts.ActionSampleLimit
	if sampleLimit <= 0 {
		sampleLimit = 8
	}

	matched, baseOnly, probeOnly := matchActionFingerprints(baseGraph, probeGraph)
	profile := diffProfile(baseGraph, probeGraph)

	var b strings.Builder
	b.WriteString("match ")
	if opts.BaseLabel != "" {
		b.WriteString(opts.BaseLabel)
	} else {
		b.WriteString("base")
	}
	b.WriteString(" -> ")
	if opts.ProbeLabel != "" {
		b.WriteString(opts.ProbeLabel)
	} else {
		b.WriteString("probe")
	}
	b.WriteString(":\n")
	b.WriteString("  actions: base=")
	b.WriteString(strconv.Itoa(len(baseGraph.actions)))
	b.WriteString(", probe=")
	b.WriteString(strconv.Itoa(len(probeGraph.actions)))
	b.WriteString(", matched=")
	b.WriteString(strconv.Itoa(matched))
	b.WriteString(", base-only=")
	b.WriteString(strconv.Itoa(len(baseOnly)))
	b.WriteString(", probe-only=")
	b.WriteString(strconv.Itoa(len(probeOnly)))
	b.WriteByte('\n')
	b.WriteString("  profile: propagating-reads=")
	b.WriteString(strconv.Itoa(len(profile.propagatingReads)))
	b.WriteString(", propagating-writes=")
	b.WriteString(strconv.Itoa(len(profile.propagatingWrites)))
	b.WriteString(", unknown-reads=")
	b.WriteString(strconv.Itoa(len(profile.unknownReads)))
	b.WriteString(", unknown-writes=")
	b.WriteString(strconv.Itoa(len(profile.unknownWrites)))
	b.WriteString(", delivery-writes=")
	b.WriteString(strconv.Itoa(len(profile.deliveryWrites)))
	b.WriteString(", tooling-reads=")
	b.WriteString(strconv.Itoa(len(profile.toolingReads)))
	b.WriteString(", tooling-writes=")
	b.WriteString(strconv.Itoa(len(profile.toolingWrites)))
	b.WriteString(", param-touches=")
	b.WriteString(strconv.Itoa(len(profile.paramTouches)))
	b.WriteByte('\n')

	writeActionSamples(&b, "base-only", baseGraph, baseOnly, sampleLimit)
	writeActionSamples(&b, "probe-only", probeGraph, probeOnly, sampleLimit)
	return b.String()
}

func DebugCollisionSummary(base ProbeResult, left ProbeResult, right ProbeResult, opts DebugCollisionSummaryOptions) string {
	baseGraph := buildGraphForProbe(base)
	leftProfile := diffProfile(baseGraph, buildGraphForProbe(left))
	rightProfile := diffProfile(baseGraph, buildGraphForProbe(right))

	limit := opts.PathSampleLimit
	if limit <= 0 {
		limit = 8
	}

	ww := sampleMapOverlapSets([]map[string]struct{}{leftProfile.propagatingWrites, leftProfile.unknownWrites}, []map[string]struct{}{rightProfile.propagatingWrites, rightProfile.unknownWrites}, limit)
	wr := sampleMapOverlapSets([]map[string]struct{}{leftProfile.propagatingWrites, leftProfile.unknownWrites}, []map[string]struct{}{rightProfile.propagatingReads, rightProfile.unknownReads}, limit)
	rw := sampleMapOverlapSets([]map[string]struct{}{leftProfile.propagatingReads, leftProfile.unknownReads}, []map[string]struct{}{rightProfile.propagatingWrites, rightProfile.unknownWrites}, limit)
	param := sampleMapOverlap(leftProfile.paramTouches, rightProfile.paramTouches, limit)

	var b strings.Builder
	b.WriteString("collision ")
	if opts.LeftLabel != "" {
		b.WriteString(opts.LeftLabel)
	} else {
		b.WriteString("left")
	}
	b.WriteString(" vs ")
	if opts.RightLabel != "" {
		b.WriteString(opts.RightLabel)
	} else {
		b.WriteString("right")
	}
	b.WriteString(" (base=")
	if opts.BaseLabel != "" {
		b.WriteString(opts.BaseLabel)
	} else {
		b.WriteString("base")
	}
	b.WriteString("):\n")
	b.WriteString("  collide=")
	if len(ww) > 0 || len(wr) > 0 || len(rw) > 0 || len(param) > 0 {
		b.WriteString("true\n")
	} else {
		b.WriteString("false\n")
	}
	writePathSamples(&b, "write-write", ww)
	writePathSamples(&b, "left-write/right-read", wr)
	writePathSamples(&b, "left-read/right-write", rw)
	writePathSamples(&b, "param-shared", param)
	return b.String()
}

func DebugPathFacts(records []trace.Record, scope trace.Scope, token string) string {
	graph := buildGraphWithScope(records, scope)

	var paths []string
	for path := range graph.paths {
		if strings.Contains(path, token) {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)

	var b strings.Builder
	b.WriteString("path facts ")
	b.WriteString(token)
	b.WriteString(":\n")
	if len(paths) == 0 {
		b.WriteString("  absent\n")
		return b.String()
	}
	for _, path := range paths {
		facts := graph.paths[path]
		b.WriteString("  ")
		b.WriteString(path)
		b.WriteString(" => ")
		b.WriteString(facts.role.String())
		b.WriteByte('\n')
		writePathFactActions(&b, "writers", graph, facts.writers)
		writePathFactActions(&b, "readers", graph, facts.readers)
	}
	return b.String()
}

func formatActionSummary(graph actionGraph) string {
	counts := map[string]int{}
	tooling := 0
	for i, action := range graph.actions {
		counts[action.kind.String()]++
		if i < len(graph.tooling) && graph.tooling[i] {
			tooling++
		}
	}

	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	parts := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		parts = append(parts, key+"="+strconv.Itoa(counts[key]))
	}
	parts = append(parts, "tooling="+strconv.Itoa(tooling))
	return strings.Join(parts, ", ")
}

func formatPathRoleSummary(graph actionGraph) string {
	counts := map[string]int{}
	for _, facts := range graph.paths {
		counts[facts.role.String()]++
	}
	keys := []string{"tooling", "propagating", "delivery", "unknown"}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+strconv.Itoa(counts[key]))
	}
	return strings.Join(parts, ", ")
}

func writeRoleSection(b *strings.Builder, graph actionGraph, role pathRole, limit int) {
	paths := samplePathsByRole(graph, role, limit)
	b.WriteString(role.String())
	b.WriteString(" sample (")
	b.WriteString(strconv.Itoa(len(paths)))
	b.WriteString("):\n")
	for _, path := range paths {
		b.WriteString("  ")
		b.WriteString(path)
		b.WriteByte('\n')
	}
}

func writeInterestingSection(b *strings.Builder, graph actionGraph, token string, limit int) {
	paths := sampleInterestingPaths(graph, token, limit)
	b.WriteString("match ")
	b.WriteString(token)
	b.WriteString(":\n")
	if len(paths) == 0 {
		b.WriteString("  absent\n")
		return
	}
	for _, path := range paths {
		b.WriteString("  ")
		b.WriteString(path)
		b.WriteByte('\n')
	}
}

func writeActionSamples(b *strings.Builder, label string, graph actionGraph, indexes []int, limit int) {
	b.WriteString("  ")
	b.WriteString(label)
	b.WriteString(" sample (")
	if len(indexes) < limit {
		b.WriteString(strconv.Itoa(len(indexes)))
	} else {
		b.WriteString(strconv.Itoa(limit))
	}
	b.WriteString("):\n")
	for _, idx := range sampleActionIndexes(graph, indexes, limit) {
		action := graph.actions[idx]
		b.WriteString("    ")
		b.WriteString(action.actionKey)
		b.WriteString(" :: ")
		b.WriteString(argvSkeleton(action.argv))
		b.WriteByte('\n')
	}
}

func writePathSamples(b *strings.Builder, label string, paths []string) {
	b.WriteString("  ")
	b.WriteString(label)
	b.WriteString(" (")
	b.WriteString(strconv.Itoa(len(paths)))
	b.WriteString("):\n")
	for _, path := range paths {
		b.WriteString("    ")
		b.WriteString(path)
		b.WriteByte('\n')
	}
}

func writePathFactActions(b *strings.Builder, label string, graph actionGraph, indexes []int) {
	b.WriteString("    ")
	b.WriteString(label)
	b.WriteString(" (")
	b.WriteString(strconv.Itoa(len(indexes)))
	b.WriteString("):\n")
	for _, idx := range indexes {
		if idx < 0 || idx >= len(graph.actions) {
			continue
		}
		action := graph.actions[idx]
		b.WriteString("      [")
		b.WriteString(strconv.Itoa(idx))
		b.WriteString("] kind=")
		b.WriteString(action.kind.String())
		b.WriteString(" tooling=")
		b.WriteString(strconv.FormatBool(idx < len(graph.tooling) && graph.tooling[idx]))
		b.WriteString(" business=")
		b.WriteString(strconv.FormatBool(idx < len(graph.business) && graph.business[idx]))
		b.WriteString(" key=")
		b.WriteString(action.actionKey)
		b.WriteString(" :: ")
		b.WriteString(argvSkeleton(action.argv))
		b.WriteByte('\n')
	}
}

func samplePathsByRole(graph actionGraph, role pathRole, limit int) []string {
	paths := make([]string, 0, len(graph.paths))
	for path, facts := range graph.paths {
		if facts.role == role {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	if len(paths) > limit {
		paths = paths[:limit]
	}
	return paths
}

func sampleInterestingPaths(graph actionGraph, token string, limit int) []string {
	paths := make([]string, 0, len(graph.paths))
	for path, facts := range graph.paths {
		if !strings.Contains(path, token) {
			continue
		}
		paths = append(paths, path+" => "+facts.role.String())
	}
	slices.Sort(paths)
	if len(paths) > limit {
		paths = paths[:limit]
	}
	return paths
}

func matchActionFingerprints(base, probe actionGraph) (matched int, baseOnly []int, probeOnly []int) {
	baseRemaining := make(map[string]int, len(base.actions))
	for _, action := range base.actions {
		baseRemaining[action.fingerprint]++
	}

	for i, action := range probe.actions {
		if baseRemaining[action.fingerprint] > 0 {
			baseRemaining[action.fingerprint]--
			matched++
			continue
		}
		probeOnly = append(probeOnly, i)
	}

	for i, action := range base.actions {
		if baseRemaining[action.fingerprint] == 0 {
			continue
		}
		baseRemaining[action.fingerprint]--
		baseOnly = append(baseOnly, i)
	}
	return matched, baseOnly, probeOnly
}

func sampleActionIndexes(graph actionGraph, indexes []int, limit int) []int {
	if len(indexes) == 0 {
		return nil
	}
	sorted := slices.Clone(indexes)
	slices.SortFunc(sorted, func(leftIdx, rightIdx int) int {
		left := graph.actions[leftIdx]
		right := graph.actions[rightIdx]
		if left.actionKey != right.actionKey {
			if left.actionKey < right.actionKey {
				return -1
			}
			return 1
		}
		leftArgv := strings.Join(left.argv, "\x1f")
		rightArgv := strings.Join(right.argv, "\x1f")
		switch {
		case leftArgv < rightArgv:
			return -1
		case leftArgv > rightArgv:
			return 1
		default:
			return 0
		}
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

func sampleMapOverlap(left, right map[string]struct{}, limit int) []string {
	paths := make([]string, 0)
	for path := range left {
		if _, ok := right[path]; ok {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	if len(paths) > limit {
		paths = paths[:limit]
	}
	return paths
}

func sampleMapOverlapSets(leftSets, rightSets []map[string]struct{}, limit int) []string {
	paths := make(map[string]struct{})
	for _, left := range leftSets {
		for _, right := range rightSets {
			for _, path := range sampleMapOverlap(left, right, limit) {
				paths[path] = struct{}{}
			}
		}
	}
	sorted := slices.Collect(maps.Keys(paths))
	slices.Sort(sorted)
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}
