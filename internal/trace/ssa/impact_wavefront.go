package ssa

import (
	"sort"
	"strings"
)

type wavefrontProbeClass uint8

const (
	wavefrontProbeUnknown wavefrontProbeClass = iota
	wavefrontProbeUnchanged
	wavefrontProbeMutationRoot
	wavefrontProbeFlow
)

type wavefrontStageResult struct {
	matched        int
	baseOnly       []int
	probeOnly      []int
	pairs          []actionPair
	remainingBase  []int
	remainingProbe []int
	probeClass     []wavefrontProbeClass
	divergedDefs   map[PathState]struct{}
	ambiguous      bool
	readAmbiguous  bool
}

// traceSSAImpactPipeline keeps the five stages strictly one-way:
// normalized observation -> Path SSA -> role projection -> wavefront diff -> impact profile.
type traceSSAImpactPipeline struct {
	base       Graph
	probe      Graph
	evidence   *impactEvidence
	baseRoles  roleProjection
	probeRoles roleProjection
	diff       wavefrontStageResult
	profile    optionProfile
	flow       pathSSAFlow
}

func runTraceSSAImpactPipeline(base, probe Graph, evidence *impactEvidence) traceSSAImpactPipeline {
	pipeline := traceSSAImpactPipeline{
		base:     base,
		probe:    probe,
		evidence: evidence,
	}

	// Stage 3: project roles onto SSA nodes and path versions.
	pipeline.baseRoles = projectRoles(base)
	pipeline.probeRoles = projectRoles(probe)

	// Stage 4: wavefront diff over the two role-aware SSA graphs.
	pipeline.diff = wavefrontDiffWithEvidence(
		base,
		probe,
		pipeline.baseRoles,
		pipeline.probeRoles,
		evidence,
	)

	// Stage 5: compress the labeled probe graph into the impact summary.
	pipeline.profile, pipeline.flow = extractWavefrontImpact(
		base,
		pipeline.baseRoles,
		probe,
		pipeline.probeRoles,
		pipeline.diff,
		evidence,
	)
	return pipeline
}

func wavefrontDiff(base, probe Graph, baseRoles, probeRoles roleProjection) wavefrontStageResult {
	return wavefrontDiffWithEvidence(base, probe, baseRoles, probeRoles, nil)
}

func wavefrontDiffWithEvidence(base, probe Graph, baseRoles, probeRoles roleProjection, evidence *impactEvidence) wavefrontStageResult {
	equivalentBaseDefs := collectEquivalentInitialDefs(base, baseRoles)
	equivalentProbeDefs := collectEquivalentInitialDefs(probe, probeRoles)
	readyBase := make(map[int]struct{}, len(base.Actions))
	readyBaseBySig := make(map[string][]int)
	usedBase := make(map[int]struct{}, len(base.Actions))
	unchangedSet := make(map[int]struct{}, len(probe.Actions))
	directMutations := make(map[int]struct{}, len(probe.Actions))
	divergedActions := make(map[int]struct{}, len(probe.Actions))
	divergedDefs := make(map[PathState]struct{})
	pairs := make([]actionPair, 0, len(probe.Actions))
	matched := 0
	readAmbiguous := false

	markEquivalentPair := func(baseIdx, probeIdx int) {
		usedBase[baseIdx] = struct{}{}
		delete(readyBase, baseIdx)
		markEquivalentActionWrites(base, baseRoles, equivalentBaseDefs, []int{baseIdx})
		markEquivalentActionWrites(probe, probeRoles, equivalentProbeDefs, []int{probeIdx})
		unchangedSet[probeIdx] = struct{}{}
		matched++
	}
	markDivergedAction := func(probeIdx int, directMutation bool) {
		if _, ok := divergedActions[probeIdx]; ok {
			return
		}
		divergedActions[probeIdx] = struct{}{}
		if directMutation {
			directMutations[probeIdx] = struct{}{}
		}
		if actionReadAmbiguousVisible(probe, probeRoles, probeIdx) {
			readAmbiguous = true
		}
		if probeIdx < 0 || probeIdx >= len(probe.ActionWrites) {
			return
		}
		for _, def := range probe.ActionWrites[probeIdx] {
			if _, noise := probeRoles.DefNoise[def]; noise {
				continue
			}
			if !impactTrackedPathAllowed(probe, probeRoles, def.Path) {
				continue
			}
			divergedDefs[def] = struct{}{}
		}
	}

	ambiguous := false
	progress := true
	for progress {
		progress = false

		for baseIdx := range base.Actions {
			if _, used := usedBase[baseIdx]; used {
				continue
			}
			if _, ready := readyBase[baseIdx]; ready {
				continue
			}
			if roleActionNoise(baseRoles, baseIdx) {
				continue
			}
			if actionReadAmbiguousVisible(base, baseRoles, baseIdx) {
				continue
			}
			if !actionInputsEquivalent(base, baseRoles, equivalentBaseDefs, baseIdx) {
				continue
			}
			readyBase[baseIdx] = struct{}{}
			sig := intrinsicActionSignature(base, baseRoles, baseIdx)
			readyBaseBySig[sig] = append(readyBaseBySig[sig], baseIdx)
		}

	nextRound:
		for probeIdx := range probe.Actions {
			if roleActionNoise(probeRoles, probeIdx) {
				continue
			}
			if _, ok := unchangedSet[probeIdx]; ok {
				continue
			}
			if _, ok := divergedActions[probeIdx]; ok {
				continue
			}

			readiness := classifyProbeWavefrontReadiness(probe, probeRoles, equivalentProbeDefs, divergedDefs, probeIdx)
			switch readiness {
			case wavefrontProbeReadinessPending:
				continue
			case wavefrontProbeReadinessAmbiguous:
				readAmbiguous = true
				continue
			case wavefrontProbeReadinessFlow:
				markDivergedAction(probeIdx, false)
				progress = true
				break nextRound
			case wavefrontProbeReadinessEquivalent:
				sig := intrinsicActionSignature(probe, probeRoles, probeIdx)
				baseIdx, decision := selectReadyBaselineCandidate(base, probe, baseRoles, probeRoles, readyBaseBySig, readyBase, usedBase, sig, probeIdx, evidence)
				switch decision {
				case readyBaselineMatchAmbiguous:
					ambiguous = true
					continue
				case readyBaselineMatchNone:
					markDivergedAction(probeIdx, true)
					progress = true
					break nextRound
				case readyBaselineMatchUnique:
					markEquivalentPair(baseIdx, probeIdx)
					progress = true
					break nextRound
				}
			}
		}
	}

	for probeIdx := range probe.Actions {
		if roleActionNoise(probeRoles, probeIdx) {
			continue
		}
		if _, ok := unchangedSet[probeIdx]; ok {
			continue
		}
		if _, ok := divergedActions[probeIdx]; ok {
			continue
		}
		if actionReadAmbiguousVisible(probe, probeRoles, probeIdx) {
			readAmbiguous = true
		}
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].probeIdx != pairs[j].probeIdx {
			return pairs[i].probeIdx < pairs[j].probeIdx
		}
		return pairs[i].baseIdx < pairs[j].baseIdx
	})

	pairedBase := make(map[int]struct{}, len(usedBase))
	for baseIdx := range usedBase {
		pairedBase[baseIdx] = struct{}{}
	}
	pairedProbe := make(map[int]struct{}, len(unchangedSet)+len(pairs))
	for probeIdx := range unchangedSet {
		pairedProbe[probeIdx] = struct{}{}
	}
	for _, pair := range pairs {
		pairedProbe[pair.probeIdx] = struct{}{}
	}

	baseOnly := collectUnpairedIndexes(indexRange(len(base.Actions)), pairedBase)
	probeOnly := collectUnpairedIndexes(indexRange(len(probe.Actions)), pairedProbe)
	probeClass, _, _, _, _, _ := finalizeWavefrontProbeClassification(
		probeRoles,
		len(probe.Actions),
		unchangedSet,
		directMutations,
		divergedActions,
	)

	return wavefrontStageResult{
		matched:        matched,
		baseOnly:       baseOnly,
		probeOnly:      probeOnly,
		pairs:          pairs,
		remainingBase:  baseOnly,
		remainingProbe: probeOnly,
		probeClass:     probeClass,
		divergedDefs:   divergedDefs,
		ambiguous:      ambiguous,
		readAmbiguous:  readAmbiguous,
	}
}

type wavefrontProbeReadiness uint8

const (
	wavefrontProbeReadinessPending wavefrontProbeReadiness = iota
	wavefrontProbeReadinessEquivalent
	wavefrontProbeReadinessFlow
	wavefrontProbeReadinessAmbiguous
)

func classifyProbeWavefrontReadiness(graph Graph, roles roleProjection, equivalentDefs, divergedDefs map[PathState]struct{}, idx int) wavefrontProbeReadiness {
	if idx < 0 || idx >= len(graph.ActionReads) {
		return wavefrontProbeReadinessEquivalent
	}
	sawDiverged := false
	for _, read := range graph.ActionReads[idx] {
		if !impactTrackedPathAllowed(graph, roles, read.Path) {
			continue
		}
		defs := visibleBindingDefs(read.Defs, roles)
		if len(defs) == 0 {
			continue
		}
		if len(defs) > 1 {
			return wavefrontProbeReadinessAmbiguous
		}
		def := defs[0]
		if _, ok := equivalentDefs[def]; ok {
			continue
		}
		if _, ok := divergedDefs[def]; ok {
			sawDiverged = true
			continue
		}
		return wavefrontProbeReadinessPending
	}
	if sawDiverged {
		return wavefrontProbeReadinessFlow
	}
	return wavefrontProbeReadinessEquivalent
}

type readyBaselineMatch uint8

const (
	readyBaselineMatchNone readyBaselineMatch = iota
	readyBaselineMatchUnique
	readyBaselineMatchAmbiguous
)

func selectReadyBaselineCandidate(base, probe Graph, baseRoles, probeRoles roleProjection, readyBySig map[string][]int, readyBase, usedBase map[int]struct{}, sig string, probeIdx int, evidence *impactEvidence) (int, readyBaselineMatch) {
	matched := -1
	matchCount := 0
	changed := map[string]bool(nil)
	if evidence != nil {
		changed = evidence.changed
	}
	for _, idx := range readyBySig[sig] {
		if _, ok := readyBase[idx]; !ok {
			continue
		}
		if _, ok := usedBase[idx]; ok {
			continue
		}
		if !wavefrontActionEquivalentWithChanged(base, probe, baseRoles, probeRoles, idx, probeIdx, changed) {
			continue
		}
		matched = idx
		matchCount++
		if matchCount > 1 {
			return -1, readyBaselineMatchAmbiguous
		}
	}
	if matchCount == 1 {
		return matched, readyBaselineMatchUnique
	}
	if relaxed, decision := selectReadyBaselineConfigureFallback(base, probe, baseRoles, probeRoles, readyBase, usedBase, probeIdx, changed); decision != readyBaselineMatchNone {
		return relaxed, decision
	}
	return -1, readyBaselineMatchNone
}

func selectReadyBaselineConfigureFallback(base, probe Graph, baseRoles, probeRoles roleProjection, readyBase, usedBase map[int]struct{}, probeIdx int, changed map[string]bool) (int, readyBaselineMatch) {
	if changed == nil {
		return -1, readyBaselineMatchNone
	}
	if probeIdx < 0 || probeIdx >= len(probe.Actions) {
		return -1, readyBaselineMatchNone
	}
	probeAction := probe.Actions[probeIdx]
	if probeAction.Kind != KindConfigure {
		return -1, readyBaselineMatchNone
	}
	if len(canonicalActionWriteSet(probe, probeRoles, probeIdx)) == 0 {
		return -1, readyBaselineMatchNone
	}
	matched := -1
	matchCount := 0
	probeCwd := normalizeScopeToken(probeAction.Cwd, probe.Scope)
	for idx := range readyBase {
		if _, ok := usedBase[idx]; ok {
			continue
		}
		if idx < 0 || idx >= len(base.Actions) {
			continue
		}
		baseAction := base.Actions[idx]
		if baseAction.Kind != KindConfigure || baseAction.Tool != probeAction.Tool {
			continue
		}
		if normalizeScopeToken(baseAction.Cwd, base.Scope) != probeCwd {
			continue
		}
		if len(canonicalActionWriteSet(base, baseRoles, idx)) == 0 {
			continue
		}
		if !actionOutputsEquivalent(base, probe, baseRoles, probeRoles, idx, probeIdx, changed) {
			continue
		}
		matched = idx
		matchCount++
		if matchCount > 1 {
			return -1, readyBaselineMatchAmbiguous
		}
	}
	if matchCount == 1 {
		return matched, readyBaselineMatchUnique
	}
	return -1, readyBaselineMatchNone
}

func intrinsicActionSignature(graph Graph, roles roleProjection, idx int) string {
	if idx < 0 || idx >= len(graph.Actions) {
		return ""
	}
	action := graph.Actions[idx]
	argv := make([]string, 0, len(action.Argv))
	for _, arg := range action.Argv {
		argv = append(argv, normalizeScopeToken(arg, graph.Scope))
	}
	reads := intrinsicActionPathSet(graph, roles, action.Reads)
	writes := intrinsicActionPathSet(graph, roles, action.Writes)
	parts := []string{behaviorActionSignature(action)}
	parts = append(parts, argv...)
	parts = append(parts, "@", normalizeScopeToken(action.Cwd, graph.Scope), "@")
	parts = append(parts, action.Env...)
	parts = append(parts, "@")
	parts = append(parts, reads...)
	parts = append(parts, "@")
	parts = append(parts, writes...)
	return strings.Join(parts, "\x1f")
}

func intrinsicActionPathSet(graph Graph, roles roleProjection, paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if !impactPathAllowed(graph, roles, path) {
			continue
		}
		set[canonicalImpactPath(graph, path)] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for path := range set {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func indexRange(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func collectAffectedPairs(base Graph, baseRoles roleProjection, probe Graph, probeRoles roleProjection, diff wavefrontStageResult) []actionPair {
	baselineByProbe := collectProbeBaselineMatchesWithRoles(base, baseRoles, probe, probeRoles, diff)
	divergedProbe := wavefrontDivergedProbe(diff)
	seen := make(map[actionPair]struct{}, len(diff.pairs)+len(divergedProbe))
	out := make([]actionPair, 0, len(diff.pairs)+len(divergedProbe))
	add := func(baseIdx, probeIdx int) {
		if baseIdx < 0 || baseIdx >= len(base.Actions) || probeIdx < 0 || probeIdx >= len(probe.Actions) {
			return
		}
		pair := actionPair{baseIdx: baseIdx, probeIdx: probeIdx}
		if _, ok := seen[pair]; ok {
			return
		}
		seen[pair] = struct{}{}
		out = append(out, pair)
	}
	for _, pair := range diff.pairs {
		add(pair.baseIdx, pair.probeIdx)
	}
	for _, probeIdx := range divergedProbe {
		baseIdx, ok := baselineByProbe[probeIdx]
		if !ok {
			continue
		}
		add(baseIdx, probeIdx)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].probeIdx != out[j].probeIdx {
			return out[i].probeIdx < out[j].probeIdx
		}
		return out[i].baseIdx < out[j].baseIdx
	})
	return out
}

func collectEquivalentInitialDefs(graph Graph, roles roleProjection) map[PathState]struct{} {
	out := make(map[PathState]struct{}, len(graph.InitialDefs))
	for _, def := range graph.InitialDefs {
		if _, noise := roles.DefNoise[def]; noise {
			continue
		}
		if !impactTrackedPathAllowed(graph, roles, def.Path) {
			continue
		}
		out[def] = struct{}{}
	}
	for _, reads := range graph.ActionReads {
		for _, read := range reads {
			if !impactTrackedPathAllowed(graph, roles, read.Path) {
				continue
			}
			for _, def := range visibleBindingDefs(read.Defs, roles) {
				if def.Writer >= 0 {
					continue
				}
				out[def] = struct{}{}
			}
		}
	}
	return out
}

func markEquivalentActionWrites(graph Graph, roles roleProjection, equivalentDefs map[PathState]struct{}, indexes []int) {
	for _, idx := range indexes {
		if idx < 0 || idx >= len(graph.ActionWrites) {
			continue
		}
		for _, def := range graph.ActionWrites[idx] {
			if _, noise := roles.DefNoise[def]; noise {
				continue
			}
			equivalentDefs[def] = struct{}{}
		}
	}
}

func actionInputsEquivalent(graph Graph, roles roleProjection, equivalentDefs map[PathState]struct{}, idx int) bool {
	if idx < 0 || idx >= len(graph.ActionReads) {
		return true
	}
	for _, read := range graph.ActionReads[idx] {
		if !impactTrackedPathAllowed(graph, roles, read.Path) {
			continue
		}
		defs := visibleBindingDefs(read.Defs, roles)
		if len(defs) == 0 {
			continue
		}
		if len(defs) != 1 {
			return false
		}
		if _, ok := equivalentDefs[defs[0]]; !ok {
			return false
		}
	}
	return true
}

func collectUnpairedIndexes(indexes []int, paired map[int]struct{}) []int {
	out := make([]int, 0, len(indexes))
	for _, idx := range indexes {
		if _, ok := paired[idx]; ok {
			continue
		}
		out = append(out, idx)
	}
	return out
}

func sortedIndexSet(values map[int]struct{}) []int {
	out := make([]int, 0, len(values))
	for idx := range values {
		out = append(out, idx)
	}
	sort.Ints(out)
	return out
}

func finalizeWavefrontProbeClassification(roles roleProjection, actionCount int, unchangedSet, directMutations, divergedActions map[int]struct{}) ([]wavefrontProbeClass, []int, []int, []int, []int, []int) {
	unchangedProbe := sortedIndexSet(unchangedSet)
	mutationProbe := sortedIndexSet(directMutations)
	divergedProbe := sortedIndexSet(divergedActions)
	probeClass := make([]wavefrontProbeClass, actionCount)
	for _, probeIdx := range unchangedProbe {
		if probeIdx < 0 || probeIdx >= len(probeClass) {
			continue
		}
		probeClass[probeIdx] = wavefrontProbeUnchanged
	}
	for _, probeIdx := range mutationProbe {
		if probeIdx < 0 || probeIdx >= len(probeClass) {
			continue
		}
		probeClass[probeIdx] = wavefrontProbeMutationRoot
	}
	flowProbe := make([]int, 0, len(divergedProbe))
	for _, probeIdx := range divergedProbe {
		if _, ok := directMutations[probeIdx]; ok {
			continue
		}
		if probeIdx < 0 || probeIdx >= len(probeClass) {
			continue
		}
		probeClass[probeIdx] = wavefrontProbeFlow
		flowProbe = append(flowProbe, probeIdx)
	}
	rootProbe := visibleProbeIndexes(roles, mutationProbe)
	return probeClass, unchangedProbe, mutationProbe, flowProbe, divergedProbe, rootProbe
}

func visibleProbeIndexes(roles roleProjection, indexes []int) []int {
	out := make([]int, 0, len(indexes))
	for _, idx := range indexes {
		if idx < 0 {
			continue
		}
		if roleActionNoise(roles, idx) || roleActionDeliveryOnly(roles, idx) {
			continue
		}
		out = append(out, idx)
	}
	return out
}

func extractWavefrontImpact(base Graph, baseRoles roleProjection, probe Graph, roles roleProjection, diff wavefrontStageResult, evidence *impactEvidence) (optionProfile, pathSSAFlow) {
	profile := initOptionProfile()
	if diff.ambiguous || diff.readAmbiguous {
		profile.ambiguous = true
	}

	mutationRoots := wavefrontVisibleMutationRoots(diff, roles)
	seedQueue := make(map[string]struct{})
	for _, idx := range mutationRoots {
		if idx < 0 || idx >= len(probe.Actions) || roleActionDeliveryOnly(roles, idx) {
			continue
		}
		for _, def := range probe.ActionWrites[idx] {
			if !pathChanged(evidence, probe, def.Path) {
				continue
			}
			if addImpactPathWithRoles(profile.seedWrites, probe, roles, def.Path) {
				seedQueue[def.Path] = struct{}{}
			}
			addImpactStateWithRoles(profile.seedStates, probe, roles, def)
			addImpactStateWithRoles(profile.flowStates, probe, roles, def)
		}
	}

	deletedSeeds := addDeletedSeedWritesWithRoles(profile.seedWrites, profile.seedStates, profile.flowStates, seedQueue, base, baseRoles, probe, roles, diff.remainingBase, evidence)
	for path := range profile.seedWrites {
		profile.slicePaths[path] = struct{}{}
	}
	flow := analyzePathSSAFlowV5(probe, roles, seedQueue, deletedSeeds, mutationRoots, evidence)
	if flow.ambiguousReads {
		profile.ambiguous = true
	}
	for def := range flow.reachedDefs {
		addImpactPathWithRoles(profile.slicePaths, probe, roles, def.Path)
		addImpactStateWithRoles(profile.flowStates, probe, roles, def)
	}
	profile.joinSet = append(profile.joinSet, flow.joinActions...)
	for idx, defs := range flow.externalDefs {
		if shouldIgnoreNeedReadsForAction(probe.Actions[idx], roles, idx) {
			continue
		}
		for def := range defs {
			addImpactPathWithRoles(profile.needPaths, probe, roles, def.Path)
			addImpactStateWithRoles(profile.needStates, probe, roles, def)
		}
	}

	return profile, flow
}

func collectProbeBaselineMatchesWithRoles(base Graph, baseRoles roleProjection, probe Graph, probeRoles roleProjection, diff wavefrontStageResult) map[int]int {
	out := make(map[int]int, len(diff.pairs))
	usedBase := make(map[int]struct{}, len(base.Actions))
	for _, pair := range diff.pairs {
		if pair.baseIdx < 0 || pair.baseIdx >= len(base.Actions) || pair.probeIdx < 0 || pair.probeIdx >= len(probe.Actions) {
			continue
		}
		out[pair.probeIdx] = pair.baseIdx
		usedBase[pair.baseIdx] = struct{}{}
	}

	baseBySig := make(map[string][]int)
	for baseIdx := range base.Actions {
		if _, ok := usedBase[baseIdx]; ok {
			continue
		}
		if roleActionNoise(baseRoles, baseIdx) {
			continue
		}
		sig := intrinsicActionSignature(base, baseRoles, baseIdx)
		baseBySig[sig] = append(baseBySig[sig], baseIdx)
	}
	baseWriterByPath := make(map[string]int)
	ambiguousBaseWritePath := make(map[string]struct{})
	for baseIdx := range base.Actions {
		if roleActionNoise(baseRoles, baseIdx) {
			continue
		}
		visibleWrites := canonicalActionWriteSet(base, baseRoles, baseIdx)
		if len(visibleWrites) != 1 {
			continue
		}
		key := visibleWrites[0]
		if prev, ok := baseWriterByPath[key]; ok && prev != baseIdx {
			ambiguousBaseWritePath[key] = struct{}{}
			continue
		}
		baseWriterByPath[key] = baseIdx
	}
	for probeIdx := range probe.Actions {
		if _, ok := out[probeIdx]; ok {
			continue
		}
		if roleActionNoise(probeRoles, probeIdx) {
			continue
		}
		sig := intrinsicActionSignature(probe, probeRoles, probeIdx)
		candidates := baseBySig[sig]
		if len(candidates) == 0 {
			visibleWrites := canonicalActionWriteSet(probe, probeRoles, probeIdx)
			if len(visibleWrites) != 1 {
				continue
			}
			key := visibleWrites[0]
			if _, ambiguous := ambiguousBaseWritePath[key]; ambiguous {
				continue
			}
			baseIdx, ok := baseWriterByPath[key]
			if !ok {
				continue
			}
			if _, ok := usedBase[baseIdx]; ok {
				continue
			}
			out[probeIdx] = baseIdx
			usedBase[baseIdx] = struct{}{}
			continue
		}
		baseIdx := candidates[0]
		baseBySig[sig] = candidates[1:]
		out[probeIdx] = baseIdx
		usedBase[baseIdx] = struct{}{}
	}
	return out
}

func addDeletedSeedWritesWithRoles(seedWrites map[string]struct{}, seedStates map[pathStateKey]struct{}, flowStates map[pathStateKey]struct{}, queue map[string]struct{}, base Graph, baseRoles roleProjection, probe Graph, probeRoles roleProjection, remainingBase []int, evidence *impactEvidence) deletedSeedSet {
	deleted := make(deletedSeedSet)
	probePaths := canonicalImpactWrittenPathSetWithRoles(probe, probeRoles)
	for _, idx := range remainingBase {
		if idx < 0 || idx >= len(base.Actions) {
			continue
		}
		for _, path := range base.Actions[idx].Writes {
			if !impactPathAllowed(base, baseRoles, path) {
				continue
			}
			key := canonicalImpactPath(base, path)
			if _, ok := probePaths[key]; ok {
				continue
			}
			if !pathChanged(evidence, base, path) {
				continue
			}
			seedWrites[key] = struct{}{}
			tombstone := pathStateKey{path: key, tombstone: true}
			seedStates[tombstone] = struct{}{}
			flowStates[tombstone] = struct{}{}
			queue[path] = struct{}{}
			deleted[path] = struct{}{}
		}
	}
	return deleted
}

func canonicalImpactWrittenPathSetWithRoles(graph Graph, roles roleProjection) map[string]struct{} {
	out := make(map[string]struct{})
	for _, action := range graph.Actions {
		for _, path := range action.Writes {
			if !impactPathAllowed(graph, roles, path) {
				continue
			}
			out[canonicalImpactPath(graph, path)] = struct{}{}
		}
	}
	return out
}

func wavefrontProbeIndexes(diff wavefrontStageResult, class wavefrontProbeClass) []int {
	out := make([]int, 0, len(diff.probeClass))
	for idx, current := range diff.probeClass {
		if current != class {
			continue
		}
		out = append(out, idx)
	}
	return out
}

func wavefrontDivergedProbe(diff wavefrontStageResult) []int {
	out := make([]int, 0, len(diff.probeClass))
	for idx, class := range diff.probeClass {
		if class != wavefrontProbeMutationRoot && class != wavefrontProbeFlow {
			continue
		}
		out = append(out, idx)
	}
	return out
}

func wavefrontVisibleMutationRoots(diff wavefrontStageResult, roles roleProjection) []int {
	return visibleProbeIndexes(roles, wavefrontProbeIndexes(diff, wavefrontProbeMutationRoot))
}

func analyzePathSSAFlowV5(graph Graph, roles roleProjection, seeds map[string]struct{}, deletedSeeds deletedSeedSet, mutationRoots []int, evidence *impactEvidence) pathSSAFlow {
	flow := pathSSAFlow{
		reachedDefs:    make(map[PathState]struct{}),
		reachedActions: make(map[int]struct{}),
		externalReads:  make(map[int]map[string]struct{}),
		externalDefs:   make(map[int]map[PathState]struct{}),
	}
	if len(seeds) == 0 && len(mutationRoots) == 0 {
		return flow
	}
	queue := make([]PathState, 0, len(seeds))
	predecessors := make(map[int]map[int]struct{})
	concreteSeedPaths := make(map[string]struct{})

	for _, idx := range mutationRoots {
		if idx < 0 || idx >= len(graph.Actions) || roleActionDeliveryOnly(roles, idx) || roleActionNoise(roles, idx) {
			continue
		}
		rootSeeded := false
		for _, def := range graph.ActionWrites[idx] {
			if _, noise := roles.DefNoise[def]; noise {
				continue
			}
			if _, seeded := seeds[def.Path]; !seeded {
				continue
			}
			if !pathChanged(evidence, graph, def.Path) {
				continue
			}
			rootSeeded = true
			if _, ok := flow.reachedDefs[def]; ok {
				continue
			}
			flow.reachedDefs[def] = struct{}{}
			queue = append(queue, def)
			concreteSeedPaths[def.Path] = struct{}{}
		}
		if rootSeeded {
			flow.reachedActions[idx] = struct{}{}
		}
	}
	for path := range seeds {
		if _, concrete := concreteSeedPaths[path]; concrete {
			continue
		}
		def := PathState{Writer: -1, Path: path}
		if _, ok := deletedSeeds[path]; ok {
			def.Tombstone = true
		}
		if _, ok := flow.reachedDefs[def]; ok {
			continue
		}
		if len(seedReadersWithRoles(graph, roles, def)) == 0 {
			continue
		}
		flow.reachedDefs[def] = struct{}{}
		queue = append(queue, def)
	}
	for len(queue) > 0 {
		def := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, reader := range seedReadersWithRoles(graph, roles, def) {
			if reader < 0 || reader >= len(graph.Actions) || roleActionNoise(roles, reader) || roleActionDeliveryOnly(roles, reader) {
				continue
			}
			if def.Writer >= 0 {
				owners := predecessors[reader]
				if owners == nil {
					owners = make(map[int]struct{})
					predecessors[reader] = owners
				}
				owners[def.Writer] = struct{}{}
			}
			if _, ok := flow.reachedActions[reader]; ok {
				continue
			}
			flow.reachedActions[reader] = struct{}{}
			for _, nextDef := range graph.ActionWrites[reader] {
				if _, noise := roles.DefNoise[nextDef]; noise {
					continue
				}
				if !pathChanged(evidence, graph, nextDef.Path) {
					continue
				}
				if _, ok := flow.reachedDefs[nextDef]; ok {
					continue
				}
				flow.reachedDefs[nextDef] = struct{}{}
				queue = append(queue, nextDef)
			}
		}
	}

	join := make(map[int]bool)
	hasJoinAncestor := make(map[int]bool)
	reachedOrder := make([]int, 0, len(flow.reachedActions))
	for reader := range flow.reachedActions {
		reachedOrder = append(reachedOrder, reader)
	}
	sort.Ints(reachedOrder)
	flow.flowActions = reachedOrder
	for _, reader := range reachedOrder {
		external := make(map[string]struct{})
		sawReachedInput := false
		sawStablePrereq := false
		for _, binding := range graph.ActionReads[reader] {
			defs := visibleBindingDefs(binding.Defs, roles)
			if len(defs) == 0 {
				continue
			}
			if len(defs) > 1 {
				flow.ambiguousReads = true
			}
			hasInternal := false
			for _, def := range defs {
				if _, ok := flow.reachedDefs[def]; ok {
					hasInternal = true
					sawReachedInput = true
				}
				if def.Writer < 0 {
					tombstone := def
					tombstone.Tombstone = true
					if _, ok := flow.reachedDefs[tombstone]; ok {
						hasInternal = true
						sawReachedInput = true
					}
				}
			}
			if hasInternal && len(defs) == 1 {
				continue
			}
			if !impactTrackedPathAllowed(graph, roles, binding.Path) {
				continue
			}
			key := canonicalImpactPath(graph, binding.Path)
			for _, def := range defs {
				if _, ok := flow.reachedDefs[def]; ok {
					continue
				}
				if def.Writer < 0 {
					tombstone := def
					tombstone.Tombstone = true
					if _, ok := flow.reachedDefs[tombstone]; ok {
						continue
					}
				}
				sawStablePrereq = true
				external[key] = struct{}{}
				defsSet := flow.externalDefs[reader]
				if defsSet == nil {
					defsSet = make(map[PathState]struct{})
					flow.externalDefs[reader] = defsSet
				}
				defsSet[def] = struct{}{}
			}
		}
		if len(external) != 0 {
			flow.externalReads[reader] = external
		}
		if sawReachedInput && sawStablePrereq {
			join[reader] = true
		}
		for pred := range predecessors[reader] {
			if join[pred] || hasJoinAncestor[pred] {
				hasJoinAncestor[reader] = true
				break
			}
		}
		if join[reader] {
			flow.joinActions = append(flow.joinActions, reader)
		}
		if join[reader] && !hasJoinAncestor[reader] {
			flow.frontierActions = append(flow.frontierActions, reader)
		}
	}
	return flow
}

func shouldIgnoreNeedReadsForAction(action ExecNode, roles roleProjection, idx int) bool {
	if !roleActionDeliveryOnly(roles, idx) {
		return false
	}
	return action.Kind == KindCopy || action.Kind == KindInstall
}

func seedReadersWithRoles(graph Graph, roles roleProjection, def PathState) []int {
	if _, noise := roles.DefNoise[def]; noise {
		return nil
	}
	readers := readersForDef(graph, def)
	if len(readers) != 0 || !def.Tombstone {
		return readers
	}
	baseline := PathState{Writer: def.Writer, Path: def.Path, Version: def.Version}
	baselineReaders := readersForDef(graph, baseline)
	filtered := make([]int, 0, len(baselineReaders))
	for _, reader := range baselineReaders {
		if def.Writer >= 0 && reader <= def.Writer {
			continue
		}
		filtered = append(filtered, reader)
	}
	return filtered
}

func readersForDef(graph Graph, def PathState) []int {
	if len(graph.ReadersByDef) != 0 {
		if readers := graph.ReadersByDef[def]; len(readers) != 0 {
			return readers
		}
	}
	readers := make([]int, 0)
	for reader, bindings := range graph.ActionReads {
		found := false
		for _, binding := range bindings {
			for _, candidate := range binding.Defs {
				if candidate != def {
					continue
				}
				found = true
				break
			}
			if found {
				readers = append(readers, reader)
				break
			}
		}
	}
	return readers
}

func addImpactPathWithRoles(set map[string]struct{}, graph Graph, roles roleProjection, path string) bool {
	if !impactPathAllowed(graph, roles, path) {
		return false
	}
	key := canonicalImpactPath(graph, path)
	if _, ok := set[key]; ok {
		return false
	}
	set[key] = struct{}{}
	return true
}

func addImpactStateWithRoles(set map[pathStateKey]struct{}, graph Graph, roles roleProjection, def PathState) {
	if !impactTrackedPathAllowed(graph, roles, def.Path) {
		return
	}
	set[pathStateKey{
		path:      canonicalImpactPath(graph, def.Path),
		tombstone: def.Tombstone,
		missing:   def.Missing,
	}] = struct{}{}
}

func wavefrontActionEquivalent(base, probe Graph, baseRoles, probeRoles roleProjection, baseIdx, probeIdx int, evidence *impactEvidence) bool {
	changed := map[string]bool(nil)
	if evidence != nil {
		changed = evidence.changed
	}
	return wavefrontActionEquivalentWithChanged(
		base,
		probe,
		baseRoles,
		probeRoles,
		baseIdx,
		probeIdx,
		changed,
	)
}

func behaviorActionSignature(action ExecNode) string {
	return action.ActionKey + "\x1f" + action.StructureKey
}
