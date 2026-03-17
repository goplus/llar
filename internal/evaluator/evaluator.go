package evaluator

import (
	"context"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/trace"
)

type ProbeResult struct {
	Records          []trace.Record
	Scope            trace.Scope
	TraceDiagnostics trace.ParseDiagnostics
	InputDigests     map[string]string
	OutputManifest   OutputManifest
}

type ProbeFunc func(context.Context, string) (ProbeResult, error)

func buildGraphForProbe(probe ProbeResult) actionGraph {
	return buildGraphWithScopeAndDigests(probe.Records, probe.Scope, probe.InputDigests)
}

type normalizedRecord struct {
	pid         int64
	parentPID   int64
	argv        []string
	cwd         string
	inputs      []string
	changes     []string
	inputOrigin map[string]string
	fingerprint string
}

type optionProfile struct {
	propagatingReads  map[string]struct{}
	propagatingWrites map[string]struct{}
	unknownReads      map[string]struct{}
	unknownWrites     map[string]struct{}
	deliveryWrites    map[string]struct{}
	toolingReads      map[string]struct{}
	toolingWrites     map[string]struct{}
	paramTouches      map[string]struct{}
}

type optionVariant struct {
	profile    optionProfile
	outputDiff outputManifestDiff
}

var (
	reTmpUnix              = regexp.MustCompile(`^/tmp/[^/]+`)
	reTmpMac               = regexp.MustCompile(`^/var/folders/[^/]+/[^/]+/[^/]+`)
	reBuildTryCompileNoise = regexp.MustCompile(`(^|/)TryCompile-[0-9A-Fa-f]+(/|$)`)
	reBuildCmTCNoise       = regexp.MustCompile(`(^|/)cmTC_[0-9A-Fa-f]+(/|$)`)
	reBuildTmpPIDNoise     = regexp.MustCompile(`\.tmp\.[0-9]+($|/)`)
)

func Watch(ctx context.Context, matrix formula.Matrix, probe ProbeFunc) ([]string, bool, error) {
	requireCombos := expandRequireCombos(matrix.Require)
	if len(requireCombos) == 0 {
		requireCombos = []string{""}
	}

	defaults := defaultOptions(matrix)
	optionKeys := slices.Sorted(maps.Keys(matrix.Options))
	execute := make(map[string]struct{})
	trusted := true

	for _, requireCombo := range requireCombos {
		baselineCombo := composeCombo(requireCombo, defaults, optionKeys)
		baseResult, err := probe(ctx, baselineCombo)
		if err != nil {
			return nil, false, err
		}
		baseGraph := buildGraphForProbe(baseResult)
		trusted = trusted && baseResult.TraceDiagnostics.Trusted() && isTrustedGraph(baseGraph)
		if len(optionKeys) == 0 {
			execute[baselineCombo] = struct{}{}
			continue
		}

		profiles := make(map[string][]optionVariant, len(optionKeys))
		for _, key := range optionKeys {
			values := slices.Clone(matrix.Options[key])
			for _, value := range values {
				if value == defaults[key] {
					continue
				}
				override := maps.Clone(defaults)
				override[key] = value
				combo := composeCombo(requireCombo, override, optionKeys)
				result, err := probe(ctx, combo)
				if err != nil {
					return nil, false, err
				}
				probeGraph := buildGraphForProbe(result)
				trusted = trusted && result.TraceDiagnostics.Trusted() && isTrustedGraph(probeGraph)
				profiles[key] = append(profiles[key], optionVariant{
					profile:    diffProfile(baseGraph, probeGraph),
					outputDiff: diffOutputManifest(baseResult.OutputManifest, result.OutputManifest),
				})
			}
		}

		zeroDiff := zeroDiffOptionKeys(profiles)
		for _, combo := range componentCombos(
			requireCombo,
			matrix.Options,
			defaults,
			optionKeys,
			collisionComponents(optionKeys, profiles, zeroDiff),
			orthogonalOptionKeys(optionKeys, zeroDiff),
		) {
			execute[combo] = struct{}{}
		}
	}

	return slices.Sorted(maps.Keys(execute)), trusted, nil
}

func isTrustedGraph(graph actionGraph) bool {
	for idx, action := range graph.actions {
		if !graph.business[idx] {
			continue
		}
		if (action.kind == kindGeneric || action.kind == kindCodegen) && !isDeliveryOnlyAction(graph, idx) {
			return false
		}
	}
	for _, facts := range graph.paths {
		if facts.role != roleUnknown {
			continue
		}
		if pathTouchesBusiness(graph.business, facts) {
			return false
		}
	}
	return true
}

func pathTouchesBusiness(business []bool, facts pathFacts) bool {
	for _, writer := range facts.writers {
		if business[writer] {
			return true
		}
	}
	for _, reader := range facts.readers {
		if business[reader] {
			return true
		}
	}
	return false
}

type diffState struct {
	base      actionGraph
	probe     actionGraph
	profile   optionProfile
	baseOnly  []int
	probeOnly []int
	refined   refinedDiffResult
	visited   []bool
	seedPaths []string
	pathStack []string
}

func diffProfile(base, probe actionGraph) optionProfile {
	state := diffState{
		base:    base,
		probe:   probe,
		profile: initOptionProfile(),
	}
	diffMatchActions(&state)
	diffRefineSeeds(&state)
	diffBuildProfile(&state)
	diffPropagate(&state)
	return state.profile
}

func diffMatchActions(state *diffState) {
	_, state.baseOnly, state.probeOnly = matchActionFingerprints(state.base, state.probe)
	addParamTouches(&state.profile, state.base, state.probe, state.baseOnly, state.probeOnly)
}

func diffRefineSeeds(state *diffState) {
	state.refined = refineDiffActions(state.base, state.probe, state.baseOnly, state.probeOnly)
}

func diffBuildProfile(state *diffState) {
	diffAddBaseOnlyProfile(state)
	diffInitPropagation(state)
	diffAddRefinedPairs(state)
	diffSeedUnmatchedProbeActions(state)
}

func diffPropagate(state *diffState) {
	state.pathStack = append(state.pathStack[:0], state.seedPaths...)
	visitedPaths := make(map[string]struct{}, len(state.pathStack))
	for len(state.pathStack) > 0 {
		path := state.pathStack[len(state.pathStack)-1]
		state.pathStack = state.pathStack[:len(state.pathStack)-1]
		if _, ok := visitedPaths[path]; ok {
			continue
		}
		visitedPaths[path] = struct{}{}
		facts, ok := state.probe.paths[path]
		if !ok || !pathSeedsDiffPropagation(state.probe, facts) {
			continue
		}
		for _, reader := range facts.readers {
			if reader < 0 || reader >= len(state.probe.actions) || state.visited[reader] || state.probe.tooling[reader] || isDeliveryOnlyAction(state.probe, reader) {
				continue
			}
			state.visited[reader] = true
			addPropagatedActionProfile(&state.profile, state.probe, reader, path)
			state.pathStack = appendPropagationSeedPaths(
				state.pathStack,
				buildPropagationSeedWrites(state.probe, state.probe.actions[reader].writes, pathHasDiffPropagationReaders),
			)
		}
	}
}

func diffAddBaseOnlyProfile(state *diffState) {
	for _, idx := range state.baseOnly {
		if _, ok := state.refined.base[idx]; ok {
			continue
		}
		addActionProfile(&state.profile, state.base, idx)
	}
}

func diffInitPropagation(state *diffState) {
	state.visited = make([]bool, len(state.probe.actions))
	state.seedPaths = make([]string, 0, len(state.probeOnly))
	state.pathStack = make([]string, 0, len(state.probeOnly))
}

func diffAddRefinedPairs(state *diffState) {
	for _, diff := range state.refined.pairs {
		addProfilePaths(&state.profile, state.base, diff.baseReads, false)
		addProfilePaths(&state.profile, state.base, diff.baseWrites, true)
		addProfilePaths(&state.profile, state.probe, diff.probeReads, false)
		addProfilePaths(&state.profile, state.probe, diff.probeWrites, true)
		state.seedPaths = appendPropagationSeedPaths(state.seedPaths, diff.probeSeedWrites)
	}
}

func diffSeedUnmatchedProbeActions(state *diffState) {
	for _, idx := range state.probeOnly {
		if _, ok := state.refined.probe[idx]; ok {
			continue
		}
		addUnmatchedProbeActionProfile(&state.profile, state.probe, idx)
		state.seedPaths = appendPropagationSeedPaths(state.seedPaths, buildUnmatchedProbePropagationSeedWrites(state.probe, idx))
	}
}

func initOptionProfile() optionProfile {
	return optionProfile{
		propagatingReads:  make(map[string]struct{}),
		propagatingWrites: make(map[string]struct{}),
		unknownReads:      make(map[string]struct{}),
		unknownWrites:     make(map[string]struct{}),
		deliveryWrites:    make(map[string]struct{}),
		toolingReads:      make(map[string]struct{}),
		toolingWrites:     make(map[string]struct{}),
		paramTouches:      make(map[string]struct{}),
	}
}

type refinedDiffResult struct {
	pairs []refinedActionDiff
	base  map[int]struct{}
	probe map[int]struct{}
}

type refinedActionDiff struct {
	baseIdx         int
	probeIdx        int
	baseReads       []string
	baseWrites      []string
	probeReads      []string
	probeWrites     []string
	probeSeedWrites []string
}

func refineDiffActions(base, probe actionGraph, baseOnly, probeOnly []int) refinedDiffResult {
	result := initRefinedDiffResult()
	baseGroups := groupRefinableActions(base, baseOnly)
	probeGroups := groupRefinableActions(probe, probeOnly)
	for key, left := range baseGroups {
		diffRefineGroup(&result, base, probe, left, probeGroups[key])
	}
	return result
}

func initRefinedDiffResult() refinedDiffResult {
	return refinedDiffResult{
		base:  make(map[int]struct{}),
		probe: make(map[int]struct{}),
	}
}

func diffRefineGroup(result *refinedDiffResult, base, probe actionGraph, baseIndexes, probeIndexes []int) {
	if len(baseIndexes) != 1 || len(probeIndexes) != 1 {
		return
	}
	baseIdx := baseIndexes[0]
	probeIdx := probeIndexes[0]
	result.base[baseIdx] = struct{}{}
	result.probe[probeIdx] = struct{}{}
	result.pairs = append(result.pairs, buildRefinedActionDiff(base, probe, baseIdx, probeIdx))
}

func buildRefinedActionDiff(base, probe actionGraph, baseIdx, probeIdx int) refinedActionDiff {
	probeWrites := scopedPathDiff(probe.actions[probeIdx].writes, probe.scope, base.actions[baseIdx].writes, base.scope)
	return refinedActionDiff{
		baseIdx:     baseIdx,
		probeIdx:    probeIdx,
		baseReads:   scopedPathDiff(base.actions[baseIdx].reads, base.scope, probe.actions[probeIdx].reads, probe.scope),
		baseWrites:  scopedPathDiff(base.actions[baseIdx].writes, base.scope, probe.actions[probeIdx].writes, probe.scope),
		probeReads:  scopedPathDiff(probe.actions[probeIdx].reads, probe.scope, base.actions[baseIdx].reads, base.scope),
		probeWrites: probeWrites,
		// Refined configure/tooling pairs only propagate through writes that are
		// still observed by business data consumers; the rest stay as profile-only
		// bookkeeping.
		probeSeedWrites: buildPropagationSeedWrites(probe, probeWrites, pathHasBusinessDataReaders),
	}
}

func buildPropagationSeedWrites(graph actionGraph, writes []string, readerFilter func(actionGraph, pathFacts) bool) []string {
	if len(writes) == 0 {
		return nil
	}
	seeds := make([]string, 0, len(writes))
	seen := make(map[string]struct{}, len(writes))
	for _, path := range writes {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		facts, ok := graph.paths[path]
		if !ok {
			continue
		}
		if !pathSeedsDiffPropagation(graph, facts) {
			continue
		}
		if !readerFilter(graph, facts) {
			continue
		}
		seeds = append(seeds, path)
	}
	return seeds
}

func groupRefinableActions(graph actionGraph, indexes []int) map[string][]int {
	groups := make(map[string][]int)
	for _, idx := range indexes {
		if !isRefinableDiffAction(graph, idx) {
			continue
		}
		key := graph.actions[idx].actionKey
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], idx)
	}
	return groups
}

func isRefinableDiffAction(graph actionGraph, idx int) bool {
	if idx < 0 || idx >= len(graph.actions) {
		return false
	}
	return graph.actions[idx].kind == kindConfigure || graph.tooling[idx]
}

func scopedPathDiff(left []string, leftScope trace.Scope, right []string, rightScope trace.Scope) []string {
	if len(left) == 0 {
		return nil
	}
	rightSet := make(map[string]struct{}, len(right))
	for _, path := range right {
		rightSet[normalizeScopeToken(path, rightScope)] = struct{}{}
	}
	diff := make([]string, 0, len(left))
	seen := make(map[string]struct{}, len(left))
	for _, path := range left {
		token := normalizeScopeToken(path, leftScope)
		if _, ok := rightSet[token]; ok {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		diff = append(diff, path)
	}
	return diff
}

func addProfilePaths(profile *optionProfile, graph actionGraph, paths []string, write bool) {
	for _, path := range paths {
		facts, ok := graph.paths[path]
		if !ok {
			continue
		}
		addProfilePath(profile, facts.role, path, write)
	}
}

func appendReaderSeeds(stack []int, graph actionGraph, writes []string) []int {
	for _, path := range writes {
		facts, ok := graph.paths[path]
		if !ok {
			continue
		}
		if !pathSeedsDiffPropagation(graph, facts) {
			continue
		}
		for _, reader := range facts.readers {
			if graph.tooling[reader] || isDeliveryOnlyAction(graph, reader) {
				continue
			}
			stack = append(stack, reader)
		}
	}
	return stack
}

func appendPropagationSeedPaths(seedPaths []string, writes []string) []string {
	if len(writes) == 0 {
		return seedPaths
	}
	return append(seedPaths, writes...)
}

func buildUnmatchedProbePropagationSeedWrites(graph actionGraph, idx int) []string {
	if idx < 0 || idx >= len(graph.actions) {
		return nil
	}
	if graph.tooling[idx] || isDeliveryOnlyAction(graph, idx) {
		return nil
	}
	if graph.actions[idx].kind == kindConfigure {
		return buildPropagationSeedWrites(graph, graph.actions[idx].writes, pathHasBusinessDataReaders)
	}
	return buildPropagationSeedWrites(graph, graph.actions[idx].writes, pathHasDiffPropagationReaders)
}

func pathSeedsDiffPropagation(graph actionGraph, facts pathFacts) bool {
	switch facts.role {
	case roleUnknown, roleTooling:
		return false
	case roleDelivery:
		return pathHasDiffPropagationReaders(graph, facts)
	default:
		return true
	}
}

func pathHasDiffPropagationReaders(graph actionGraph, facts pathFacts) bool {
	for _, reader := range facts.readers {
		if graph.tooling[reader] || isDeliveryOnlyAction(graph, reader) {
			continue
		}
		return true
	}
	return false
}

func pathHasBusinessDataReaders(graph actionGraph, facts pathFacts) bool {
	for _, reader := range facts.readers {
		if reader < 0 || reader >= len(graph.actions) {
			continue
		}
		isTooling := reader < len(graph.tooling) && graph.tooling[reader]
		isBusiness := reader < len(graph.business) && graph.business[reader]
		if isTooling || isDeliveryOnlyAction(graph, reader) || !isBusiness {
			continue
		}
		if actionConsumesBusinessData(graph.actions[reader]) {
			return true
		}
	}
	return false
}

func addParamTouches(profile *optionProfile, base, probe actionGraph, baseOnly, probeOnly []int) {
	baseGroups := groupSemanticActionKeys(base, baseOnly)
	probeGroups := groupSemanticActionKeys(probe, probeOnly)
	for key, left := range baseGroups {
		right := probeGroups[key]
		if len(left) == 0 || len(right) == 0 {
			continue
		}
		profile.paramTouches[key] = struct{}{}
	}
}

func groupSemanticActionKeys(graph actionGraph, indexes []int) map[string][]int {
	groups := make(map[string][]int)
	for _, idx := range indexes {
		if idx < 0 || idx >= len(graph.actions) {
			continue
		}
		if idx >= len(graph.business) || !graph.business[idx] {
			continue
		}
		if idx < len(graph.tooling) && graph.tooling[idx] {
			continue
		}
		action := graph.actions[idx]
		if action.actionKey == "" {
			continue
		}
		switch action.kind {
		case kindCompile, kindLink, kindArchive:
		default:
			continue
		}
		groups[action.actionKey] = append(groups[action.actionKey], idx)
	}
	return groups
}

func addActionProfile(profile *optionProfile, graph actionGraph, idx int) {
	action := graph.actions[idx]
	if isDeliveryOnlyAction(graph, idx) {
		for _, changed := range action.writes {
			addProfilePath(profile, graph.paths[changed].role, changed, true)
		}
		return
	}
	for _, input := range action.reads {
		addProfilePath(profile, graph.paths[input].role, input, false)
	}
	for _, changed := range action.writes {
		addProfilePath(profile, graph.paths[changed].role, changed, true)
	}
}

func addUnmatchedProbeActionProfile(profile *optionProfile, graph actionGraph, idx int) {
	if idx < 0 || idx >= len(graph.actions) {
		return
	}
	action := graph.actions[idx]
	if action.kind == kindConfigure || graph.tooling[idx] {
		for _, changed := range action.writes {
			addProfilePath(profile, graph.paths[changed].role, changed, true)
		}
		return
	}
	addActionProfile(profile, graph, idx)
}

func addPropagatedActionProfile(profile *optionProfile, graph actionGraph, idx int, seedPath string) {
	action := graph.actions[idx]
	if len(action.writes) == 0 {
		return
	}
	if facts, ok := graph.paths[seedPath]; ok {
		addProfilePath(profile, facts.role, seedPath, false)
	}
	for _, changed := range action.writes {
		addProfilePath(profile, graph.paths[changed].role, changed, true)
	}
}

func isDeliveryOnlyAction(graph actionGraph, idx int) bool {
	action := graph.actions[idx]
	if len(action.writes) == 0 {
		return false
	}
	explicitDeliveryOnly := true
	for _, changed := range action.writes {
		if graph.paths[changed].role != roleDelivery {
			return false
		}
		if !isExplicitDeliveryPath(changed, graph.scope) {
			explicitDeliveryOnly = false
		}
	}
	if action.kind == kindCopy || action.kind == kindInstall {
		return true
	}
	return explicitDeliveryOnly
}

func addProfilePath(profile *optionProfile, role pathRole, path string, write bool) {
	switch role {
	case roleTooling:
		if write {
			profile.toolingWrites[path] = struct{}{}
			return
		}
		profile.toolingReads[path] = struct{}{}
	case roleDelivery:
		if write {
			profile.deliveryWrites[path] = struct{}{}
			return
		}
		profile.propagatingReads[path] = struct{}{}
	case roleUnknown:
		if write {
			profile.unknownWrites[path] = struct{}{}
			return
		}
		profile.unknownReads[path] = struct{}{}
	default:
		if write {
			profile.propagatingWrites[path] = struct{}{}
			return
		}
		profile.propagatingReads[path] = struct{}{}
	}
}

func zeroDiffOptionKeys(profiles map[string][]optionVariant) map[string]struct{} {
	tainted := make(map[string]struct{})
	for key, variants := range profiles {
		if len(variants) == 0 {
			continue
		}
		allEmpty := true
		for _, variant := range variants {
			if variant.empty() {
				continue
			}
			allEmpty = false
			break
		}
		if allEmpty {
			tainted[key] = struct{}{}
		}
	}
	return tainted
}

func collisionComponents(optionKeys []string, profiles map[string][]optionVariant, zeroDiff map[string]struct{}) [][]string {
	keys := make([]string, 0, len(optionKeys))
	for _, key := range optionKeys {
		if _, ok := zeroDiff[key]; ok {
			continue
		}
		keys = append(keys, key)
	}
	adj := make(map[string]map[string]struct{}, len(keys))
	for _, key := range keys {
		adj[key] = make(map[string]struct{})
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			left, right := keys[i], keys[j]
			if !profilesCollide(profiles[left], profiles[right]) {
				continue
			}
			adj[left][right] = struct{}{}
			adj[right][left] = struct{}{}
		}
	}

	visited := make(map[string]bool, len(keys))
	var components [][]string
	for _, key := range keys {
		if visited[key] {
			continue
		}
		component := []string{}
		stack := []string{key}
		for len(stack) > 0 {
			node := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if visited[node] {
				continue
			}
			visited[node] = true
			component = append(component, node)
			for next := range adj[node] {
				if !visited[next] {
					stack = append(stack, next)
				}
			}
		}
		slices.Sort(component)
		components = append(components, component)
	}
	return components
}

func orthogonalOptionKeys(optionKeys []string, zeroDiff map[string]struct{}) []string {
	keys := make([]string, 0, len(zeroDiff))
	for _, key := range optionKeys {
		if _, ok := zeroDiff[key]; ok {
			keys = append(keys, key)
		}
	}
	return keys
}

func profilesCollide(left, right []optionVariant) bool {
	for _, l := range left {
		for _, r := range right {
			if optionVariantsCollide(l, r) {
				return true
			}
		}
	}
	return false
}

func optionVariantsCollide(left, right optionVariant) bool {
	writeWrite := overlapSets(profileCollisionWriteSets(left.profile), profileCollisionWriteSets(right.profile))
	leftWriteRightRead := overlapSets(profileCollisionWriteSets(left.profile), profileCollisionReadSets(right.profile))
	leftReadRightWrite := overlapSets(profileCollisionReadSets(left.profile), profileCollisionWriteSets(right.profile))
	paramShared := overlap(left.profile.paramTouches, right.profile.paramTouches)
	if leftWriteRightRead || leftReadRightWrite || paramShared {
		return true
	}
	if !writeWrite {
		return false
	}
	return outputManifestDiffsCollide(left.outputDiff, right.outputDiff)
}

func profileCollisionWriteSets(profile optionProfile) []map[string]struct{} {
	return []map[string]struct{}{
		profile.propagatingWrites,
		profile.unknownWrites,
	}
}

func profileCollisionReadSets(profile optionProfile) []map[string]struct{} {
	return []map[string]struct{}{
		profile.propagatingReads,
		profile.unknownReads,
	}
}

func (profile optionProfile) empty() bool {
	return len(profile.propagatingReads) == 0 &&
		len(profile.propagatingWrites) == 0 &&
		len(profile.unknownReads) == 0 &&
		len(profile.unknownWrites) == 0 &&
		len(profile.deliveryWrites) == 0 &&
		len(profile.toolingReads) == 0 &&
		len(profile.toolingWrites) == 0 &&
		len(profile.paramTouches) == 0
}

func (variant optionVariant) empty() bool {
	return variant.profile.empty() && variant.outputDiff.empty()
}

func overlapSets(leftSets, rightSets []map[string]struct{}) bool {
	for _, left := range leftSets {
		for _, right := range rightSets {
			if overlap(left, right) {
				return true
			}
		}
	}
	return false
}

func overlap(left, right map[string]struct{}) bool {
	for path := range left {
		if _, ok := right[path]; ok {
			return true
		}
	}
	return false
}

func componentCombos(
	requireCombo string,
	options map[string][]string,
	defaults map[string]string,
	optionKeys []string,
	components [][]string,
	orthogonalKeys []string,
) []string {
	seen := make(map[string]struct{})
	selections := []map[string]string{{}}
	for _, component := range components {
		var expand func(int, map[string]string)
		expand = func(i int, selected map[string]string) {
			if i == len(component) {
				selections = append(selections, maps.Clone(selected))
				return
			}
			key := component[i]
			for _, value := range options[key] {
				selected[key] = value
				expand(i+1, selected)
			}
		}
		expand(0, make(map[string]string, len(component)))
	}

	for _, selected := range selections {
		var expand func(int, map[string]string)
		expand = func(i int, override map[string]string) {
			if i == len(orthogonalKeys) {
				merged := maps.Clone(defaults)
				for key, value := range selected {
					merged[key] = value
				}
				for key, value := range override {
					merged[key] = value
				}
				seen[composeCombo(requireCombo, merged, optionKeys)] = struct{}{}
				return
			}
			key := orthogonalKeys[i]
			for _, value := range options[key] {
				override[key] = value
				expand(i+1, override)
			}
			delete(override, key)
		}
		expand(0, make(map[string]string, len(orthogonalKeys)))
	}
	return slices.Sorted(maps.Keys(seen))
}

func defaultOptions(matrix formula.Matrix) map[string]string {
	out := make(map[string]string, len(matrix.Options))
	for _, key := range slices.Sorted(maps.Keys(matrix.Options)) {
		values := matrix.Options[key]
		if len(values) == 0 {
			continue
		}
		def := values[0]
		if defaults := matrix.DefaultOptions[key]; len(defaults) > 0 && slices.Contains(values, defaults[0]) {
			def = defaults[0]
		}
		out[key] = def
	}
	return out
}

func expandRequireCombos(require map[string][]string) []string {
	keys := slices.Sorted(maps.Keys(require))
	if len(keys) == 0 {
		return nil
	}
	combos := []string{""}
	for _, key := range keys {
		values := require[key]
		next := make([]string, 0, len(combos)*len(values))
		for _, prefix := range combos {
			for _, value := range values {
				if prefix == "" {
					next = append(next, value)
					continue
				}
				next = append(next, prefix+"-"+value)
			}
		}
		combos = next
	}
	return combos
}

func composeCombo(requireCombo string, options map[string]string, optionKeys []string) string {
	optionParts := make([]string, 0, len(optionKeys))
	for _, key := range optionKeys {
		if value, ok := options[key]; ok && value != "" {
			optionParts = append(optionParts, value)
		}
	}
	optionCombo := strings.Join(optionParts, "-")
	switch {
	case requireCombo == "":
		return optionCombo
	case optionCombo == "":
		return requireCombo
	default:
		return requireCombo + "|" + optionCombo
	}
}

func normalizeRecord(record trace.Record) normalizedRecord {
	normalizer := resolveNormalizer(record)
	argv, cwd, inputs, changes := normalizer.normalize(record)
	inputOrigin := make(map[string]string, len(record.Inputs))
	for _, path := range record.Inputs {
		normalized := normalizePath(path)
		if normalized == "" {
			continue
		}
		if _, ok := inputOrigin[normalized]; !ok {
			inputOrigin[normalized] = path
		}
	}
	inputs = uniqueSorted(inputs)
	changes = uniqueSorted(changes)
	parts := append([]string{}, argv...)
	parts = append(parts, "@", cwd, "@")
	parts = append(parts, inputs...)
	parts = append(parts, "@")
	parts = append(parts, changes...)
	return normalizedRecord{
		pid:         record.PID,
		parentPID:   record.ParentPID,
		argv:        argv,
		cwd:         cwd,
		inputs:      inputs,
		changes:     changes,
		inputOrigin: inputOrigin,
		fingerprint: strings.Join(parts, "\x1f"),
	}
}

type recordNormalizer interface {
	match(trace.Record) bool
	normalize(trace.Record) ([]string, string, []string, []string)
}

func resolveNormalizer(record trace.Record) recordNormalizer {
	for _, normalizer := range []recordNormalizer{
		ccNormalizer{},
		cmakeNormalizer{},
		pythonNormalizer{},
		goNormalizer{},
		genericNormalizer{},
	} {
		if normalizer.match(record) {
			return normalizer
		}
	}
	return genericNormalizer{}
}

type genericNormalizer struct{}

func (genericNormalizer) match(trace.Record) bool { return true }

func (genericNormalizer) normalize(record trace.Record) ([]string, string, []string, []string) {
	argv := make([]string, 0, len(record.Argv))
	for _, arg := range record.Argv {
		argv = append(argv, strings.ReplaceAll(normalizePath(arg), `\`, `/`))
	}
	inputs := make([]string, 0, len(record.Inputs))
	for _, path := range record.Inputs {
		inputs = append(inputs, normalizePath(path))
	}
	changes := make([]string, 0, len(record.Changes))
	for _, path := range record.Changes {
		changes = append(changes, normalizePath(path))
	}
	return argv, normalizePath(record.Cwd), inputs, changes
}

type ccNormalizer struct{ genericNormalizer }

func (ccNormalizer) match(record trace.Record) bool {
	tool := ""
	if len(record.Argv) > 0 {
		tool = filepath.Base(record.Argv[0])
	}
	switch tool {
	case "cc", "c++", "gcc", "g++", "clang", "clang++", "ld", "ar":
		return true
	default:
		return false
	}
}

type cmakeNormalizer struct{ genericNormalizer }

func (cmakeNormalizer) match(record trace.Record) bool {
	tool := ""
	if len(record.Argv) > 0 {
		tool = filepath.Base(record.Argv[0])
	}
	return tool == "cmake" || tool == "ninja" || tool == "make"
}

type pythonNormalizer struct{ genericNormalizer }

func (pythonNormalizer) match(record trace.Record) bool {
	tool := ""
	if len(record.Argv) > 0 {
		tool = filepath.Base(record.Argv[0])
	}
	if strings.HasPrefix(tool, "python") {
		return true
	}
	return tool == "pip"
}

type goNormalizer struct{ genericNormalizer }

func (goNormalizer) match(record trace.Record) bool {
	if len(record.Argv) == 0 {
		return false
	}
	return filepath.Base(record.Argv[0]) == "go"
}

func normalizePath(path string) string {
	if path == "" {
		return ""
	}
	path = filepath.ToSlash(path)
	path = reTmpUnix.ReplaceAllString(path, "/tmp/$$$$TMP")
	path = reTmpMac.ReplaceAllString(path, "/var/folders/$$$$TMP")
	return path
}

func normalizeScopeToken(token string, scope trace.Scope) string {
	if token == "" {
		return ""
	}
	token = normalizePath(token)
	replacements := []struct {
		root        string
		placeholder string
	}{
		{scope.BuildRoot, "$BUILD"},
		{scope.InstallRoot, "$INSTALL"},
		{scope.SourceRoot, "$SRC"},
	}
	slices.SortFunc(replacements, func(left, right struct {
		root        string
		placeholder string
	}) int {
		if len(left.root) != len(right.root) {
			return len(right.root) - len(left.root)
		}
		return strings.Compare(left.placeholder, right.placeholder)
	})
	for _, item := range replacements {
		if item.root == "" {
			continue
		}
		token = replaceScopeRootToken(token, item.root, item.placeholder)
	}
	return normalizeScopedBuildNoise(token)
}

func replaceScopeRootToken(token, root, placeholder string) string {
	token = strings.ReplaceAll(token, root, placeholder)
	if !strings.Contains(root, "$$TMP") {
		return token
	}
	pattern := regexp.QuoteMeta(root)
	pattern = strings.ReplaceAll(pattern, `\$\$TMP`, `[^/]+`)
	re := regexp.MustCompile(pattern)
	return re.ReplaceAllString(token, strings.ReplaceAll(placeholder, "$", "$$"))
}

func normalizeScopedBuildNoise(token string) string {
	if !strings.Contains(token, "$BUILD") {
		return token
	}
	token = reBuildTryCompileNoise.ReplaceAllString(token, `${1}TryCompile-$$$$ID$2`)
	token = reBuildCmTCNoise.ReplaceAllString(token, `${1}cmTC_$$$$ID$2`)
	token = reBuildTmpPIDNoise.ReplaceAllString(token, `.tmp.$$$$ID$1`)
	return token
}

func uniqueSorted(values []string) []string {
	out := slices.Clone(values)
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	out = slices.DeleteFunc(out, func(value string) bool {
		return value == ""
	})
	slices.Sort(out)
	return slices.Compact(out)
}
