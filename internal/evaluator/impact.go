package evaluator

import "sort"

type actionPair struct {
	baseIdx  int
	probeIdx int
}

type impactAnalysis struct {
	profile         optionProfile
	matched         int
	baseOnly        []int
	probeOnly       []int
	affectedPairs   []actionPair
	rootProbe       []int
	flowProbe       []int
	frontierProbe   []int
	remainingBase   []int
	remainingProbe  []int
	ambiguousGroups bool
}

func analyzeImpact(base, probe actionGraph) impactAnalysis {
	return analyzeImpactWithEvidence(base, probe, nil)
}

type impactEvidence struct {
	changed map[string]bool
}

type pathSSADef struct {
	writer    int
	path      string
	tombstone bool
}

type pathSSARead struct {
	path      string
	defs      []pathSSADef
	ambiguous bool
}

type pathSSA struct {
	actionReads  [][]pathSSARead
	actionWrites [][]pathSSADef
	readersByDef map[pathSSADef][]int
}

type pathSSAFlow struct {
	reachedDefs     map[pathSSADef]struct{}
	reachedActions  map[int]struct{}
	flowActions     []int
	frontierActions []int
	externalReads   map[int]map[string]struct{}
	externalDefs    map[int]map[pathSSADef]struct{}
	ambiguousReads  bool
}

type deletedSeedSet map[string]struct{}

func analyzeImpactWithEvidence(base, probe actionGraph, evidence *impactEvidence) impactAnalysis {
	matched, baseOnly, probeOnly := matchActionFingerprints(base, probe)
	pairs, remainingBase, remainingProbe, ambiguous := matchStructurePairs(base, probe, baseOnly, probeOnly)
	candidateProbe := collectCandidateProbeIndexes(remainingProbe, pairs)
	rootProbe, rootAmbiguous := classifyMutationRoots(probe, candidateProbe)
	ssa := buildPathSSA(probe)

	profile := initOptionProfile()
	if ambiguous || rootAmbiguous {
		profile.ambiguous = true
	}

	seedQueue := make(map[string]struct{})
	candidateWrites := collectProbeCandidateWrites(probe, candidateProbe)
	pairedProbeReads := collectProbeReadBaselines(base, probe, pairs)
	for _, idx := range rootProbe {
		if isDeliveryOnlyAction(probe, idx) {
			continue
		}
		for _, def := range ssa.actionWrites[idx] {
			if !pathChanged(evidence, probe, def.path) {
				continue
			}
			if addImpactPath(profile.seedWrites, probe, def.path) {
				seedQueue[def.path] = struct{}{}
			}
			addImpactState(profile.seedStates, probe, def)
			addImpactState(profile.flowStates, probe, def)
		}
		for _, binding := range ssa.actionReads[idx] {
			if _, internal := candidateWrites[binding.path]; internal {
				continue
			}
			if !impactTrackedPathAllowed(probe, binding.path) {
				continue
			}
			if binding.ambiguous {
				profile.ambiguous = true
			}
			addImpactPath(profile.needPaths, probe, binding.path)
			for _, def := range binding.defs {
				addImpactState(profile.needStates, probe, def)
			}
		}
	}

	deletedSeeds := addDeletedSeedWrites(profile.seedWrites, profile.seedStates, profile.flowStates, seedQueue, base, probe, remainingBase, evidence)
	for path := range profile.seedWrites {
		profile.slicePaths[path] = struct{}{}
	}
	flow := analyzePathSSAFlow(probe, ssa, seedQueue, deletedSeeds, rootProbe, pairedProbeReads, evidence)
	if flow.ambiguousReads {
		profile.ambiguous = true
	}
	for def := range flow.reachedDefs {
		addImpactPath(profile.slicePaths, probe, def.path)
		addImpactState(profile.flowStates, probe, def)
	}
	for _, idx := range flow.frontierActions {
		for path := range flow.externalReads[idx] {
			profile.needPaths[path] = struct{}{}
		}
		for def := range flow.externalDefs[idx] {
			addImpactState(profile.needStates, probe, def)
		}
	}

	return impactAnalysis{
		profile:         profile,
		matched:         matched,
		baseOnly:        baseOnly,
		probeOnly:       probeOnly,
		affectedPairs:   pairs,
		rootProbe:       rootProbe,
		flowProbe:       flow.flowActions,
		frontierProbe:   flow.frontierActions,
		remainingBase:   remainingBase,
		remainingProbe:  remainingProbe,
		ambiguousGroups: ambiguous || rootAmbiguous,
	}
}

func collectCandidateProbeIndexes(remainingProbe []int, pairs []actionPair) []int {
	out := make([]int, 0, len(remainingProbe)+len(pairs))
	seen := make(map[int]struct{}, len(remainingProbe)+len(pairs))
	for _, idx := range remainingProbe {
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		out = append(out, idx)
	}
	for _, pair := range pairs {
		if _, ok := seen[pair.probeIdx]; ok {
			continue
		}
		seen[pair.probeIdx] = struct{}{}
		out = append(out, pair.probeIdx)
	}
	return out
}

func initOptionProfile() optionProfile {
	return optionProfile{
		seedWrites: make(map[string]struct{}),
		needPaths:  make(map[string]struct{}),
		slicePaths: make(map[string]struct{}),
		seedStates: make(map[pathStateKey]struct{}),
		needStates: make(map[pathStateKey]struct{}),
		flowStates: make(map[pathStateKey]struct{}),
	}
}

func matchStructurePairs(base, probe actionGraph, baseOnly, probeOnly []int) ([]actionPair, []int, []int, bool) {
	baseGroups := make(map[string][]int)
	for _, idx := range baseOnly {
		if idx < 0 || idx >= len(base.actions) {
			continue
		}
		key := base.actions[idx].structureKey
		if key == "" {
			continue
		}
		baseGroups[key] = append(baseGroups[key], idx)
	}
	probeGroups := make(map[string][]int)
	for _, idx := range probeOnly {
		if idx < 0 || idx >= len(probe.actions) {
			continue
		}
		key := probe.actions[idx].structureKey
		if key == "" {
			continue
		}
		probeGroups[key] = append(probeGroups[key], idx)
	}

	pairedBase := make(map[int]struct{})
	pairedProbe := make(map[int]struct{})
	pairs := make([]actionPair, 0)
	ambiguous := false
	for key, left := range baseGroups {
		right := probeGroups[key]
		switch {
		case len(left) == 1 && len(right) == 1:
			pairs = append(pairs, actionPair{baseIdx: left[0], probeIdx: right[0]})
			pairedBase[left[0]] = struct{}{}
			pairedProbe[right[0]] = struct{}{}
		case len(left) != 0 && len(right) != 0:
			ambiguous = true
		}
	}

	remainingBase := make([]int, 0, len(baseOnly))
	for _, idx := range baseOnly {
		if _, ok := pairedBase[idx]; ok {
			continue
		}
		remainingBase = append(remainingBase, idx)
	}
	remainingProbe := make([]int, 0, len(probeOnly))
	for _, idx := range probeOnly {
		if _, ok := pairedProbe[idx]; ok {
			continue
		}
		remainingProbe = append(remainingProbe, idx)
	}
	return pairs, remainingBase, remainingProbe, ambiguous
}

func classifyMutationRoots(graph actionGraph, candidates []int) ([]int, bool) {
	if len(candidates) == 0 {
		return nil, false
	}
	writers := make(map[string]map[int]struct{})
	for _, idx := range candidates {
		if idx < 0 || idx >= len(graph.actions) {
			continue
		}
		for _, path := range graph.actions[idx].writes {
			if !impactPathAllowed(graph, path) {
				continue
			}
			owners := writers[path]
			if owners == nil {
				owners = make(map[int]struct{})
				writers[path] = owners
			}
			owners[idx] = struct{}{}
		}
	}

	roots := make([]int, 0, len(candidates))
	for _, idx := range candidates {
		if idx < 0 || idx >= len(graph.actions) {
			continue
		}
		dependsOnCandidate := false
		for _, path := range graph.actions[idx].reads {
			owners := writers[path]
			if len(owners) == 0 {
				continue
			}
			for owner := range owners {
				if owner == idx {
					continue
				}
				dependsOnCandidate = true
				break
			}
			if dependsOnCandidate {
				break
			}
		}
		if !dependsOnCandidate {
			roots = append(roots, idx)
		}
	}
	if len(roots) != 0 {
		return roots, false
	}
	return candidates, true
}

func collectProbeCandidateWrites(graph actionGraph, candidates []int) map[string]struct{} {
	out := make(map[string]struct{})
	for _, idx := range candidates {
		if idx < 0 || idx >= len(graph.actions) {
			continue
		}
		for _, path := range graph.actions[idx].writes {
			if !impactPathAllowed(graph, path) {
				continue
			}
			out[path] = struct{}{}
		}
	}
	return out
}

func collectProbeReadBaselines(base, probe actionGraph, pairs []actionPair) map[int]map[string]struct{} {
	out := make(map[int]map[string]struct{}, len(pairs))
	for _, pair := range pairs {
		if pair.baseIdx < 0 || pair.baseIdx >= len(base.actions) || pair.probeIdx < 0 || pair.probeIdx >= len(probe.actions) {
			continue
		}
		out[pair.probeIdx] = canonicalReadSet(base, pair.baseIdx)
	}

	baseWriterByPath := make(map[string]int)
	ambiguousBaseWritePath := make(map[string]struct{})
	for idx, action := range base.actions {
		for _, path := range action.writes {
			if !impactPathAllowed(base, path) {
				continue
			}
			key := canonicalImpactPath(base, path)
			if prev, ok := baseWriterByPath[key]; ok && prev != idx {
				ambiguousBaseWritePath[key] = struct{}{}
				continue
			}
			baseWriterByPath[key] = idx
		}
	}
	for probeIdx, action := range probe.actions {
		if _, ok := out[probeIdx]; ok {
			continue
		}
		if len(action.writes) != 1 {
			continue
		}
		key := canonicalImpactPath(probe, action.writes[0])
		if _, ambiguous := ambiguousBaseWritePath[key]; ambiguous {
			continue
		}
		baseIdx, ok := baseWriterByPath[key]
		if !ok {
			continue
		}
		out[probeIdx] = canonicalReadSet(base, baseIdx)
	}
	return out
}

func canonicalReadSet(graph actionGraph, idx int) map[string]struct{} {
	if idx < 0 || idx >= len(graph.actions) {
		return nil
	}
	reads := make(map[string]struct{}, len(graph.actions[idx].reads))
	for _, path := range graph.actions[idx].reads {
		if !impactPathAllowed(graph, path) {
			continue
		}
		reads[canonicalImpactPath(graph, path)] = struct{}{}
	}
	return reads
}

func addDeletedSeedWrites(seedWrites map[string]struct{}, seedStates map[pathStateKey]struct{}, flowStates map[pathStateKey]struct{}, queue map[string]struct{}, base, probe actionGraph, remainingBase []int, evidence *impactEvidence) deletedSeedSet {
	deleted := make(deletedSeedSet)
	probePaths := canonicalImpactWrittenPathSet(probe)
	for _, idx := range remainingBase {
		if idx < 0 || idx >= len(base.actions) {
			continue
		}
		for _, path := range base.actions[idx].writes {
			if !impactPathAllowed(base, path) {
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

func buildPathSSA(graph actionGraph) pathSSA {
	ssa := pathSSA{
		actionReads:  make([][]pathSSARead, len(graph.actions)),
		actionWrites: make([][]pathSSADef, len(graph.actions)),
		readersByDef: make(map[pathSSADef][]int),
	}
	order := newCausalOrder(graph)
	for i, action := range graph.actions {
		for _, read := range action.reads {
			if !rawSSAPathAllowed(graph, read) {
				continue
			}
			defs := reachingDefsForRead(graph, &order, read, i)
			binding := pathSSARead{path: read, defs: defs, ambiguous: len(defs) > 1}
			ssa.actionReads[i] = append(ssa.actionReads[i], binding)
			for _, def := range defs {
				ssa.readersByDef[def] = append(ssa.readersByDef[def], i)
			}
		}
		for _, write := range action.writes {
			if !rawSSAPathAllowed(graph, write) {
				continue
			}
			def := writerPathDef(graph, i, write)
			ssa.actionWrites[i] = append(ssa.actionWrites[i], def)
		}
	}
	return ssa
}

type causalOrder struct {
	graph     actionGraph
	descCache map[int]map[int]struct{}
}

func newCausalOrder(graph actionGraph) causalOrder {
	return causalOrder{
		graph:     graph,
		descCache: make(map[int]map[int]struct{}),
	}
}

func (order *causalOrder) causallyBefore(left, right int) bool {
	if left < 0 || right < 0 || left >= right {
		return false
	}
	leftAction := order.graph.actions[left]
	rightAction := order.graph.actions[right]
	if leftAction.pid == 0 || rightAction.pid == 0 {
		return false
	}
	if leftAction.pid == rightAction.pid {
		return true
	}
	for parent := order.graph.parentAction[right]; parent >= 0; parent = order.graph.parentAction[parent] {
		if parent == left {
			return true
		}
	}
	if _, ok := order.descendants(left)[right]; ok {
		return true
	}
	return false
}

func (order *causalOrder) descendants(idx int) map[int]struct{} {
	if out, ok := order.descCache[idx]; ok {
		return out
	}
	seen := make(map[int]struct{})
	stack := []int{idx}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, edge := range order.graph.out[cur] {
			if _, ok := seen[edge.to]; ok {
				continue
			}
			seen[edge.to] = struct{}{}
			stack = append(stack, edge.to)
		}
	}
	order.descCache[idx] = seen
	return seen
}

func reachingDefsForRead(graph actionGraph, order *causalOrder, path string, reader int) []pathSSADef {
	facts, ok := graph.rawPaths[path]
	if !ok {
		return []pathSSADef{{writer: -1, path: path}}
	}
	candidates := make([]int, 0, len(facts.writers))
	for _, writer := range facts.writers {
		if writer >= reader {
			break
		}
		candidates = append(candidates, writer)
	}
	if len(candidates) == 0 {
		return []pathSSADef{{writer: -1, path: path}}
	}
	if readUsesLinearLatest(graph, candidates, reader) {
		return []pathSSADef{writerPathDef(graph, candidates[len(candidates)-1], path)}
	}
	defs := make([]pathSSADef, 0, len(candidates))
	for _, writer := range candidates {
		superseded := false
		for _, other := range candidates {
			if writer == other {
				continue
			}
			if order.causallyBefore(writer, other) {
				superseded = true
				break
			}
		}
		if superseded {
			continue
		}
		defs = append(defs, writerPathDef(graph, writer, path))
	}
	if len(defs) == 0 {
		return []pathSSADef{{writer: -1, path: path}}
	}
	return defs
}

func readUsesLinearLatest(graph actionGraph, candidates []int, reader int) bool {
	if reader < 0 || reader >= len(graph.actions) {
		return true
	}
	if graph.actions[reader].pid == 0 {
		return true
	}
	for _, writer := range candidates {
		if graph.actions[writer].pid == 0 {
			return true
		}
	}
	return false
}

func analyzePathSSAFlow(graph actionGraph, ssa pathSSA, seeds map[string]struct{}, deletedSeeds deletedSeedSet, rootProbe []int, pairedProbeReads map[int]map[string]struct{}, evidence *impactEvidence) pathSSAFlow {
	flow := pathSSAFlow{
		reachedDefs:    make(map[pathSSADef]struct{}),
		reachedActions: make(map[int]struct{}),
		externalReads:  make(map[int]map[string]struct{}),
		externalDefs:   make(map[int]map[pathSSADef]struct{}),
	}
	if len(seeds) == 0 && len(rootProbe) == 0 {
		return flow
	}
	queue := make([]pathSSADef, 0, len(seeds))
	predecessors := make(map[int]map[int]struct{})

	for _, idx := range rootProbe {
		if idx < 0 || idx >= len(graph.actions) || isDeliveryOnlyAction(graph, idx) {
			continue
		}
		rootSeeded := false
		for _, def := range ssa.actionWrites[idx] {
			if _, seeded := seeds[def.path]; !seeded {
				continue
			}
			if !pathChanged(evidence, graph, def.path) {
				continue
			}
			rootSeeded = true
			if _, ok := flow.reachedDefs[def]; ok {
				continue
			}
			flow.reachedDefs[def] = struct{}{}
			queue = append(queue, def)
		}
		if rootSeeded {
			flow.reachedActions[idx] = struct{}{}
		}
	}
	for path := range seeds {
		def := pathSSADef{writer: -1, path: path}
		if _, ok := deletedSeeds[path]; ok {
			def.tombstone = true
		}
		if _, ok := flow.reachedDefs[def]; ok {
			continue
		}
		if len(seedReaders(ssa, def)) == 0 {
			continue
		}
		flow.reachedDefs[def] = struct{}{}
		queue = append(queue, def)
	}
	for len(queue) > 0 {
		def := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, reader := range seedReaders(ssa, def) {
			if reader < 0 || reader >= len(graph.actions) || graph.tooling[reader] || isDeliveryOnlyAction(graph, reader) {
				continue
			}
			if def.writer >= 0 {
				owners := predecessors[reader]
				if owners == nil {
					owners = make(map[int]struct{})
					predecessors[reader] = owners
				}
				owners[def.writer] = struct{}{}
			}
			if _, ok := flow.reachedActions[reader]; ok {
				continue
			}
			flow.reachedActions[reader] = struct{}{}
			for _, nextDef := range ssa.actionWrites[reader] {
				if !pathChanged(evidence, graph, nextDef.path) {
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

	mixed := make(map[int]bool)
	hasMixedAncestor := make(map[int]bool)
	reachedOrder := make([]int, 0, len(flow.reachedActions))
	for reader := range flow.reachedActions {
		reachedOrder = append(reachedOrder, reader)
	}
	sort.Ints(reachedOrder)
	flow.flowActions = reachedOrder
	for _, reader := range reachedOrder {
		baseReads := pairedProbeReads[reader]
		external := make(map[string]struct{})
		sawReachedInput := false
		for _, binding := range ssa.actionReads[reader] {
			if binding.ambiguous {
				flow.ambiguousReads = true
			}
			hasInternal := false
			for _, def := range binding.defs {
				if _, ok := flow.reachedDefs[def]; ok {
					hasInternal = true
					sawReachedInput = true
				}
			}
			if hasInternal && len(binding.defs) == 1 {
				continue
			}
			if !impactTrackedPathAllowed(graph, binding.path) {
				continue
			}
			key := canonicalImpactPath(graph, binding.path)
			for _, def := range binding.defs {
				if _, ok := flow.reachedDefs[def]; ok {
					continue
				}
				if len(baseReads) != 0 {
					if _, ok := baseReads[key]; ok {
						continue
					}
				}
				external[key] = struct{}{}
				defs := flow.externalDefs[reader]
				if defs == nil {
					defs = make(map[pathSSADef]struct{})
					flow.externalDefs[reader] = defs
				}
				defs[def] = struct{}{}
			}
		}
		if len(external) != 0 {
			flow.externalReads[reader] = external
		}
		if sawReachedInput && len(external) != 0 {
			mixed[reader] = true
		}
		for pred := range predecessors[reader] {
			if mixed[pred] || hasMixedAncestor[pred] {
				hasMixedAncestor[reader] = true
				break
			}
		}
		if mixed[reader] && !hasMixedAncestor[reader] {
			flow.frontierActions = append(flow.frontierActions, reader)
		}
	}
	return flow
}

func seedReaders(ssa pathSSA, def pathSSADef) []int {
	readers := ssa.readersByDef[def]
	if len(readers) != 0 || !def.tombstone {
		return readers
	}
	baseline := pathSSADef{writer: def.writer, path: def.path}
	filtered := make([]int, 0, len(ssa.readersByDef[baseline]))
	for _, reader := range ssa.readersByDef[baseline] {
		if def.writer >= 0 && reader <= def.writer {
			continue
		}
		filtered = append(filtered, reader)
	}
	return filtered
}

func addImpactPath(set map[string]struct{}, graph actionGraph, path string) bool {
	if !impactPathAllowed(graph, path) {
		return false
	}
	key := canonicalImpactPath(graph, path)
	if _, ok := set[key]; ok {
		return false
	}
	set[key] = struct{}{}
	return true
}

func impactPathAllowed(graph actionGraph, path string) bool {
	facts, ok := graph.paths[path]
	if !ok {
		return false
	}
	if isProbeOnlyNoisePath(graph, path) {
		return false
	}
	if facts.role == roleTooling {
		return false
	}
	if facts.role == roleDelivery && !isExplicitDeliveryPath(path, graph.scope) {
		return false
	}
	return true
}

func rawSSAPathAllowed(graph actionGraph, path string) bool {
	_, ok := graph.rawPaths[path]
	return ok
}

func writerPathDef(graph actionGraph, writer int, path string) pathSSADef {
	def := pathSSADef{writer: writer, path: path}
	if writer < 0 || writer >= len(graph.actions) {
		return def
	}
	for _, deleted := range graph.actions[writer].deletes {
		if deleted == path {
			def.tombstone = true
			break
		}
	}
	return def
}

func addImpactState(set map[pathStateKey]struct{}, graph actionGraph, def pathSSADef) {
	if !impactTrackedPathAllowed(graph, def.path) {
		return
	}
	set[pathStateKey{
		path:      canonicalImpactPath(graph, def.path),
		tombstone: def.tombstone,
	}] = struct{}{}
}

func impactTrackedPathAllowed(graph actionGraph, path string) bool {
	if !impactPathAllowed(graph, path) {
		return false
	}
	if graph.scope.SourceRoot == "" && graph.scope.BuildRoot == "" && graph.scope.InstallRoot == "" && len(graph.scope.KeepRoots) == 0 {
		return true
	}
	return pathWithinObservedScope(path, graph.scope)
}

func canonicalImpactPath(graph actionGraph, path string) string {
	return normalizeScopeToken(path, graph.scope)
}

func canonicalImpactPathSet(graph actionGraph) map[string]struct{} {
	out := make(map[string]struct{}, len(graph.paths))
	for path := range graph.paths {
		if !impactPathAllowed(graph, path) {
			continue
		}
		out[canonicalImpactPath(graph, path)] = struct{}{}
	}
	return out
}

func canonicalImpactWrittenPathSet(graph actionGraph) map[string]struct{} {
	out := make(map[string]struct{})
	for _, action := range graph.actions {
		for _, path := range action.writes {
			if !impactPathAllowed(graph, path) {
				continue
			}
			out[canonicalImpactPath(graph, path)] = struct{}{}
		}
	}
	return out
}

func pathChanged(evidence *impactEvidence, graph actionGraph, path string) bool {
	if evidence == nil {
		return true
	}
	key := canonicalImpactPath(graph, path)
	changed, ok := evidence.changed[key]
	if !ok {
		return true
	}
	return changed
}
