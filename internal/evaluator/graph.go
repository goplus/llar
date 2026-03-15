package evaluator

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goplus/llar/internal/trace"
)

type actionKind uint8

const (
	kindGeneric actionKind = iota
	kindCompile
	kindLink
	kindArchive
	kindCopy
	kindInstall
	kindConfigure
)

type graphEdge struct {
	from int
	to   int
	path string
}

type actionNode struct {
	pid         int64
	parentPID   int64
	argv        []string
	cwd         string
	reads       []string
	writes      []string
	inputOrigin map[string]string
	execPath    string
	tool        string
	kind        actionKind
	actionKey   string
	fullKey     string
	fingerprint string
}

type actionGraph struct {
	records  int
	scope    trace.Scope
	actions  []actionNode
	out      [][]graphEdge
	in       [][]graphEdge
	indeg    []int
	outdeg   []int
	tooling  []bool
	business []bool
	paths    map[string]pathFacts
}

type pathRole uint8

const (
	roleUnknown pathRole = iota
	roleTooling
	rolePropagating
	roleDelivery
)

type pathFacts struct {
	path    string
	writers []int
	readers []int
	role    pathRole
}

type analysisState struct {
	records      []trace.Record
	scope        trace.Scope
	inputDigests map[string]string

	normalized  []normalizedRecord
	directories map[string]struct{}
	actions     []actionNode

	out          [][]graphEdge
	in           [][]graphEdge
	indeg        []int
	outdeg       []int
	paths        map[string]pathFacts
	parentAction []int

	deliverySeed         []bool
	toolingBlockedAction []bool
	toolingBlockedPath   map[string]struct{}
	controlPlanePaths    map[string]struct{}
	probeInputPaths      map[string]struct{}
	seedTooling          []bool
	probeSeed            []bool
	tooling              []bool
	business             []bool
}

func buildGraph(records []trace.Record) actionGraph {
	return buildGraphWithScope(records, trace.Scope{})
}

func buildGraphWithScope(records []trace.Record, scope trace.Scope) actionGraph {
	return buildGraphWithScopeAndDigests(records, scope, nil)
}

func buildGraphWithScopeAndDigests(records []trace.Record, scope trace.Scope, inputDigests map[string]string) actionGraph {
	state := newAnalysisState(records, scope, inputDigests)
	runNormalizePass(state)
	runActionKindPass(state)
	runCompileMergePass(state)
	runGraphBuildPass(state)
	runProcessTreePass(state)
	runDeliverySeedPass(state)
	runToolingSeedPass(state)
	runBusinessPass(state, state.seedTooling)
	runToolingHardNegativePass(state)
	runBusinessPass(state, state.seedTooling)
	runControlPlanePass(state, state.seedTooling)
	runProbeSubgraphPass(state)
	runToolingFinalizePass(state)
	runBusinessPass(state, state.tooling)
	runControlPlanePass(state, state.tooling)
	runPathRoleFinalizePass(state)
	return freezeActionGraph(state)
}

func newAnalysisState(records []trace.Record, scope trace.Scope, inputDigests map[string]string) *analysisState {
	return &analysisState{
		records:      records,
		scope:        normalizeScope(scope),
		inputDigests: inputDigests,
	}
}

func runNormalizePass(state *analysisState) {
	state.normalized = make([]normalizedRecord, 0, len(state.records))
	for _, record := range state.records {
		state.normalized = append(state.normalized, normalizeRecord(record))
	}
	state.directories = inferDirectoryLikePaths(state.normalized)
}

func runActionKindPass(state *analysisState) {
	state.actions = make([]actionNode, 0, len(state.normalized))
	for _, record := range state.normalized {
		filtered := filterDirectoryPaths(record, state.directories)
		tool := ""
		if len(filtered.argv) > 0 {
			tool = filepath.Base(filtered.argv[0])
		}
		state.actions = append(state.actions, buildActionNode(
			filtered,
			tool,
			classifyActionKind(filtered),
			state.scope,
			state.inputDigests,
		))
	}
}

func runCompileMergePass(state *analysisState) {
	state.actions = coalesceCompilePipelines(state.actions, state.scope, state.inputDigests)
}

func runGraphBuildPass(state *analysisState) {
	actions := state.actions
	state.out = make([][]graphEdge, len(actions))
	state.in = make([][]graphEdge, len(actions))
	state.indeg = make([]int, len(actions))
	state.outdeg = make([]int, len(actions))
	state.paths = make(map[string]pathFacts)

	lastWriter := make(map[string]int)
	for i, action := range actions {
		for _, read := range action.reads {
			facts := state.paths[read]
			facts.path = read
			facts.readers = append(facts.readers, i)
			state.paths[read] = facts
		}
		for _, written := range action.writes {
			facts := state.paths[written]
			facts.path = written
			facts.writers = append(facts.writers, i)
			state.paths[written] = facts
		}
		for _, read := range action.reads {
			writer, ok := lastWriter[read]
			if !ok {
				continue
			}
			edge := graphEdge{from: writer, to: i, path: read}
			state.out[writer] = append(state.out[writer], edge)
			state.in[i] = append(state.in[i], edge)
			state.outdeg[writer]++
			state.indeg[i]++
		}
		for _, written := range action.writes {
			lastWriter[written] = i
		}
	}
}

func runProcessTreePass(state *analysisState) {
	state.parentAction = make([]int, len(state.actions))
	for i := range state.parentAction {
		state.parentAction[i] = -1
	}
	lastByPID := make(map[int64]int, len(state.actions))
	for i, action := range state.actions {
		// Parent lookup follows trace order: each record points at the latest
		// already-seen action for its recorded parent pid.
		if action.parentPID != 0 {
			if idx, ok := lastByPID[action.parentPID]; ok {
				state.parentAction[i] = idx
			}
		}
		if action.pid != 0 {
			lastByPID[action.pid] = i
		}
	}
}

func runDeliverySeedPass(state *analysisState) {
	state.deliverySeed = make([]bool, len(state.actions))
	for i, action := range state.actions {
		// Delivery seeds stay action-based: explicit install outputs and terminal
		// copy/install leaves are the backward-closure roots for business.
		state.deliverySeed[i] = actionWritesDelivery(action, state.outdeg[i], state.scope)
	}
}

func runToolingSeedPass(state *analysisState) {
	state.seedTooling = classifyToolingActions(state.actions, state.paths)
}

func runBusinessPass(state *analysisState, tooling []bool) {
	state.business = classifyBusinessActions(state.actions, state.in, state.outdeg, state.scope, tooling)
}

func runToolingHardNegativePass(state *analysisState) {
	state.toolingBlockedAction, state.toolingBlockedPath = classifyToolingHardNegatives(
		state.actions,
		state.paths,
		state.outdeg,
		state.scope,
		state.business,
	)
	for i, blocked := range state.toolingBlockedAction {
		if blocked {
			state.seedTooling[i] = false
		}
	}
}

func runToolingFinalizePass(state *analysisState) {
	state.tooling = finalizeToolingActions(
		state.actions,
		state.paths,
		state.parentAction,
		state.in,
		state.outdeg,
		state.scope,
		state.seedTooling,
		state.probeSeed,
		state.toolingBlockedAction,
		state.toolingBlockedPath,
	)
}

func runControlPlanePass(state *analysisState, tooling []bool) {
	state.probeInputPaths = classifyProbeInputPaths(
		state.paths,
		state.actions,
		state.parentAction,
		state.business,
		tooling,
		state.toolingBlockedPath,
	)
	state.controlPlanePaths = classifyControlPlanePaths(
		state.paths,
		state.actions,
		state.business,
		tooling,
		state.toolingBlockedPath,
		state.probeInputPaths,
	)
}

func runProbeSubgraphPass(state *analysisState) {
	state.probeSeed = classifyProbeSubgraphActions(
		state.actions,
		state.paths,
		state.parentAction,
		state.seedTooling,
		state.business,
		state.controlPlanePaths,
		state.probeInputPaths,
		state.toolingBlockedAction,
		state.toolingBlockedPath,
	)
}

func runPathRoleFinalizePass(state *analysisState) {
	for path, facts := range state.paths {
		// Delivery must win before tooling/business so staged install paths do not
		// fall back into the build graph roles.
		switch {
		case len(facts.readers) == 0 && len(facts.writers) == 0:
			facts.role = roleUnknown
		case isExplicitDeliveryPath(facts.path, state.scope):
			facts.role = roleDelivery
		case isDeliveryPath(state.actions, state.outdeg, facts):
			facts.role = roleDelivery
		case isStagedDeliveryPath(state.actions, state.tooling, state.business, facts, state.scope):
			facts.role = roleDelivery
		case isToolingPath(state.actions, state.tooling, facts, state.controlPlanePaths, state.probeInputPaths):
			facts.role = roleTooling
		case isBusinessPath(state.business, facts):
			facts.role = rolePropagating
		default:
			facts.role = roleUnknown
		}
		state.paths[path] = facts
	}
}

func freezeActionGraph(state *analysisState) actionGraph {
	return actionGraph{
		records:  len(state.records),
		scope:    state.scope,
		actions:  state.actions,
		out:      state.out,
		in:       state.in,
		indeg:    state.indeg,
		outdeg:   state.outdeg,
		tooling:  state.tooling,
		business: state.business,
		paths:    state.paths,
	}
}

func buildActionNode(normalized normalizedRecord, tool string, kind actionKind, scope trace.Scope, inputDigests map[string]string) actionNode {
	fingerprint := scopedFingerprint(kind, normalized, scope, inputDigests)
	return actionNode{
		pid:         normalized.pid,
		parentPID:   normalized.parentPID,
		argv:        normalized.argv,
		cwd:         normalized.cwd,
		reads:       normalized.inputs,
		writes:      normalized.changes,
		inputOrigin: maps.Clone(normalized.inputOrigin),
		execPath:    resolveExecPath(normalized.cwd, normalized.argv),
		tool:        tool,
		kind:        kind,
		actionKey:   buildActionKey(kind, tool, normalized, scope),
		fullKey:     fingerprint,
		fingerprint: fingerprint,
	}
}

func coalesceCompilePipelines(actions []actionNode, scope trace.Scope, inputDigests map[string]string) []actionNode {
	actions = coalesceCompilePipelinesByProcess(actions, scope, inputDigests)
	return coalesceAdjacentCompilePipelines(actions, scope, inputDigests)
}

func coalesceCompilePipelinesByProcess(actions []actionNode, scope trace.Scope, inputDigests map[string]string) []actionNode {
	if len(actions) == 0 {
		return nil
	}
	groups := findCompilePipelineGroups(actions)
	if len(groups) == 0 {
		return slices.Clone(actions)
	}
	startToGroup := make(map[int][]actionNode, len(groups))
	skipped := make(map[int]struct{})
	for _, group := range groups {
		start := group[0]
		members := make([]actionNode, 0, len(group))
		for _, idx := range group {
			members = append(members, actions[idx])
			skipped[idx] = struct{}{}
		}
		startToGroup[start] = members
	}
	merged := make([]actionNode, 0, len(actions))
	for i := 0; i < len(actions); i++ {
		if group, ok := startToGroup[i]; ok {
			merged = append(merged, mergeCompileGroup(group, scope, inputDigests))
			continue
		}
		if _, ok := skipped[i]; ok {
			continue
		}
		merged = append(merged, actions[i])
	}
	return merged
}

func coalesceAdjacentCompilePipelines(actions []actionNode, scope trace.Scope, inputDigests map[string]string) []actionNode {
	if len(actions) == 0 {
		return nil
	}
	merged := make([]actionNode, 0, len(actions))
	for i := 0; i < len(actions); {
		action := actions[i]
		if !isCompileFamilyAction(action) {
			merged = append(merged, action)
			i++
			continue
		}
		if group, ok := matchCompilePipeline(actions, i, 3); ok {
			merged = append(merged, mergeCompileGroup(group, scope, inputDigests))
			i += len(group)
			continue
		}
		if group, ok := matchCompilePipeline(actions, i, 2); ok {
			merged = append(merged, mergeCompileGroup(group, scope, inputDigests))
			i += len(group)
			continue
		}
		merged = append(merged, action)
		i++
	}
	return merged
}

func findCompilePipelineGroups(actions []actionNode) [][]int {
	used := make([]bool, len(actions))
	groups := make([][]int, 0)
	for i, action := range actions {
		if used[i] || compileStageOf(action) != compileStageDriver {
			continue
		}
		group, ok := matchCompilePipelineByProcess(actions, used, i)
		if !ok {
			continue
		}
		for _, idx := range group {
			used[idx] = true
		}
		groups = append(groups, group)
	}
	return groups
}

func matchCompilePipelineByProcess(actions []actionNode, used []bool, driverIdx int) ([]int, bool) {
	driver := actions[driverIdx]
	sig := compileSignature{
		source: actionPrimarySource(driver),
		object: compileDriverObjectOutput(driver),
	}
	if sig.source == "" || sig.object == "" {
		return nil, false
	}

	var best []int
	bestScore := 0
	ambiguous := false
	for assemblerIdx := driverIdx + 1; assemblerIdx < len(actions); assemblerIdx++ {
		if used[assemblerIdx] {
			continue
		}
		assembler := actions[assemblerIdx]
		if compileStageOf(assembler) != compileStageAssembler || compileObjectOutput(assembler) != sig.object {
			continue
		}

		group := []int{driverIdx, assemblerIdx}
		score := compileRelationScore(driver, assembler)
		if frontendIdx, frontendScore, ok := selectCompileFrontend(actions, used, driverIdx, assemblerIdx, sig.source); ok {
			candidate := []int{driverIdx, frontendIdx, assemblerIdx}
			if isRecognizedCompilePipeline(compileGroupActions(actions, candidate)) {
				group = candidate
				score = max(score, frontendScore)
			}
		}
		if score == 0 || !isRecognizedCompilePipeline(compileGroupActions(actions, group)) {
			continue
		}
		if score > bestScore {
			best = group
			bestScore = score
			ambiguous = false
			continue
		}
		if score == bestScore {
			ambiguous = true
		}
	}
	if len(best) == 0 || ambiguous {
		return nil, false
	}
	return best, true
}

func selectCompileFrontend(actions []actionNode, used []bool, driverIdx, assemblerIdx int, source string) (int, int, bool) {
	driver := actions[driverIdx]
	assembler := actions[assemblerIdx]

	bestIdx := -1
	bestScore := 0
	ambiguous := false
	for i := driverIdx + 1; i < assemblerIdx; i++ {
		if used[i] {
			continue
		}
		frontend := actions[i]
		if compileStageOf(frontend) != compileStageFrontend || actionPrimarySource(frontend) != source {
			continue
		}
		score := max(compileRelationScore(driver, frontend), compileRelationScore(frontend, assembler))
		if score == 0 {
			continue
		}
		if score > bestScore {
			bestIdx = i
			bestScore = score
			ambiguous = false
			continue
		}
		if score == bestScore {
			ambiguous = true
		}
	}
	if bestIdx < 0 || ambiguous {
		return 0, 0, false
	}
	return bestIdx, bestScore, true
}

func compileGroupActions(actions []actionNode, indices []int) []actionNode {
	group := make([]actionNode, 0, len(indices))
	for _, idx := range indices {
		group = append(group, actions[idx])
	}
	return group
}

func compileRelationScore(left, right actionNode) int {
	switch {
	case left.pid != 0 && right.parentPID == left.pid:
		return 3
	case right.pid != 0 && left.parentPID == right.pid:
		return 3
	case left.parentPID != 0 && left.parentPID == right.parentPID:
		return 2
	default:
		return 0
	}
}

type compileSignature struct {
	source string
	object string
}

type compileStage uint8

const (
	compileStageUnknown compileStage = iota
	compileStageDriver
	compileStageFrontend
	compileStageAssembler
)

func isCompileFamilyAction(action actionNode) bool {
	switch compileStageOf(action) {
	case compileStageDriver:
		return slices.Contains(action.argv, "-c") && actionPrimarySource(action) != ""
	case compileStageFrontend:
		return actionPrimarySource(action) != ""
	case compileStageAssembler:
		return compileObjectOutput(action) != ""
	default:
		return false
	}
}

func matchCompilePipeline(actions []actionNode, start, size int) ([]actionNode, bool) {
	if start < 0 || size < 2 || start+size > len(actions) {
		return nil, false
	}
	group := actions[start : start+size]
	for _, action := range group {
		if !isCompileFamilyAction(action) {
			return nil, false
		}
	}
	if !isRecognizedCompilePipeline(group) {
		return nil, false
	}
	return group, true
}

func isRecognizedCompilePipeline(group []actionNode) bool {
	if len(group) < 2 || len(group) > 3 {
		return false
	}
	pattern := make([]compileStage, 0, len(group))
	for _, action := range group {
		stage := compileStageOf(action)
		if stage == compileStageUnknown {
			return false
		}
		pattern = append(pattern, stage)
	}
	switch len(pattern) {
	case 2:
		if !slices.Equal(pattern, []compileStage{compileStageDriver, compileStageAssembler}) &&
			!slices.Equal(pattern, []compileStage{compileStageFrontend, compileStageAssembler}) {
			return false
		}
	case 3:
		if !slices.Equal(pattern, []compileStage{compileStageDriver, compileStageFrontend, compileStageAssembler}) {
			return false
		}
	}
	sig := compileGroupSignature(group)
	if sig.source == "" || sig.object == "" {
		return false
	}
	if !compileFamiliesCompatible(group) {
		return false
	}
	if !compileStagesVerified(group, sig.object) {
		return false
	}
	return true
}

func compileStagesVerified(group []actionNode, object string) bool {
	for _, action := range group {
		switch compileStageOf(action) {
		case compileStageDriver:
			if !slices.Contains(action.argv, "-c") {
				return false
			}
			if out := compileDriverObjectOutput(action); out == "" || out != object {
				return false
			}
		case compileStageAssembler:
			if compileObjectOutput(action) != object {
				return false
			}
		}
	}
	return true
}

func compileGroupSignature(group []actionNode) compileSignature {
	var sig compileSignature
	for _, action := range group {
		next := compileSignature{
			source: actionPrimarySource(action),
			object: compileObjectOutput(action),
		}
		if next.source != "" {
			if sig.source != "" && sig.source != next.source {
				return compileSignature{}
			}
			sig.source = next.source
		}
		if next.object != "" {
			if sig.object != "" && sig.object != next.object {
				return compileSignature{}
			}
			sig.object = next.object
		}
	}
	return sig
}

func compileFamiliesCompatible(group []actionNode) bool {
	seenCompileFamily := false
	for _, action := range group {
		stage := compileStageOf(action)
		if stage == compileStageAssembler {
			continue
		}
		current := toolFamily(action.tool)
		if current != "cc" && current != "cxx" {
			return false
		}
		seenCompileFamily = true
	}
	return seenCompileFamily
}

func compileStageOf(action actionNode) compileStage {
	switch filepath.Base(action.tool) {
	case "cc1", "cc1plus":
		return compileStageFrontend
	case "as":
		return compileStageAssembler
	}
	switch toolFamily(action.tool) {
	case "cc", "cxx":
		return compileStageDriver
	default:
		return compileStageUnknown
	}
}

func mergeCompileGroup(group []actionNode, scope trace.Scope, inputDigests map[string]string) actionNode {
	cwd := ""
	tool := ""
	for _, action := range group {
		// Keep the first non-empty cwd and first non-assembler tool so the merged
		// compile stays anchored to the driver stage when present.
		if cwd == "" && action.cwd != "" {
			cwd = action.cwd
		}
		if tool == "" && filepath.Base(action.tool) != "as" {
			tool = action.tool
		}
	}
	if tool == "" && len(group) != 0 {
		tool = group[0].tool
	}
	record := normalizedRecord{
		pid:         group[0].pid,
		parentPID:   group[0].parentPID,
		argv:        mergeCompileArgv(group),
		cwd:         cwd,
		inputs:      mergeCompilePaths(group, false),
		changes:     mergeCompilePaths(group, true),
		inputOrigin: mergeCompileInputOrigins(group),
	}
	return buildActionNode(record, tool, kindCompile, scope, inputDigests)
}

func mergeCompileArgv(group []actionNode) []string {
	merged := make([]string, 0)
	for _, action := range group {
		if filepath.Base(action.tool) == "as" {
			continue
		}
		if len(merged) > 0 {
			merged = append(merged, "||")
		}
		merged = append(merged, action.argv...)
	}
	if len(merged) != 0 {
		return merged
	}
	for _, action := range group {
		merged = append(merged, action.argv...)
	}
	return merged
}

func mergeCompilePaths(group []actionNode, writes bool) []string {
	merged := make([]string, 0)
	for _, action := range group {
		paths := action.reads
		if writes {
			paths = action.writes
		}
		merged = append(merged, paths...)
	}
	return uniqueSorted(merged)
}

func mergeCompileInputOrigins(group []actionNode) map[string]string {
	merged := make(map[string]string)
	for _, action := range group {
		for path, origin := range action.inputOrigin {
			if _, ok := merged[path]; ok {
				continue
			}
			merged[path] = origin
		}
	}
	return merged
}

func filterDirectoryPaths(record normalizedRecord, directories map[string]struct{}) normalizedRecord {
	if len(directories) != 0 {
		// Traces can include directory-like entries inferred from path prefixes; we
		// strip them here so later passes only reason about file paths.
		filter := func(paths []string) []string {
			if len(paths) == 0 {
				return paths
			}
			filtered := make([]string, 0, len(paths))
			for _, path := range paths {
				if _, ok := directories[path]; ok {
					continue
				}
				filtered = append(filtered, path)
			}
			return filtered
		}
		record.inputs = filter(record.inputs)
		record.changes = filter(record.changes)
	}
	return record
}

func inferDirectoryLikePaths(records []normalizedRecord) map[string]struct{} {
	seen := make(map[string]struct{})
	paths := make([]string, 0)
	add := func(path string) {
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	for _, record := range records {
		for _, path := range record.inputs {
			add(path)
		}
		for _, path := range record.changes {
			add(path)
		}
	}
	slices.Sort(paths)
	directories := make(map[string]struct{})
	for i := 0; i < len(paths); i++ {
		path := paths[i]
		prefix := path + "/"
		for j := i + 1; j < len(paths); j++ {
			next := paths[j]
			if !strings.HasPrefix(next, path) {
				break
			}
			if strings.HasPrefix(next, prefix) {
				directories[path] = struct{}{}
				break
			}
		}
	}
	return directories
}

func scopedFingerprint(kind actionKind, record normalizedRecord, scope trace.Scope, inputDigests map[string]string) string {
	argv := make([]string, 0, len(record.argv))
	for _, arg := range record.argv {
		argv = append(argv, normalizeScopeToken(arg, scope))
	}
	inputs := make([]string, 0, len(record.inputs))
	for _, path := range record.inputs {
		inputs = append(inputs, fingerprintInputToken(kind, path, record.inputOrigin[path], scope, inputDigests))
	}
	changes := make([]string, 0, len(record.changes))
	for _, path := range fingerprintChanges(kind, record.changes) {
		changes = append(changes, normalizeScopeToken(path, scope))
	}
	parts := append([]string{}, argv...)
	parts = append(parts, "@")
	if kind != kindCompile {
		parts = append(parts, normalizeScopeToken(record.cwd, scope))
	}
	parts = append(parts, "@")
	parts = append(parts, inputs...)
	parts = append(parts, "@")
	parts = append(parts, changes...)
	return strings.Join(parts, "\x1f")
}

func fingerprintInputToken(kind actionKind, path, origin string, scope trace.Scope, inputDigests map[string]string) string {
	token := normalizeScopeToken(path, scope)
	if !shouldHashFingerprintInput(kind, path, origin, scope) {
		return token
	}
	if sum, ok := inputDigests[origin]; ok && sum != "" {
		return token + "#" + sum
	}
	if origin == "" {
		return token + "#missing-origin"
	}
	// Evaluator runs after the traced build, so re-reading the filesystem here
	// would use post-build state instead of the action's point-in-time inputs.
	return token + "#missing-digest"
}

func shouldHashFingerprintInput(kind actionKind, path, origin string, scope trace.Scope) bool {
	if kind != kindCompile {
		return false
	}
	if origin == "" || scope.BuildRoot == "" {
		return false
	}
	origin = filepath.ToSlash(origin)
	buildRoot := filepath.ToSlash(scope.BuildRoot)
	if !strings.HasPrefix(origin, buildRoot+"/") && !strings.HasPrefix(normalizePath(path), normalizePath(scope.BuildRoot)+"/") {
		return false
	}
	return !isArtifactPath(origin)
}

func fingerprintChanges(kind actionKind, changes []string) []string {
	switch kind {
	case kindArchive:
		filtered := make([]string, 0, len(changes))
		for _, path := range changes {
			if isArchivePath(path) {
				filtered = append(filtered, path)
			}
		}
		if len(filtered) > 0 {
			return filtered
		}
	}
	return changes
}

func classifyActionKind(record normalizedRecord) actionKind {
	if len(record.argv) == 0 {
		return kindGeneric
	}

	tool := filepath.Base(record.argv[0])
	switch {
	case tool == "cp":
		if len(record.inputs) != 0 && len(record.changes) != 0 {
			return kindCopy
		}
		return kindGeneric
	case tool == "install":
		if len(record.changes) != 0 {
			return kindInstall
		}
		return kindGeneric
	case tool == "cmake" && len(record.argv) > 1 && record.argv[1] == "--install":
		if len(record.changes) != 0 {
			return kindInstall
		}
		return kindGeneric
	case tool == "cmake" && len(record.argv) > 1 && record.argv[1] == "-E":
		return kindGeneric
	case tool == "cmake" && len(record.argv) > 2 && record.argv[1] == "-E" && record.argv[2] == "cmake_symlink_library":
		return kindGeneric
	case tool == "cmake" || tool == "ninja" || tool == "make" || tool == "gmake":
		return kindConfigure
	case tool == "ar":
		if firstArchiveOutput(record.changes) != "" && slices.ContainsFunc(record.inputs, isArtifactPath) {
			return kindArchive
		}
		return kindGeneric
	case tool == "ranlib":
		if firstArchiveOutput(record.changes) != "" {
			return kindArchive
		}
		return kindGeneric
	case tool == "ld":
		if firstLinkOutput(record.changes) != "" && slices.ContainsFunc(record.inputs, isArtifactPath) {
			return kindLink
		}
		return kindGeneric
	case tool == "collect2":
		if firstLinkOutput(record.changes) != "" && slices.ContainsFunc(record.inputs, isArtifactPath) {
			return kindLink
		}
		return kindGeneric
	case tool == "cc1":
		if firstPathByExt(record.inputs, ".c", ".cc", ".cpp", ".cxx") != "" {
			return kindCompile
		}
	case tool == "cc1plus":
		if firstPathByExt(record.inputs, ".cc", ".cpp", ".cxx") != "" {
			return kindCompile
		}
	case tool == "as":
		if firstPathByExt(record.changes, ".o", ".obj") != "" {
			return kindCompile
		}
	}

	switch family := toolFamily(tool); family {
	case "cc", "cxx":
		if slices.Contains(record.argv, "-c") &&
			firstPathByExt(record.inputs, ".c", ".cc", ".cpp", ".cxx") != "" &&
			(firstPathByExt(record.changes, ".o", ".obj") != "" ||
				(len(record.changes) != 0 && declaredCompileObjectOutput(record.cwd, record.argv) != "")) {
			return kindCompile
		}
		if firstLinkOutput(record.changes) != "" &&
			(slices.ContainsFunc(record.inputs, isArtifactPath) || firstPathByExt(record.inputs, ".c", ".cc", ".cpp", ".cxx") != "") {
			return kindLink
		}
	}
	return kindGeneric
}

func buildActionKey(kind actionKind, tool string, record normalizedRecord, scope trace.Scope) string {
	parts := []string{
		kind.String(),
		toolFamily(tool),
		"cwd=" + normalizeScopeToken(record.cwd, scope),
	}
	switch kind {
	case kindCompile:
		src := firstPathByExt(record.inputs, ".c", ".cc", ".cpp", ".cxx")
		out := firstPathByExt(record.changes, ".o", ".obj")
		if src != "" {
			parts = append(parts, "src="+normalizeScopeToken(src, scope))
		}
		if out != "" {
			parts = append(parts, "out="+normalizeScopeToken(out, scope))
		}
	case kindArchive:
		if out := firstArchiveOutput(record.changes); out != "" {
			parts = append(parts, "out="+normalizeScopeToken(out, scope))
		}
	case kindLink:
		if out := firstLinkOutput(record.changes); out != "" {
			parts = append(parts, "out="+normalizeScopeToken(out, scope))
		} else {
			parts = append(parts, "argv="+normalizeScopeToken(argvSkeleton(record.argv), scope))
		}
	case kindCopy, kindInstall:
		for _, dst := range record.changes {
			if dst == "" {
				continue
			}
			parts = append(parts, "dst="+normalizeScopeToken(dst, scope))
			break
		}
	case kindConfigure:
		parts = append(parts, "argv="+normalizeScopeToken(argvSkeleton(record.argv), scope))
	default:
		parts = append(parts, "argv="+normalizeScopeToken(argvSkeleton(record.argv), scope))
	}
	return strings.Join(parts, "|")
}

func firstPathByExt(paths []string, exts ...string) string {
	for _, path := range paths {
		for _, ext := range exts {
			if strings.HasSuffix(path, ext) {
				return path
			}
		}
	}
	return ""
}

func firstLinkOutput(paths []string) string {
	for _, path := range paths {
		base := filepath.Base(path)
		if strings.HasSuffix(base, ".so") || strings.Contains(base, ".so.") || strings.HasSuffix(base, ".dylib") {
			return path
		}
		if ext := filepath.Ext(base); ext == "" {
			return path
		}
	}
	return ""
}

func declaredCompileObjectOutput(cwd string, argv []string) string {
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if arg == "-o" && i+1 < len(argv) {
			out := argv[i+1]
			if strings.HasSuffix(out, ".o") || strings.HasSuffix(out, ".obj") {
				return normalizeCompileOutputPath(cwd, out)
			}
			continue
		}
		if strings.HasPrefix(arg, "-o") && len(arg) > 2 {
			out := strings.TrimPrefix(arg, "-o")
			if strings.HasSuffix(out, ".o") || strings.HasSuffix(out, ".obj") {
				return normalizeCompileOutputPath(cwd, out)
			}
		}
	}
	return ""
}

func compileDriverObjectOutput(action actionNode) string {
	if out := declaredCompileObjectOutput(action.cwd, action.argv); out != "" {
		return out
	}
	src := actionPrimarySource(action)
	if src == "" || action.cwd == "" {
		return ""
	}
	base := filepath.Base(src)
	ext := filepath.Ext(base)
	if ext == "" {
		return ""
	}
	return normalizePath(filepath.Join(action.cwd, strings.TrimSuffix(base, ext)+".o"))
}

func argvSkeleton(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	limit := min(len(argv), 4)
	return strings.Join(argv[:limit], " ")
}

func toolFamily(tool string) string {
	switch tool {
	case "cc", "gcc", "clang":
		return "cc"
	case "c++", "g++", "clang++":
		return "cxx"
	case "cc1", "as":
		return "cc"
	case "cc1plus":
		return "cxx"
	case "ld", "collect2":
		return "ld"
	case "ar", "ranlib":
		return "ar"
	case "make", "gmake", "ninja", "cmake":
		return "build"
	default:
		return tool
	}
}

func (kind actionKind) String() string {
	switch kind {
	case kindCompile:
		return "compile"
	case kindLink:
		return "link"
	case kindArchive:
		return "archive"
	case kindCopy:
		return "copy"
	case kindInstall:
		return "install"
	case kindConfigure:
		return "configure"
	default:
		return "generic"
	}
}

func normalizeScope(scope trace.Scope) trace.Scope {
	out := scope
	out.SourceRoot = normalizeRootPath(scope.SourceRoot)
	out.BuildRoot = normalizeRootPath(scope.BuildRoot)
	out.InstallRoot = normalizeRootPath(scope.InstallRoot)
	out.KeepRoots = make([]string, 0, len(scope.KeepRoots))
	for _, root := range scope.KeepRoots {
		out.KeepRoots = append(out.KeepRoots, normalizeRootPath(root))
	}
	return out
}

func normalizeRootPath(path string) string {
	if path == "" {
		return ""
	}
	return normalizePath(filepath.Clean(path))
}

func isExplicitDeliveryPath(path string, scope trace.Scope) bool {
	if scope.InstallRoot == "" {
		return false
	}
	return path == scope.InstallRoot || strings.HasPrefix(path, scope.InstallRoot+"/")
}

func isDeliveryPath(actions []actionNode, outdeg []int, facts pathFacts) bool {
	if len(facts.readers) != 0 || len(facts.writers) == 0 {
		return false
	}
	for _, writer := range facts.writers {
		kind := actions[writer].kind
		if kind != kindCopy && kind != kindInstall {
			return false
		}
		if outdeg[writer] != 0 {
			return false
		}
	}
	return true
}

func isStagedDeliveryPath(actions []actionNode, tooling []bool, business []bool, facts pathFacts, scope trace.Scope) bool {
	if len(facts.readers) == 0 || len(facts.writers) == 0 {
		return false
	}
	hasDeliveryReader := false
	for _, writer := range facts.writers {
		if tooling[writer] || !business[writer] {
			return false
		}
	}
	for _, reader := range facts.readers {
		if tooling[reader] || !business[reader] {
			return false
		}
		explicitDelivery := false
		for _, path := range actions[reader].writes {
			if isExplicitDeliveryPath(path, scope) {
				explicitDelivery = true
				break
			}
		}
		if !explicitDelivery {
			return false
		}
		hasDeliveryReader = true
	}
	return hasDeliveryReader
}

func isBusinessPath(business []bool, facts pathFacts) bool {
	for _, reader := range facts.readers {
		if business[reader] {
			return true
		}
	}
	for _, writer := range facts.writers {
		if business[writer] {
			return true
		}
	}
	return false
}

func isToolingPath(actions []actionNode, tooling []bool, facts pathFacts, controlPlane map[string]struct{}, probeInputs map[string]struct{}) bool {
	if len(facts.writers) == 0 && len(facts.readers) == 0 {
		return false
	}
	if _, ok := controlPlane[facts.path]; !ok {
		if _, ok := probeInputs[facts.path]; !ok {
			return false
		}
	}
	for _, writer := range facts.writers {
		if !tooling[writer] && (actionConsumesBusinessData(actions[writer]) || actionProducesBusinessData(actions[writer])) {
			return false
		}
	}
	for _, reader := range facts.readers {
		if !tooling[reader] && (actionConsumesBusinessData(actions[reader]) || actionProducesBusinessData(actions[reader])) {
			return false
		}
	}
	return true
}

func classifyToolingActions(actions []actionNode, paths map[string]pathFacts) []bool {
	tooling := make([]bool, len(actions))
	for i, action := range actions {
		tooling[i] = action.kind == kindConfigure
	}
	for _, execFacts := range collectProducedExecutableFacts(actions, paths) {
		tooling[execFacts.executor] = true
		for _, writer := range execFacts.writers {
			tooling[writer] = true
		}
	}

	changed := true
	for changed {
		changed = false
		for i, action := range actions {
			if tooling[i] {
				continue
			}
			switch action.kind {
			case kindCompile:
				src := actionPrimarySource(action)
				if src == "" {
					continue
				}
				if facts, ok := paths[src]; ok && writersAllTooling(facts.writers, tooling) {
					tooling[i] = true
					changed = true
				}
			case kindLink, kindArchive:
				allTooling := true
				hasArtifactInput := false
				for _, input := range action.reads {
					if !isArtifactPath(input) {
						continue
					}
					hasArtifactInput = true
					facts, ok := paths[input]
					if !ok || !writersAllTooling(facts.writers, tooling) {
						allTooling = false
						break
					}
				}
				if hasArtifactInput && allTooling {
					tooling[i] = true
					changed = true
				}
			}
		}
	}

	return tooling
}

func classifyBusinessActions(actions []actionNode, in [][]graphEdge, outdeg []int, scope trace.Scope, seedTooling []bool) []bool {
	business := make([]bool, len(actions))
	stack := make([]int, 0, len(actions))
	for i, action := range actions {
		if actionWritesDelivery(action, outdeg[i], scope) {
			stack = append(stack, i)
		}
	}
	if len(stack) == 0 {
		for i, action := range actions {
			if actionSeedsLeafBusiness(action, outdeg[i], seedTooling[i]) {
				stack = append(stack, i)
			}
		}
	}
	for len(stack) > 0 {
		idx := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if business[idx] {
			continue
		}
		business[idx] = true
		for _, edge := range in[idx] {
			if seedTooling[edge.from] {
				continue
			}
			if !business[edge.from] {
				stack = append(stack, edge.from)
			}
		}
	}
	return business
}

func actionSeedsLeafBusiness(action actionNode, outdeg int, seedTooling bool) bool {
	if seedTooling || outdeg != 0 {
		return false
	}
	switch action.kind {
	case kindLink:
		return firstLinkOutput(action.writes) != ""
	case kindArchive:
		return firstArchiveOutput(action.writes) != ""
	case kindCompile:
		return firstPathByExt(action.writes, ".o", ".obj") != ""
	default:
		return false
	}
}

func actionWritesDelivery(action actionNode, outdeg int, scope trace.Scope) bool {
	if len(action.writes) == 0 {
		return false
	}
	for _, path := range action.writes {
		if isExplicitDeliveryPath(path, scope) {
			return true
		}
	}
	if action.kind != kindCopy && action.kind != kindInstall {
		return false
	}
	return outdeg == 0
}

func finalizeToolingActions(actions []actionNode, paths map[string]pathFacts, parentAction []int, in [][]graphEdge, outdeg []int, scope trace.Scope, seedTooling, probeSeed, blocked []bool, blockedPaths map[string]struct{}) []bool {
	// Tooling expansion starts from explicit seeds plus probe seeds, then keeps
	// recomputing business/control-plane boundaries until the set stabilizes.
	tooling := make([]bool, len(seedTooling))
	for i := range seedTooling {
		tooling[i] = seedTooling[i] || probeSeed[i]
	}
	for {
		business := classifyBusinessActions(actions, in, outdeg, scope, tooling)
		probeInputs := classifyProbeInputPaths(paths, actions, parentAction, business, tooling, blockedPaths)
		controlPlane := classifyControlPlanePaths(paths, actions, business, tooling, blockedPaths, probeInputs)
		next := slices.Clone(tooling)
		changed := false
		for i, action := range actions {
			if next[i] || business[i] || blocked[i] {
				continue
			}
			if !actionBelongsToProbeSubgraph(i, action, parentAction, paths, tooling, controlPlane, probeInputs, blockedPaths) {
				continue
			}
			next[i] = true
			changed = true
		}
		if !changed {
			return tooling
		}
		tooling = next
	}
}

func actionBelongsToProbeSubgraph(idx int, action actionNode, parentAction []int, paths map[string]pathFacts, tooling []bool, controlPlane map[string]struct{}, probeInputs map[string]struct{}, blocked map[string]struct{}) bool {
	// A probe action must be reachable from existing tooling evidence, either
	// through its launcher, through tooling-only consumers of its outputs, or
	// through running a tooling-produced executable.
	hasRelationEvidence := actionLaunchedByTooling(idx, parentAction, tooling) ||
		actionWritesConsumedByTooling(action, paths, tooling)
	if !hasRelationEvidence && action.execPath != "" {
		if facts, ok := paths[action.execPath]; ok {
			hasRelationEvidence = writersAllTooling(facts.writers, tooling)
		}
	}
	if !hasRelationEvidence {
		return false
	}
	return actionTouchesOnlyControlPlane(action, paths, controlPlane, probeInputs, blocked)
}

func classifyProbeSubgraphActions(actions []actionNode, paths map[string]pathFacts, parentAction []int, tooling, business []bool, controlPlane map[string]struct{}, probeInputs map[string]struct{}, blockedActions []bool, blockedPaths map[string]struct{}) []bool {
	probe := make([]bool, len(actions))
	for i, action := range actions {
		if tooling[i] || business[i] || blockedActions[i] {
			continue
		}
		if actionBelongsToProbeSubgraph(i, action, parentAction, paths, tooling, controlPlane, probeInputs, blockedPaths) {
			probe[i] = true
		}
	}
	return probe
}

func actionLaunchedByTooling(idx int, parentAction []int, tooling []bool) bool {
	if idx < 0 || idx >= len(parentAction) {
		return false
	}
	parent := parentAction[idx]
	if parent < 0 || parent >= len(tooling) {
		return false
	}
	return tooling[parent]
}

func actionTouchesOnlyControlPlane(action actionNode, paths map[string]pathFacts, controlPlane map[string]struct{}, probeInputs map[string]struct{}, blocked map[string]struct{}) bool {
	for _, path := range action.reads {
		if _, ok := blocked[path]; ok {
			return false
		}
		if _, ok := controlPlane[path]; ok {
			continue
		}
		if _, ok := probeInputs[path]; ok {
			continue
		}
		facts, ok := paths[path]
		if !ok || len(facts.writers) == 0 {
			if pathLooksLikeCompilationInput(path) {
				return false
			}
			continue
		}
		return false
	}
	for _, path := range action.writes {
		if _, ok := blocked[path]; ok {
			return false
		}
	}
	return true
}

func classifyControlPlanePaths(paths map[string]pathFacts, actions []actionNode, business []bool, tooling []bool, blocked map[string]struct{}, probeInputs map[string]struct{}) map[string]struct{} {
	controlPlane := make(map[string]struct{})
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		if _, ok := blocked[path]; ok {
			continue
		}
		if _, ok := probeInputs[path]; ok {
			continue
		}
		facts := paths[path]
		if pathTouchesBusinessData(actions, business, facts) {
			continue
		}
		touchesTooling := false
		for _, writer := range facts.writers {
			if tooling[writer] {
				touchesTooling = true
				break
			}
		}
		if !touchesTooling {
			for _, reader := range facts.readers {
				if tooling[reader] {
					touchesTooling = true
					break
				}
			}
		}
		if !touchesTooling {
			continue
		}
		if len(facts.writers) == 0 && pathLooksLikeCompilationInput(path) {
			continue
		}
		controlPlane[path] = struct{}{}
	}
	return controlPlane
}

func classifyProbeInputPaths(paths map[string]pathFacts, actions []actionNode, parentAction []int, business []bool, tooling []bool, blocked map[string]struct{}) map[string]struct{} {
	probeInputs := make(map[string]struct{})
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		if _, ok := blocked[path]; ok {
			continue
		}
		facts := paths[path]
		if len(facts.writers) != 0 || pathTouchesBusiness(business, facts) {
			continue
		}
		if !pathLooksLikeCompilationInput(path) {
			continue
		}
		hasToolingCompileReader := false
		allReadersEligible := true
		for _, reader := range facts.readers {
			if tooling[reader] {
				if actions[reader].kind == kindCompile {
					hasToolingCompileReader = true
				}
				continue
			}
			if actions[reader].kind != kindCompile {
				allReadersEligible = false
				break
			}
			if actionLaunchedByTooling(reader, parentAction, tooling) || actionWritesConsumedByTooling(actions[reader], paths, tooling) {
				hasToolingCompileReader = true
				continue
			}
			allReadersEligible = false
			break
		}
		if allReadersEligible && hasToolingCompileReader {
			probeInputs[path] = struct{}{}
		}
	}
	return probeInputs
}

func actionWritesConsumedByTooling(action actionNode, paths map[string]pathFacts, tooling []bool) bool {
	hasToolingReader := false
	for _, path := range action.writes {
		facts, ok := paths[path]
		if !ok || len(facts.readers) == 0 {
			continue
		}
		for _, reader := range facts.readers {
			if !tooling[reader] {
				return false
			}
			hasToolingReader = true
		}
	}
	return hasToolingReader
}

func pathTouchesBusinessData(actions []actionNode, business []bool, facts pathFacts) bool {
	for _, writer := range facts.writers {
		if business[writer] && actionProducesBusinessData(actions[writer]) {
			return true
		}
	}
	for _, reader := range facts.readers {
		if business[reader] && actionConsumesBusinessData(actions[reader]) {
			return true
		}
	}
	return false
}

func pathLooksLikeCompilationInput(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".c", ".cc", ".cpp", ".cxx", ".c++", ".m", ".mm", ".s", ".sx", ".asm", ".h", ".hh", ".hpp", ".hxx", ".ipp", ".inl", ".inc":
		return true
	default:
		return false
	}
}

func classifyToolingHardNegatives(actions []actionNode, paths map[string]pathFacts, outdeg []int, scope trace.Scope, business []bool) ([]bool, map[string]struct{}) {
	blockedPathSet := make(map[string]struct{}, len(paths))
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		if pathBlockedFromTooling(actions, scope, business, paths[path]) {
			blockedPathSet[path] = struct{}{}
		}
	}

	blockedActions := make([]bool, len(actions))
	for i, action := range actions {
		// Non-configure actions that only write already-blocked paths cannot seed
		// tooling again; we fold that check here instead of bouncing through a
		// one-off helper.
		writesOnlyBlockedPaths := action.kind != kindConfigure && len(action.writes) != 0
		if writesOnlyBlockedPaths {
			for _, path := range action.writes {
				if _, ok := blockedPathSet[path]; !ok {
					writesOnlyBlockedPaths = false
					break
				}
			}
		}
		switch {
		case actionWritesDelivery(action, outdeg[i], scope):
			blockedActions[i] = true
		case writesOnlyBlockedPaths:
			blockedActions[i] = true
		}
	}

	return blockedActions, blockedPathSet
}

func pathBlockedFromTooling(actions []actionNode, scope trace.Scope, business []bool, facts pathFacts) bool {
	if isExplicitDeliveryPath(facts.path, scope) {
		return true
	}
	for _, reader := range facts.readers {
		if !business[reader] {
			continue
		}
		if actionConsumesBusinessData(actions[reader]) {
			return true
		}
	}
	for _, writer := range facts.writers {
		if !business[writer] {
			continue
		}
		if actionProducesBusinessData(actions[writer]) {
			return true
		}
	}
	return false
}

func actionConsumesBusinessData(action actionNode) bool {
	switch action.kind {
	case kindCompile, kindLink, kindArchive, kindCopy, kindInstall:
		return true
	default:
		return false
	}
}

func actionProducesBusinessData(action actionNode) bool {
	switch action.kind {
	case kindCompile, kindLink, kindArchive, kindCopy, kindInstall:
		return true
	default:
		return false
	}
}

type producedExecutableFact struct {
	executor int
	writers  []int
}

func collectProducedExecutableFacts(actions []actionNode, paths map[string]pathFacts) []producedExecutableFact {
	facts := make([]producedExecutableFact, 0)
	for i, action := range actions {
		if action.execPath == "" {
			continue
		}
		pathFacts, ok := paths[action.execPath]
		if !ok || len(pathFacts.writers) == 0 {
			continue
		}
		facts = append(facts, producedExecutableFact{
			executor: i,
			writers:  pathFacts.writers,
		})
	}
	return facts
}

func writersAllTooling(writers []int, tooling []bool) bool {
	if len(writers) == 0 {
		return false
	}
	for _, writer := range writers {
		if !tooling[writer] {
			return false
		}
	}
	return true
}

func resolveExecPath(cwd string, argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	execPath := argv[0]
	if !strings.Contains(execPath, "/") {
		return ""
	}
	if !filepath.IsAbs(execPath) && cwd != "" {
		execPath = filepath.Join(cwd, execPath)
	}
	return normalizePath(execPath)
}

func actionPrimarySource(action actionNode) string {
	return firstPathByExt(action.reads, ".c", ".cc", ".cpp", ".cxx")
}

func compileObjectOutput(action actionNode) string {
	return firstPathByExt(action.writes, ".o", ".obj")
}

func normalizeCompileOutputPath(cwd, out string) string {
	if out == "" {
		return ""
	}
	if filepath.IsAbs(out) || cwd == "" {
		return normalizePath(out)
	}
	return normalizePath(filepath.Join(cwd, out))
}

func isArtifactPath(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, ".o") || strings.HasSuffix(base, ".obj") || isArchivePath(base) {
		return true
	}
	if strings.HasSuffix(base, ".so") || strings.Contains(base, ".so.") || strings.HasSuffix(base, ".dylib") {
		return true
	}
	return false
}

func isArchivePath(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".a") || strings.HasSuffix(base, ".lib")
}

func firstArchiveOutput(paths []string) string {
	for _, path := range paths {
		if isArchivePath(path) {
			return path
		}
	}
	return ""
}

func (role pathRole) String() string {
	switch role {
	case roleTooling:
		return "tooling"
	case rolePropagating:
		return "propagating"
	case roleDelivery:
		return "delivery"
	default:
		return "unknown"
	}
}
