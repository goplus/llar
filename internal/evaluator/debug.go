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
	BaseLabel         string
	LeftLabel         string
	RightLabel        string
	PathSampleLimit   int
	AllowMergeSurface bool
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
	probe.Scope = merged.Scope
	graph := buildGraphForProbe(probe)
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
	b.WriteString("observations: source=")
	b.WriteString(graph.source.String())
	b.WriteString(", records=")
	b.WriteString(strconv.Itoa(graph.records))
	b.WriteString(", events=")
	b.WriteString(strconv.Itoa(graph.events))
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
	impact := analyzeImpactWithEvidence(baseGraph, probeGraph, buildImpactEvidence(base, probe))

	sampleLimit := opts.ActionSampleLimit
	if sampleLimit <= 0 {
		sampleLimit = 8
	}

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
	b.WriteString(strconv.Itoa(impact.matched))
	b.WriteString(", base-only=")
	b.WriteString(strconv.Itoa(len(impact.baseOnly)))
	b.WriteString(", probe-only=")
	b.WriteString(strconv.Itoa(len(impact.probeOnly)))
	b.WriteByte('\n')
	b.WriteString("  impact: affected-pairs=")
	b.WriteString(strconv.Itoa(len(impact.affectedPairs)))
	b.WriteString(", mutation-roots=")
	b.WriteString(strconv.Itoa(len(impact.rootProbe)))
	b.WriteString(", flow-actions=")
	b.WriteString(strconv.Itoa(len(impact.flowProbe)))
	b.WriteString(", frontier-actions=")
	b.WriteString(strconv.Itoa(len(impact.frontierProbe)))
	b.WriteString(", seed-writes=")
	b.WriteString(strconv.Itoa(len(impact.profile.seedWrites)))
	b.WriteString(", seed-states=")
	b.WriteString(strconv.Itoa(len(impact.profile.seedStates)))
	b.WriteString(", need-paths=")
	b.WriteString(strconv.Itoa(len(impact.profile.needPaths)))
	b.WriteString(", need-states=")
	b.WriteString(strconv.Itoa(len(impact.profile.needStates)))
	b.WriteString(", slice-paths=")
	b.WriteString(strconv.Itoa(len(impact.profile.slicePaths)))
	b.WriteString(", flow-states=")
	b.WriteString(strconv.Itoa(len(impact.profile.flowStates)))
	b.WriteString(", ambiguous=")
	b.WriteString(strconv.FormatBool(impact.profile.ambiguous))
	b.WriteByte('\n')

	writeActionPairSamples(&b, "affected-pairs", baseGraph, probeGraph, impact.affectedPairs, sampleLimit)
	writeActionSamples(&b, "mutation-roots", probeGraph, impact.rootProbe, sampleLimit)
	writeActionSamples(&b, "flow-actions", probeGraph, impact.flowProbe, sampleLimit)
	writeActionSamples(&b, "frontier-actions", probeGraph, impact.frontierProbe, sampleLimit)
	writeActionSamples(&b, "base-only", baseGraph, impact.baseOnly, sampleLimit)
	writeActionSamples(&b, "probe-only", probeGraph, impact.probeOnly, sampleLimit)
	writePathSamples(&b, "seed-writes", sampleMapKeys(impact.profile.seedWrites, sampleLimit))
	writePathSamples(&b, "seed-states", sampleStateKeys(impact.profile.seedStates, sampleLimit))
	writePathSamples(&b, "need-paths", sampleMapKeys(impact.profile.needPaths, sampleLimit))
	writePathSamples(&b, "need-states", sampleStateKeys(impact.profile.needStates, sampleLimit))
	writePathSamples(&b, "slice-paths", sampleMapKeys(impact.profile.slicePaths, sampleLimit))
	writePathSamples(&b, "flow-states", sampleStateKeys(impact.profile.flowStates, sampleLimit))
	return b.String()
}

func DebugCollisionSummary(base ProbeResult, left ProbeResult, right ProbeResult, opts DebugCollisionSummaryOptions) string {
	baseGraph := buildGraphForProbe(base)
	leftGraph := buildGraphForProbe(left)
	rightGraph := buildGraphForProbe(right)
	leftImpact := analyzeImpactWithEvidence(baseGraph, leftGraph, buildImpactEvidence(base, left))
	rightImpact := analyzeImpactWithEvidence(baseGraph, rightGraph, buildImpactEvidence(base, right))
	leftVariant := optionVariant{
		profile:           leftImpact.profile,
		outputDiff:        diffOutputManifest(base.OutputManifest, left.OutputManifest),
		mergeSurfacePaths: mergeSurfacePaths(left.Scope, base.OutputManifest, left.OutputManifest),
	}
	rightVariant := optionVariant{
		profile:           rightImpact.profile,
		outputDiff:        diffOutputManifest(base.OutputManifest, right.OutputManifest),
		mergeSurfacePaths: mergeSurfacePaths(right.Scope, base.OutputManifest, right.OutputManifest),
	}

	limit := opts.PathSampleLimit
	if limit <= 0 {
		limit = 8
	}

	seed := sampleMapOverlap(leftImpact.profile.seedWrites, rightImpact.profile.seedWrites, limit)
	seedStates := sampleStateOverlap(leftImpact.profile.seedStates, rightImpact.profile.seedStates, limit)
	leftNeed := sampleMapOverlap(leftImpact.profile.slicePaths, rightImpact.profile.needPaths, limit)
	leftNeedStates := sampleStateOverlap(leftImpact.profile.flowStates, rightImpact.profile.needStates, limit)
	rightNeed := sampleMapOverlap(rightImpact.profile.slicePaths, leftImpact.profile.needPaths, limit)
	rightNeedStates := sampleStateOverlap(rightImpact.profile.flowStates, leftImpact.profile.needStates, limit)
	shared := sampleMapOverlap(leftImpact.profile.slicePaths, rightImpact.profile.slicePaths, limit*4)
	onMerge, offMerge := partitionSharedPaths(shared, leftVariant.mergeSurfacePaths, rightVariant.mergeSurfacePaths, limit)
	strictAssessment := assessOptionVariantCollision(leftVariant, rightVariant, false)
	mergeAwareAssessment := assessOptionVariantCollision(leftVariant, rightVariant, true)
	selectedAssessment := assessOptionVariantCollision(leftVariant, rightVariant, opts.AllowMergeSurface)
	strict := strictAssessment.collide()
	mergeAware := mergeAwareAssessment.collide()
	selected := selectedAssessment.collide()

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
	b.WriteString(strconv.FormatBool(selected))
	b.WriteByte('\n')
	b.WriteString("  strict-collide=")
	b.WriteString(strconv.FormatBool(strict))
	b.WriteString(", merge-aware-collide=")
	b.WriteString(strconv.FormatBool(mergeAware))
	b.WriteByte('\n')
	writePathSamples(&b, "strict-hazards", formatHazards(strictAssessment.hazards))
	writePathSamples(&b, "merge-aware-hazards", formatHazards(mergeAwareAssessment.hazards))
	writePathSamples(&b, "selected-hazards", formatHazards(selectedAssessment.hazards))
	b.WriteString("  ambiguous: left=")
	b.WriteString(strconv.FormatBool(leftImpact.profile.ambiguous))
	b.WriteString(", right=")
	b.WriteString(strconv.FormatBool(rightImpact.profile.ambiguous))
	b.WriteByte('\n')
	writePathSamples(&b, "seed-overlap", seed)
	writePathSamples(&b, "seed-state-overlap", seedStates)
	writePathSamples(&b, "left-slice/right-need", leftNeed)
	writePathSamples(&b, "left-flow/right-need-states", leftNeedStates)
	writePathSamples(&b, "right-slice/left-need", rightNeed)
	writePathSamples(&b, "right-flow/left-need-states", rightNeedStates)
	writePathSamples(&b, "shared-slice/off-merge-surface", offMerge)
	writePathSamples(&b, "shared-slice/on-merge-surface", onMerge)
	return b.String()
}

func DebugProbeTraceMatches(probe ProbeResult, tokens []string, limit int) string {
	if len(probe.Events) > 0 {
		return DebugTraceMatchesEvents(probe.Events, tokens, limit)
	}
	return DebugTraceMatches(probe.Records, tokens, limit)
}

func DebugSynthesizedPairSummary(observation SynthesizedPairObservation) string {
	var b strings.Builder
	b.WriteString("synthesized pair ")
	b.WriteString(observation.Combo)
	b.WriteString(":\n")
	b.WriteString("  mode=")
	b.WriteString(string(observation.SynthesisResult.Mode))
	b.WriteByte('\n')
	b.WriteString("  status=")
	b.WriteString(string(observation.SynthesisResult.Status))
	b.WriteByte('\n')
	b.WriteString("  clean=")
	if observation.SynthesisResult.Clean() {
		b.WriteString("true\n")
	} else {
		b.WriteString("false\n")
	}
	if observation.ValidationAttempted {
		b.WriteString("  validated=")
		if observation.Validated {
			b.WriteString("true\n")
		} else {
			b.WriteString("false\n")
			if observation.ValidationDetail != "" {
				b.WriteString("  validation-detail: ")
				b.WriteString(observation.ValidationDetail)
				b.WriteByte('\n')
			}
		}
	}
	if observation.SynthesisResult.Replay != nil {
		replay := observation.SynthesisResult.Replay
		b.WriteString("  replay: candidates=")
		b.WriteString(strconv.Itoa(replay.CandidateRoots))
		b.WriteString(", eligible=")
		b.WriteString(strconv.Itoa(replay.EligibleRoots))
		b.WriteString(", changed=")
		b.WriteString(strconv.Itoa(len(replay.ChangedRoots)))
		b.WriteString(", selected=")
		b.WriteString(strconv.Itoa(len(replay.SelectedRoots)))
		b.WriteString(", selected-writes=")
		b.WriteString(strconv.Itoa(replay.SelectedWrites))
		b.WriteByte('\n')
		if replay.Unavailable != "" {
			b.WriteString("  replay-unavailable: ")
			b.WriteString(replay.Unavailable)
			b.WriteByte('\n')
		}
		writeDebugList(&b, "replay-changed-roots", replay.ChangedRoots)
		writeDebugList(&b, "replay-selected-roots", replay.SelectedRoots)
		writeDebugList(&b, "replay-selected-commands", replay.SelectedCommands)
	}
	if len(observation.SynthesisResult.Issues) == 0 {
		b.WriteString("  issues: none\n")
		return b.String()
	}
	b.WriteString("  issues (")
	b.WriteString(strconv.Itoa(len(observation.SynthesisResult.Issues)))
	b.WriteString("):\n")
	for _, issue := range observation.SynthesisResult.Issues {
		b.WriteString("    ")
		b.WriteString(issue.Path)
		b.WriteString(" :: ")
		b.WriteString(issue.Reason)
		if issue.Kind != "" {
			b.WriteString(" [")
			b.WriteString(string(issue.Kind))
			b.WriteString("]")
		}
		b.WriteByte('\n')
		if issue.Detail != "" {
			lines := strings.Split(issue.Detail, "\n")
			for i, line := range lines {
				if i == 0 {
					b.WriteString("      detail: ")
				} else {
					b.WriteString("              ")
				}
				b.WriteString(line)
				b.WriteByte('\n')
			}
		}
		if issue.Base != "" {
			b.WriteString("      base: ")
			b.WriteString(issue.Base)
			b.WriteByte('\n')
		}
		if issue.Left != "" {
			b.WriteString("      left: ")
			b.WriteString(issue.Left)
			b.WriteByte('\n')
		}
		if issue.Right != "" {
			b.WriteString("      right: ")
			b.WriteString(issue.Right)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeDebugList(b *strings.Builder, label string, values []string) {
	if len(values) == 0 {
		return
	}
	b.WriteString("  ")
	b.WriteString(label)
	b.WriteString(":\n")
	for _, value := range values {
		b.WriteString("    ")
		b.WriteString(value)
		b.WriteByte('\n')
	}
}

func DebugMergedPairSummary(observation MergedPairObservation) string {
	var b strings.Builder
	b.WriteString("merged pair ")
	b.WriteString(observation.Combo)
	b.WriteString(":\n")
	b.WriteString("  status=")
	b.WriteString(string(observation.MergeResult.Status))
	b.WriteByte('\n')
	b.WriteString("  clean=")
	if observation.MergeResult.Clean() {
		b.WriteString("true\n")
	} else {
		b.WriteString("false\n")
	}
	if observation.ValidationAttempted {
		b.WriteString("  validated=")
		if observation.Validated {
			b.WriteString("true\n")
		} else {
			b.WriteString("false\n")
		}
	}
	if len(observation.MergeResult.Issues) == 0 {
		b.WriteString("  issues: none\n")
		return b.String()
	}
	b.WriteString("  issues (")
	b.WriteString(strconv.Itoa(len(observation.MergeResult.Issues)))
	b.WriteString("):\n")
	for _, issue := range observation.MergeResult.Issues {
		b.WriteString("    ")
		b.WriteString(issue.Path)
		b.WriteString(" :: ")
		b.WriteString(issue.Reason)
		if issue.Kind != "" {
			b.WriteString(" [")
			b.WriteString(string(issue.Kind))
			b.WriteString("]")
		}
		b.WriteByte('\n')
		if issue.Detail != "" {
			lines := strings.Split(issue.Detail, "\n")
			for i, line := range lines {
				if i == 0 {
					b.WriteString("      detail: ")
				} else {
					b.WriteString("              ")
				}
				b.WriteString(line)
				b.WriteByte('\n')
			}
		}
		if issue.Base != "" {
			b.WriteString("      base: ")
			b.WriteString(issue.Base)
			b.WriteByte('\n')
		}
		if issue.Left != "" {
			b.WriteString("      left: ")
			b.WriteString(issue.Left)
			b.WriteByte('\n')
		}
		if issue.Right != "" {
			b.WriteString("      right: ")
			b.WriteString(issue.Right)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
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
	keys := []string{"tooling", "propagating", "delivery"}
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

func writeActionPairSamples(b *strings.Builder, label string, base, probe actionGraph, pairs []actionPair, limit int) {
	if limit <= 0 {
		limit = 8
	}
	b.WriteString("  ")
	b.WriteString(label)
	b.WriteString(" sample (")
	if len(pairs) < limit {
		b.WriteString(strconv.Itoa(len(pairs)))
	} else {
		b.WriteString(strconv.Itoa(limit))
	}
	b.WriteString("):\n")
	for _, pair := range sampleActionPairs(base, probe, pairs, limit) {
		baseAction := base.actions[pair.baseIdx]
		probeAction := probe.actions[pair.probeIdx]
		b.WriteString("    ")
		b.WriteString(baseAction.actionKey)
		b.WriteString(" => ")
		b.WriteString(probeAction.actionKey)
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
	if limit > 0 && len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

func sampleActionPairs(base, probe actionGraph, pairs []actionPair, limit int) []actionPair {
	if len(pairs) == 0 {
		return nil
	}
	sorted := slices.Clone(pairs)
	slices.SortFunc(sorted, func(left, right actionPair) int {
		leftBase := base.actions[left.baseIdx].actionKey
		rightBase := base.actions[right.baseIdx].actionKey
		if leftBase != rightBase {
			if leftBase < rightBase {
				return -1
			}
			return 1
		}
		leftProbe := probe.actions[left.probeIdx].actionKey
		rightProbe := probe.actions[right.probeIdx].actionKey
		switch {
		case leftProbe < rightProbe:
			return -1
		case leftProbe > rightProbe:
			return 1
		default:
			return 0
		}
	})
	if limit > 0 && len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

func sampleMapKeys(values map[string]struct{}, limit int) []string {
	out := slices.Collect(maps.Keys(values))
	slices.Sort(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
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

func sampleStateKeys(values map[pathStateKey]struct{}, limit int) []string {
	out := make([]string, 0, len(values))
	for state := range values {
		out = append(out, formatStateKey(state))
	}
	slices.Sort(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func sampleStateOverlap(left, right map[pathStateKey]struct{}, limit int) []string {
	out := make([]string, 0)
	for state := range left {
		if _, ok := right[state]; ok {
			out = append(out, formatStateKey(state))
		}
	}
	slices.Sort(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func formatStateKey(state pathStateKey) string {
	if state.tombstone {
		return state.path + " [tombstone]"
	}
	return state.path + " [live]"
}

func formatHazards(hazards []collisionHazardKind) []string {
	if len(hazards) == 0 {
		return nil
	}
	out := make([]string, 0, len(hazards))
	for _, hazard := range hazards {
		out = append(out, string(hazard))
	}
	return out
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

func partitionSharedPaths(paths []string, leftMergeSurface, rightMergeSurface map[string]struct{}, limit int) (onMerge []string, offMerge []string) {
	for _, path := range paths {
		_, leftOK := leftMergeSurface[path]
		_, rightOK := rightMergeSurface[path]
		if leftOK && rightOK {
			onMerge = append(onMerge, path)
			continue
		}
		offMerge = append(offMerge, path)
	}
	slices.Sort(onMerge)
	slices.Sort(offMerge)
	if len(onMerge) > limit {
		onMerge = onMerge[:limit]
	}
	if len(offMerge) > limit {
		offMerge = offMerge[:limit]
	}
	return onMerge, offMerge
}

func DebugTraceMatchesEvents(events []trace.Event, tokens []string, limit int) string {
	if limit <= 0 {
		limit = 8
	}
	var b strings.Builder
	b.WriteString("trace matches:\n")

	matched := 0
	for _, event := range events {
		if !eventMatchesTokens(event, tokens) {
			continue
		}
		b.WriteString("  seq: ")
		b.WriteString(strconv.FormatInt(event.Seq, 10))
		b.WriteString(" kind: ")
		b.WriteString(event.Kind.String())
		b.WriteByte('\n')
		if event.PID != 0 {
			b.WriteString("    pid: ")
			b.WriteString(strconv.FormatInt(event.PID, 10))
			b.WriteByte('\n')
		}
		if event.ParentPID != 0 {
			b.WriteString("    ppid: ")
			b.WriteString(strconv.FormatInt(event.ParentPID, 10))
			b.WriteByte('\n')
		}
		if event.Cwd != "" {
			b.WriteString("    cwd: ")
			b.WriteString(event.Cwd)
			b.WriteByte('\n')
		}
		if event.Path != "" {
			b.WriteString("    path: ")
			b.WriteString(event.Path)
			b.WriteByte('\n')
		}
		if event.RelatedPath != "" {
			b.WriteString("    related: ")
			b.WriteString(event.RelatedPath)
			b.WriteByte('\n')
		}
		if len(event.Argv) > 0 {
			b.WriteString("    argv: ")
			b.WriteString(strings.Join(event.Argv, " "))
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

func eventMatchesTokens(event trace.Event, tokens []string) bool {
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if strings.Contains(event.Path, token) || strings.Contains(event.RelatedPath, token) {
			return true
		}
		for _, arg := range event.Argv {
			if strings.Contains(arg, token) {
				return true
			}
		}
	}
	return false
}
