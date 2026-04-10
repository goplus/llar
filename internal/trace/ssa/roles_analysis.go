package ssa

import (
	"slices"
	"strings"

	"github.com/goplus/llar/internal/trace"
)

type roleProjection struct {
	ActionNoise        []bool
	ActionDeliveryOnly []bool
	DefNoise           map[PathState]struct{}
	ActionClass        []actionRole
	DefClass           map[PathState]defRole
}

type actionRole uint8

const (
	actionRoleMainline actionRole = iota
	actionRoleTooling
	actionRoleProbe
	actionRoleDelivery
)

type defRole uint8

const (
	defRoleMainline defRole = iota
	defRoleTooling
	defRoleProbe
	defRoleDelivery
)

func projectRoles(graph Graph) roleProjection {
	projection := roleProjection{
		ActionNoise:        make([]bool, len(graph.Actions)),
		ActionDeliveryOnly: make([]bool, len(graph.Actions)),
		DefNoise:           make(map[PathState]struct{}),
		ActionClass:        make([]actionRole, len(graph.Actions)),
		DefClass:           make(map[PathState]defRole),
	}
	for idx := range graph.Actions {
		projection.ActionDeliveryOnly[idx] = isDeliveryOnlyAction(graph, idx)
	}
	toolingFamily := classifyToolingFamily(graph)
	toolingFamily = expandToolingFamily(graph, toolingFamily, projection.ActionDeliveryOnly)
	toolingWorkspaceRoots := inferToolingWorkspaceRoots(graph, toolingFamily, projection.ActionDeliveryOnly)
	nonEscapingToolingDefs := classifyNonEscapingToolingDefs(graph, toolingFamily, projection.ActionDeliveryOnly, toolingWorkspaceRoots)
	mainlineVisibleDefs := inferMainlineVisibleDefs(graph, toolingFamily, toolingWorkspaceRoots, nonEscapingToolingDefs)
	for idx := range graph.Actions {
		if toolingFamily[idx] && !projection.ActionDeliveryOnly[idx] && !actionHasEscapingWrite(graph, nonEscapingToolingDefs, idx) {
			projection.ActionNoise[idx] = true
		}
		if !projection.ActionNoise[idx] {
			projection.ActionNoise[idx] = !projection.ActionDeliveryOnly[idx] &&
				!actionHasMainlineWrites(graph, toolingFamily, toolingWorkspaceRoots, nonEscapingToolingDefs, mainlineVisibleDefs, idx) &&
				actionTouchesOnlyToolingPaths(graph, toolingFamily, toolingWorkspaceRoots, nonEscapingToolingDefs, idx)
		}
		for _, def := range graph.ActionWrites[idx] {
			if projection.ActionDeliveryOnly[idx] || projection.ActionNoise[idx] {
				projection.DefNoise[def] = struct{}{}
				continue
			}
			if _, ok := nonEscapingToolingDefs[def]; ok {
				projection.DefNoise[def] = struct{}{}
				continue
			}
		}
	}
	for def := range classifyNonEscapingSiblingDefs(graph, mainlineVisibleDefs, projection.ActionNoise, projection.ActionDeliveryOnly) {
		projection.DefNoise[def] = struct{}{}
	}
	for idx := range graph.Actions {
		switch {
		case projection.ActionDeliveryOnly[idx]:
			projection.ActionClass[idx] = actionRoleDelivery
		case projection.ActionNoise[idx]:
			if idx < len(toolingFamily) && toolingFamily[idx] {
				projection.ActionClass[idx] = actionRoleTooling
			} else {
				projection.ActionClass[idx] = actionRoleProbe
			}
		default:
			projection.ActionClass[idx] = actionRoleMainline
		}
	}
	for idx, defs := range graph.ActionWrites {
		for _, def := range defs {
			class := defRoleMainline
			switch {
			case idx < len(projection.ActionDeliveryOnly) && projection.ActionDeliveryOnly[idx]:
				class = defRoleDelivery
			case hasProjectionDef(projection.DefNoise, def):
				if idx < len(toolingFamily) && toolingFamily[idx] {
					class = defRoleTooling
				} else if pathLooksToolingForFamily(graph, toolingFamily, toolingWorkspaceRoots, def.Path) {
					class = defRoleTooling
				} else {
					class = defRoleProbe
				}
			case pathLooksDelivery(graph, def.Path):
				class = defRoleDelivery
			}
			projection.DefClass[def] = class
		}
	}
	return projection
}

func classifyNonEscapingSiblingDefs(graph Graph, mainlineVisibleDefs map[PathState]struct{}, actionNoise, actionDeliveryOnly []bool) map[PathState]struct{} {
	out := make(map[PathState]struct{})
	for idx, defs := range graph.ActionWrites {
		if idx < len(actionNoise) && actionNoise[idx] {
			continue
		}
		if idx < len(actionDeliveryOnly) && actionDeliveryOnly[idx] {
			continue
		}
		if len(defs) < 2 {
			continue
		}
		escaping := make([]bool, len(defs))
		sawEscaping := false
		sawIsolated := false
		for i, def := range defs {
			escaping[i] = mainlineDefEscapes(graph, mainlineVisibleDefs, actionNoise, actionDeliveryOnly, def)
			if escaping[i] {
				sawEscaping = true
			} else {
				sawIsolated = true
			}
		}
		if !sawEscaping || !sawIsolated {
			continue
		}
		for i, def := range defs {
			if escaping[i] {
				continue
			}
			out[def] = struct{}{}
		}
	}
	return out
}

func inferMainlineVisibleDefs(graph Graph, toolingFamily []bool, toolingWorkspaceRoots map[string]struct{}, nonEscapingToolingDefs map[PathState]struct{}) map[PathState]struct{} {
	sinks := collectHardSinkDefs(graph, nonEscapingToolingDefs)
	if len(sinks) == 0 {
		sinks = collectDerivedSinkDefs(graph, toolingFamily, toolingWorkspaceRoots, nonEscapingToolingDefs)
	}
	if len(sinks) == 0 {
		return nil
	}
	order := newCausalOrder(graph)
	visible := make(map[PathState]struct{}, len(sinks))
	queue := make([]PathState, 0, len(sinks))
	for def := range sinks {
		visible[def] = struct{}{}
		queue = append(queue, def)
	}
	for len(queue) > 0 {
		def := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		writer := def.Writer
		if writer < 0 || writer >= len(graph.Actions) {
			continue
		}
		for _, read := range graph.ActionReads[writer] {
			for _, input := range read.Defs {
				if !defEligibleForMainlineClosure(graph, nonEscapingToolingDefs, input) {
					continue
				}
				if _, ok := visible[input]; ok {
					continue
				}
				visible[input] = struct{}{}
				queue = append(queue, input)
			}
		}
		for _, input := range actionExecPathDefs(graph, &order, writer) {
			if !defEligibleForMainlineClosure(graph, nonEscapingToolingDefs, input) {
				continue
			}
			if _, ok := visible[input]; ok {
				continue
			}
			visible[input] = struct{}{}
			queue = append(queue, input)
		}
	}
	return visible
}

func collectHardSinkDefs(graph Graph, nonEscapingToolingDefs map[PathState]struct{}) map[PathState]struct{} {
	out := make(map[PathState]struct{})
	for _, defs := range graph.ActionWrites {
		for _, def := range defs {
			if !isExplicitDeliveryPath(def.Path, graph.Scope) {
				continue
			}
			if !defEligibleForMainlineClosure(graph, nonEscapingToolingDefs, def) {
				continue
			}
			out[def] = struct{}{}
		}
	}
	return out
}

func collectDerivedSinkDefs(graph Graph, toolingFamily []bool, toolingWorkspaceRoots map[string]struct{}, nonEscapingToolingDefs map[PathState]struct{}) map[PathState]struct{} {
	out := make(map[PathState]struct{})
	residualDescendants := makeResidualDescendantWriteIndex(graph, nonEscapingToolingDefs)
	continues := make(map[PathState]bool)
	for _, defs := range graph.ActionWrites {
		for _, def := range defs {
			if !defEligibleForMainlineClosure(graph, nonEscapingToolingDefs, def) {
				continue
			}
			continues[def] = defHasResidualContinuation(graph, residualDescendants, nonEscapingToolingDefs, def)
		}
	}
	for idx, defs := range graph.ActionWrites {
		if idx < 0 || idx >= len(graph.Actions) {
			continue
		}
		if actionTouchesOnlyToolingPaths(graph, toolingFamily, toolingWorkspaceRoots, nonEscapingToolingDefs, idx) {
			continue
		}
		sawContinuingSibling := false
		for _, def := range defs {
			if continues[def] {
				sawContinuingSibling = true
				break
			}
		}
		for _, def := range defs {
			if !defEligibleForMainlineClosure(graph, nonEscapingToolingDefs, def) {
				continue
			}
			if continues[def] {
				continue
			}
			if sawContinuingSibling {
				continue
			}
			out[def] = struct{}{}
		}
	}
	return out
}

func defEligibleForMainlineClosure(graph Graph, nonEscapingToolingDefs map[PathState]struct{}, def PathState) bool {
	if def.Path == "" {
		return false
	}
	if _, ok := nonEscapingToolingDefs[def]; ok {
		return false
	}
	if def.Missing {
		return false
	}
	if !pathWithinObservedScope(def.Path, graph.Scope) {
		return false
	}
	if pathLooksDelivery(graph, def.Path) && !isExplicitDeliveryPath(def.Path, graph.Scope) {
		return false
	}
	return true
}

func makeResidualDescendantWriteIndex(graph Graph, nonEscapingToolingDefs map[PathState]struct{}) []bool {
	children := buildActionChildren(graph.ParentAction, len(graph.Actions))
	memo := make([]uint8, len(graph.Actions))
	var visit func(int) bool
	visit = func(idx int) bool {
		if idx < 0 || idx >= len(graph.Actions) {
			return false
		}
		switch memo[idx] {
		case 1:
			return false
		case 2:
			return true
		}
		if actionWritesResidualDef(graph, nonEscapingToolingDefs, idx) {
			memo[idx] = 2
			return true
		}
		memo[idx] = 1
		for _, child := range children[idx] {
			if visit(child) {
				memo[idx] = 2
				return true
			}
		}
		return false
	}
	out := make([]bool, len(graph.Actions))
	for idx := range graph.Actions {
		out[idx] = visit(idx)
	}
	return out
}

func buildActionChildren(parent []int, n int) [][]int {
	children := make([][]int, n)
	for idx, p := range parent {
		if p < 0 || p >= n {
			continue
		}
		children[p] = append(children[p], idx)
	}
	return children
}

func actionWritesResidualDef(graph Graph, nonEscapingToolingDefs map[PathState]struct{}, idx int) bool {
	if idx < 0 || idx >= len(graph.ActionWrites) {
		return false
	}
	for _, def := range graph.ActionWrites[idx] {
		if defEligibleForMainlineClosure(graph, nonEscapingToolingDefs, def) {
			return true
		}
	}
	return false
}

func defHasResidualContinuation(graph Graph, residualDescendants []bool, nonEscapingToolingDefs map[PathState]struct{}, def PathState) bool {
	for _, reader := range roleReadersForDef(graph, def) {
		if reader < 0 || reader >= len(graph.Actions) {
			continue
		}
		if actionHasContinuationWrite(graph, nonEscapingToolingDefs, reader) {
			return true
		}
		if reader < len(residualDescendants) && residualDescendants[reader] {
			return true
		}
		if actionReadsResidualExecPath(graph, nonEscapingToolingDefs, reader) {
			return true
		}
	}
	return false
}

func actionHasContinuationWrite(graph Graph, nonEscapingToolingDefs map[PathState]struct{}, idx int) bool {
	if idx < 0 || idx >= len(graph.ActionWrites) {
		return false
	}
	for _, def := range graph.ActionWrites[idx] {
		if _, ok := nonEscapingToolingDefs[def]; ok {
			continue
		}
		return true
	}
	return false
}

func actionReadsResidualExecPath(graph Graph, nonEscapingToolingDefs map[PathState]struct{}, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	path := normalizePath(graph.Actions[idx].ExecPath)
	if path == "" {
		return false
	}
	for _, def := range graph.DefsByPath[path] {
		if defEligibleForMainlineClosure(graph, nonEscapingToolingDefs, def) {
			return true
		}
	}
	return false
}

func actionExecPathDefs(graph Graph, order *causalOrder, idx int) []PathState {
	if idx < 0 || idx >= len(graph.Actions) {
		return nil
	}
	path := normalizePath(graph.Actions[idx].ExecPath)
	if path == "" || !pathWithinObservedScope(path, graph.Scope) {
		return nil
	}
	if defs := reachingDefsForRead(order, graph.DefsByPath[path], idx); len(defs) != 0 {
		return defs
	}
	if initial, ok := graph.InitialDefs[path]; ok {
		return []PathState{initial}
	}
	return nil
}

func defBelongsToMainlineVisibleClosure(mainlineVisibleDefs map[PathState]struct{}, def PathState) bool {
	_, ok := mainlineVisibleDefs[def]
	return ok
}

func hasMainlineVisibleClosure(mainlineVisibleDefs map[PathState]struct{}) bool {
	return len(mainlineVisibleDefs) != 0
}

func mainlineDefEscapes(graph Graph, mainlineVisibleDefs map[PathState]struct{}, actionNoise, actionDeliveryOnly []bool, start PathState) bool {
	if defBelongsToMainlineVisibleClosure(mainlineVisibleDefs, start) {
		return true
	}
	seenDefs := map[PathState]struct{}{start: {}}
	seenActions := make(map[int]struct{})
	queue := []PathState{start}
	for len(queue) > 0 {
		def := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, reader := range roleReadersForDef(graph, def) {
			if reader < 0 || reader >= len(graph.Actions) {
				continue
			}
			if idx := reader; idx >= len(actionNoise) || !actionNoise[idx] {
				if idx >= len(actionDeliveryOnly) || !actionDeliveryOnly[idx] {
					if actionConsumesMainlineData(graph.Actions[idx]) {
						return true
					}
				} else {
					// Delivery-only consumers are observable sinks even when the
					// install root is not explicitly scoped.
					return true
				}
			}
			if idx := reader; idx < len(actionDeliveryOnly) && actionDeliveryOnly[idx] {
				continue
			}
			if _, ok := seenActions[reader]; ok {
				continue
			}
			seenActions[reader] = struct{}{}
			if reader >= len(graph.ActionWrites) {
				continue
			}
			for _, next := range graph.ActionWrites[reader] {
				if defBelongsToMainlineVisibleClosure(mainlineVisibleDefs, next) {
					return true
				}
				if _, ok := seenDefs[next]; ok {
					continue
				}
				seenDefs[next] = struct{}{}
				queue = append(queue, next)
			}
		}
	}
	return false
}

func hasProjectionDef(defs map[PathState]struct{}, def PathState) bool {
	_, ok := defs[def]
	return ok
}

func classifyToolingFamily(graph Graph) []bool {
	toolingFamily := inferToolingSeedActions(graph, classifyToolingSeedHints(graph))
	for {
		changed := false
		for idx := range graph.Actions {
			if markProducedExecToolingFamily(graph, toolingFamily, idx) {
				changed = true
			}
			if !toolingFamily[idx] && actionCopiesToolingInputs(graph, toolingFamily, idx) {
				toolingFamily[idx] = true
				changed = true
				continue
			}
			if toolingFamily[idx] || !actionWritesConsumedOnlyByToolingFamily(graph, toolingFamily, idx) {
				continue
			}
			toolingFamily[idx] = true
			changed = true
		}
		if !changed {
			return toolingFamily
		}
	}
}

func classifyToolingSeedHints(graph Graph) []bool {
	hints := make([]bool, len(graph.Actions))
	for idx, action := range graph.Actions {
		if action.Kind == KindConfigure {
			hints[idx] = true
		}
	}
	return hints
}

func inferToolingSeedActions(graph Graph, hints []bool) []bool {
	seeds := make([]bool, len(graph.Actions))
	for {
		changed := false
		for idx := range graph.Actions {
			if idx < len(seeds) && seeds[idx] {
				continue
			}
			if !actionHasToolingSeedHint(graph, hints, seeds, idx) {
				continue
			}
			if !actionHasToolingSeedEvidence(graph, hints, seeds, idx) {
				continue
			}
			seeds[idx] = true
			changed = true
		}
		if !changed {
			return seeds
		}
	}
}

func actionHasToolingSeedHint(graph Graph, hints, seeds []bool, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	if idx < len(hints) && hints[idx] {
		return true
	}
	if actionLaunchedByToolingCandidate(graph.ParentAction, hints, seeds, idx) {
		return true
	}
	return actionExecPathHasToolingCandidateWriter(graph, hints, seeds, idx)
}

func actionHasToolingSeedEvidence(graph Graph, hints, seeds []bool, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	if actionExecPathHasToolingCandidateWriter(graph, hints, seeds, idx) {
		return true
	}
	if actionWritesConsumedOnlyByToolingCandidates(graph, hints, seeds, idx) {
		return true
	}
	if actionHasLocalToolingWorkspaceEvidence(graph, hints, seeds, idx) {
		return true
	}
	if idx < len(hints) && hints[idx] && (actionWritesObservedOutputs(graph, idx) || actionHasChild(graph.ParentAction, idx)) {
		return true
	}
	return false
}

func actionLaunchedByToolingCandidate(parentAction []int, hints, seeds []bool, idx int) bool {
	if idx < 0 || idx >= len(parentAction) {
		return false
	}
	parent := parentAction[idx]
	if parent < 0 {
		return false
	}
	if parent < len(seeds) && seeds[parent] {
		return true
	}
	return parent < len(hints) && hints[parent]
}

func actionExecPathHasToolingCandidateWriter(graph Graph, hints, seeds []bool, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	execPath := normalizePath(graph.Actions[idx].ExecPath)
	if execPath == "" {
		return false
	}
	facts, ok := graph.Paths[execPath]
	if !ok {
		return false
	}
	for _, writer := range facts.Writers {
		if writer < 0 {
			continue
		}
		if writer < len(seeds) && seeds[writer] {
			return true
		}
		if writer < len(hints) && hints[writer] {
			return true
		}
	}
	return false
}

func actionWritesConsumedOnlyByToolingCandidates(graph Graph, hints, seeds []bool, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	sawCandidateConsumer := false
	for _, path := range graph.Actions[idx].Writes {
		facts, ok := graph.Paths[path]
		if !ok {
			return false
		}
		for _, reader := range facts.Readers {
			if !actionIsToolingCandidate(hints, seeds, reader) {
				return false
			}
			sawCandidateConsumer = true
		}
		for consumer, action := range graph.Actions {
			if action.ExecPath != path {
				continue
			}
			if !actionIsToolingCandidate(hints, seeds, consumer) {
				return false
			}
			sawCandidateConsumer = true
		}
	}
	return sawCandidateConsumer
}

func actionHasLocalToolingWorkspaceEvidence(graph Graph, hints, seeds []bool, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	action := graph.Actions[idx]
	cwd := normalizePath(action.Cwd)
	scopedWorkspace := toolingWorkspaceRootCandidate(graph.Scope, cwd)
	unscopedWorkspace := !scopedWorkspace && scopeRootsEmpty(graph.Scope) && actionLaunchedByToolingCandidate(graph.ParentAction, hints, seeds, idx) && cwd != ""
	if !scopedWorkspace && !unscopedWorkspace {
		return false
	}
	hasLocalTouch := false
	for _, path := range action.Writes {
		path = normalizePath(path)
		if path == "" || !pathCountsForToolingEvidence(path, graph.Scope) {
			continue
		}
		if isExplicitDeliveryPath(path, graph.Scope) || !pathWithinDir(path, cwd) {
			return false
		}
		hasLocalTouch = true
	}
	if idx < len(graph.ActionReads) {
		for _, read := range graph.ActionReads[idx] {
			path := normalizePath(read.Path)
			if path == "" || !pathCountsForToolingEvidence(path, graph.Scope) || strings.HasPrefix(path, envNamespacePrefix) {
				continue
			}
			if pathWithinDir(path, cwd) {
				hasLocalTouch = true
				continue
			}
			if !readDefsAllToolingCandidates(read, hints, seeds) {
				if unscopedWorkspace && unscopedExternalToolingReadAllowed(graph, idx, path) {
					continue
				}
				return false
			}
		}
	}
	if hasLocalTouch {
		return true
	}
	return actionHasChildWithinDir(graph, idx, cwd)
}

func actionWritesObservedOutputs(graph Graph, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	for _, path := range graph.Actions[idx].Writes {
		if pathCountsForToolingEvidence(path, graph.Scope) {
			return true
		}
	}
	return false
}

func pathCountsForToolingEvidence(path string, scope trace.Scope) bool {
	path = normalizePath(path)
	if path == "" || strings.HasPrefix(path, envNamespacePrefix) {
		return false
	}
	if pathWithinObservedScope(path, scope) {
		return true
	}
	return scopeRootsEmpty(scope)
}

func scopeRootsEmpty(scope trace.Scope) bool {
	return scope.SourceRoot == "" && scope.BuildRoot == "" && scope.InstallRoot == "" && len(scope.KeepRoots) == 0
}

func actionHasChild(parentAction []int, idx int) bool {
	for _, parent := range parentAction {
		if parent == idx {
			return true
		}
	}
	return false
}

func actionHasChildWithinDir(graph Graph, idx int, dir string) bool {
	for child, parent := range graph.ParentAction {
		if parent != idx || child < 0 || child >= len(graph.Actions) {
			continue
		}
		if pathWithinDir(graph.Actions[child].Cwd, dir) {
			return true
		}
	}
	return false
}

func unscopedExternalToolingReadAllowed(graph Graph, idx int, path string) bool {
	if idx < 0 || idx >= len(graph.ParentAction) {
		return false
	}
	parent := graph.ParentAction[idx]
	if parent < 0 || parent >= len(graph.Actions) {
		return false
	}
	parentCwd := normalizePath(graph.Actions[parent].Cwd)
	if parentCwd == "" {
		return false
	}
	return !pathWithinDir(path, parentCwd)
}

func readDefsAllToolingCandidates(read Read, hints, seeds []bool) bool {
	if len(read.Defs) == 0 {
		return false
	}
	for _, def := range read.Defs {
		if !actionIsToolingCandidate(hints, seeds, def.Writer) {
			return false
		}
	}
	return true
}

func actionIsToolingCandidate(hints, seeds []bool, idx int) bool {
	if idx < 0 {
		return false
	}
	if idx < len(seeds) && seeds[idx] {
		return true
	}
	return idx < len(hints) && hints[idx]
}

func actionCopiesToolingInputs(graph Graph, toolingFamily []bool, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	action := graph.Actions[idx]
	if action.Kind != KindCopy || len(action.Reads) == 0 || len(action.Writes) == 0 {
		return false
	}
	for _, path := range action.Reads {
		facts, ok := graph.Paths[path]
		if !ok || !writersAllTooling(facts.Writers, toolingFamily) {
			return false
		}
	}
	return true
}

func writersAllTooling(writers []int, toolingFamily []bool) bool {
	if len(writers) == 0 {
		return false
	}
	for _, writer := range writers {
		if writer < 0 || writer >= len(toolingFamily) || !toolingFamily[writer] {
			return false
		}
	}
	return true
}

func inferToolingWorkspaceRoots(graph Graph, toolingFamily, deliveryOnly []bool) map[string]struct{} {
	roots := make(map[string]struct{})
	for idx := range graph.Actions {
		root := inferToolingWorkspaceRoot(graph, toolingFamily, deliveryOnly, idx)
		if root == "" {
			continue
		}
		roots[root] = struct{}{}
	}
	return roots
}

func inferToolingWorkspaceRoot(graph Graph, toolingFamily, deliveryOnly []bool, idx int) string {
	if idx < 0 || idx >= len(graph.Actions) || idx >= len(toolingFamily) || !toolingFamily[idx] {
		return ""
	}
	if idx < len(deliveryOnly) && deliveryOnly[idx] {
		return ""
	}
	action := graph.Actions[idx]
	cwd := normalizePath(action.Cwd)
	if !toolingWorkspaceRootCandidate(graph.Scope, cwd) {
		return ""
	}
	hasLocalWrite := false
	hasToolingProducedLocalPath := false
	for _, path := range action.Writes {
		path = normalizePath(path)
		if path == "" || !pathWithinObservedScope(path, graph.Scope) {
			continue
		}
		if isExplicitDeliveryPath(path, graph.Scope) || !pathWithinDir(path, cwd) {
			return ""
		}
		hasLocalWrite = true
		if pathHasToolingWriter(graph, toolingFamily, path) {
			hasToolingProducedLocalPath = true
		}
	}
	if !hasLocalWrite || !hasToolingProducedLocalPath {
		return ""
	}
	if idx < len(graph.ActionReads) {
		for _, read := range graph.ActionReads[idx] {
			path := normalizePath(read.Path)
			if path == "" || !pathWithinObservedScope(path, graph.Scope) || strings.HasPrefix(path, envNamespacePrefix) {
				continue
			}
			if pathWithinDir(path, cwd) {
				continue
			}
			if !readDefsAllToolingWriters(read, toolingFamily) {
				return ""
			}
		}
	}
	return cwd
}

func toolingWorkspaceRootCandidate(scope trace.Scope, cwd string) bool {
	cwd = normalizePath(cwd)
	if cwd == "" || !pathWithinObservedScope(cwd, scope) {
		return false
	}
	buildRoot := strings.TrimSuffix(normalizePath(scope.BuildRoot), "/")
	if buildRoot == "" || cwd == buildRoot || !strings.HasPrefix(cwd, buildRoot+"/") {
		return false
	}
	installRoot := strings.TrimSuffix(normalizePath(scope.InstallRoot), "/")
	if installRoot != "" && (cwd == installRoot || strings.HasPrefix(cwd, installRoot+"/")) {
		return false
	}
	return true
}

func pathWithinDir(path, dir string) bool {
	path = normalizePath(path)
	dir = strings.TrimSuffix(normalizePath(dir), "/")
	if path == "" || dir == "" {
		return false
	}
	return path == dir || strings.HasPrefix(path, dir+"/")
}

func pathBelongsToToolingWorkspace(workspaceRoots map[string]struct{}, path string) bool {
	path = normalizePath(path)
	if path == "" {
		return false
	}
	for root := range workspaceRoots {
		if pathWithinDir(path, root) {
			return true
		}
	}
	return false
}

func pathHasToolingWriter(graph Graph, toolingFamily []bool, path string) bool {
	facts, ok := graph.Paths[normalizePath(path)]
	if !ok {
		return false
	}
	for _, writer := range facts.Writers {
		if writer >= 0 && writer < len(toolingFamily) && toolingFamily[writer] {
			return true
		}
	}
	return false
}

func readDefsAllToolingWriters(read Read, toolingFamily []bool) bool {
	if len(read.Defs) == 0 {
		return false
	}
	for _, def := range read.Defs {
		if def.Writer < 0 || def.Writer >= len(toolingFamily) || !toolingFamily[def.Writer] {
			return false
		}
	}
	return true
}

func expandToolingFamily(graph Graph, toolingFamily, deliveryOnly []bool) []bool {
	expanded := slices.Clone(toolingFamily)
	for {
		changed := false
		toolingDefs := collectToolingFamilyDefs(graph, expanded)
		for idx := range graph.Actions {
			if idx >= len(expanded) || expanded[idx] {
				continue
			}
			if idx < len(deliveryOnly) && deliveryOnly[idx] {
				continue
			}
			if !actionConsumesOnlyToolingDefs(graph, toolingDefs, expanded, idx) &&
				!actionWritesConsumedOnlyByToolingFamily(graph, expanded, idx) &&
				!actionWritesToolingExecPath(graph, expanded, idx) {
				continue
			}
			expanded[idx] = true
			changed = true
		}
		if !changed {
			return expanded
		}
	}
}

func markProducedExecToolingFamily(graph Graph, toolingFamily []bool, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	execPath := normalizePath(graph.Actions[idx].ExecPath)
	if execPath == "" {
		return false
	}
	facts, ok := graph.Paths[execPath]
	if !ok || len(facts.Writers) == 0 {
		return false
	}
	changed := false
	if !toolingFamily[idx] {
		toolingFamily[idx] = true
		changed = true
	}
	for _, writer := range facts.Writers {
		if writer < 0 || writer >= len(toolingFamily) || toolingFamily[writer] {
			continue
		}
		toolingFamily[writer] = true
		changed = true
	}
	return changed
}

func actionWritesConsumedOnlyByToolingFamily(graph Graph, toolingFamily []bool, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	sawToolingConsumer := false
	for _, path := range graph.Actions[idx].Writes {
		facts, ok := graph.Paths[path]
		if !ok {
			return false
		}
		for _, reader := range facts.Readers {
			if reader < 0 || reader >= len(toolingFamily) || !toolingFamily[reader] {
				return false
			}
			sawToolingConsumer = true
		}
		for consumer, action := range graph.Actions {
			if consumer < 0 || consumer >= len(toolingFamily) || !toolingFamily[consumer] {
				continue
			}
			if action.ExecPath != path {
				continue
			}
			sawToolingConsumer = true
		}
	}
	return sawToolingConsumer
}

func actionWritesToolingExecPath(graph Graph, toolingFamily []bool, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	for _, path := range graph.Actions[idx].Writes {
		for consumer, action := range graph.Actions {
			if consumer < 0 || consumer >= len(toolingFamily) || !toolingFamily[consumer] {
				continue
			}
			if action.ExecPath == path {
				return true
			}
		}
	}
	return false
}

func collectToolingFamilyDefs(graph Graph, toolingFamily []bool) map[PathState]struct{} {
	out := make(map[PathState]struct{})
	for idx := range toolingFamily {
		if !toolingFamily[idx] || idx >= len(graph.ActionWrites) {
			continue
		}
		for _, def := range graph.ActionWrites[idx] {
			out[def] = struct{}{}
		}
	}
	return out
}

func actionConsumesOnlyToolingDefs(graph Graph, toolingDefs map[PathState]struct{}, toolingFamily []bool, idx int) bool {
	if idx < 0 || idx >= len(graph.ActionReads) {
		return false
	}
	sawToolingRead := false
	for _, read := range graph.ActionReads[idx] {
		if len(read.Defs) == 0 {
			if pathLooksToolingForFamily(graph, toolingFamily, nil, read.Path) {
				sawToolingRead = true
				continue
			}
			return false
		}
		for _, def := range read.Defs {
			if _, ok := toolingDefs[def]; ok {
				sawToolingRead = true
				continue
			}
			return false
		}
	}
	return sawToolingRead
}

func classifyNonEscapingToolingDefs(graph Graph, toolingFamily, deliveryOnly []bool, toolingWorkspaceRoots map[string]struct{}) map[PathState]struct{} {
	candidates := make(map[PathState]struct{})
	for idx := range graph.Actions {
		if idx >= len(toolingFamily) || !toolingFamily[idx] || idx >= len(graph.ActionWrites) {
			continue
		}
		for _, def := range graph.ActionWrites[idx] {
			candidates[def] = struct{}{}
		}
	}
	for {
		escaping := make([]PathState, 0)
		for def := range candidates {
			if toolingDefEscapes(graph, toolingFamily, deliveryOnly, toolingWorkspaceRoots, candidates, def) {
				escaping = append(escaping, def)
			}
		}
		if len(escaping) == 0 {
			return candidates
		}
		for _, def := range escaping {
			delete(candidates, def)
		}
	}
}

func toolingDefEscapes(graph Graph, toolingFamily, deliveryOnly []bool, toolingWorkspaceRoots map[string]struct{}, candidates map[PathState]struct{}, start PathState) bool {
	workspaceMember := pathBelongsToToolingWorkspace(toolingWorkspaceRoots, start.Path)
	if isExplicitDeliveryPath(start.Path, graph.Scope) {
		return true
	}
	if !workspaceMember && start.Writer >= 0 && actionCrossesMixedConsumerFrontier(graph, toolingFamily, candidates, toolingWorkspaceRoots, start.Writer) {
		return true
	}
	seenDefs := map[PathState]struct{}{start: {}}
	seenActions := make(map[int]struct{})
	queue := []PathState{start}
	for len(queue) > 0 {
		def := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, reader := range roleReadersForDef(graph, def) {
			if reader < 0 || reader >= len(graph.Actions) {
				continue
			}
			if reader >= len(toolingFamily) || !toolingFamily[reader] {
				return true
			}
			if !workspaceMember && actionCrossesMixedConsumerFrontier(graph, toolingFamily, candidates, toolingWorkspaceRoots, reader) {
				return true
			}
			if _, ok := seenActions[reader]; ok {
				continue
			}
			seenActions[reader] = struct{}{}
			if !workspaceMember && !actionWritesOnlyToolingCandidates(graph, candidates, reader) {
				return true
			}
			if reader >= len(graph.ActionWrites) {
				continue
			}
			for _, next := range graph.ActionWrites[reader] {
				if _, ok := candidates[next]; !ok {
					if workspaceMember {
						continue
					}
					return true
				}
				if _, ok := seenDefs[next]; ok {
					continue
				}
				seenDefs[next] = struct{}{}
				queue = append(queue, next)
			}
		}
	}
	return false
}

func actionCrossesMixedConsumerFrontier(graph Graph, toolingFamily []bool, candidates map[PathState]struct{}, toolingWorkspaceRoots map[string]struct{}, idx int) bool {
	if idx < 0 || idx >= len(graph.ActionReads) {
		return false
	}
	sawTooling := false
	sawResidual := false
	for _, read := range graph.ActionReads[idx] {
		if readBelongsToToolingCandidates(graph, toolingFamily, candidates, toolingWorkspaceRoots, read) {
			sawTooling = true
		} else if readCountsAsResidualFrontier(graph, read) {
			sawResidual = true
		}
		if sawTooling && sawResidual {
			return true
		}
	}
	return false
}

func readBelongsToToolingCandidates(graph Graph, toolingFamily []bool, candidates map[PathState]struct{}, toolingWorkspaceRoots map[string]struct{}, read Read) bool {
	if len(read.Defs) == 0 {
		return pathLooksToolingForFamily(graph, toolingFamily, toolingWorkspaceRoots, read.Path)
	}
	sawCandidate := false
	for _, def := range read.Defs {
		if _, ok := candidates[def]; !ok {
			return false
		}
		sawCandidate = true
	}
	return sawCandidate
}

func readCountsAsResidualFrontier(graph Graph, read Read) bool {
	path := normalizePath(read.Path)
	if path == "" {
		return false
	}
	if !pathWithinObservedScope(path, graph.Scope) {
		return false
	}
	if len(read.Defs) == 0 {
		return true
	}
	for _, def := range read.Defs {
		if def.Writer >= 0 || def.Missing {
			return true
		}
	}
	return true
}

func actionWritesOnlyToolingCandidates(graph Graph, candidates map[PathState]struct{}, idx int) bool {
	if idx < 0 || idx >= len(graph.ActionWrites) {
		return true
	}
	for _, def := range graph.ActionWrites[idx] {
		if isExplicitDeliveryPath(def.Path, graph.Scope) {
			return false
		}
		if _, ok := candidates[def]; !ok {
			return false
		}
	}
	return true
}

func actionHasEscapingWrite(graph Graph, nonEscapingToolingDefs map[PathState]struct{}, idx int) bool {
	if idx < 0 || idx >= len(graph.ActionWrites) {
		return false
	}
	for _, def := range graph.ActionWrites[idx] {
		if _, ok := nonEscapingToolingDefs[def]; ok {
			continue
		}
		return true
	}
	return false
}

func actionHasMainlineWrites(graph Graph, toolingFamily []bool, toolingWorkspaceRoots map[string]struct{}, nonEscapingToolingDefs, mainlineVisibleDefs map[PathState]struct{}, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) || idx >= len(graph.ActionWrites) {
		return false
	}
	closureAvailable := hasMainlineVisibleClosure(mainlineVisibleDefs)
	for _, def := range graph.ActionWrites[idx] {
		if defBelongsToMainlineVisibleClosure(mainlineVisibleDefs, def) {
			return true
		}
		if defLooksTooling(graph, toolingFamily, toolingWorkspaceRoots, nonEscapingToolingDefs, def) || pathLooksDelivery(graph, def.Path) {
			continue
		}
		if closureAvailable {
			continue
		}
		return true
	}
	return false
}

func actionTouchesOnlyToolingPaths(graph Graph, toolingFamily []bool, toolingWorkspaceRoots map[string]struct{}, nonEscapingToolingDefs map[PathState]struct{}, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	touched := false
	if idx < len(graph.ActionReads) {
		for _, read := range graph.ActionReads[idx] {
			if len(read.Defs) == 0 {
				if pathLooksToolingForFamily(graph, toolingFamily, toolingWorkspaceRoots, read.Path) {
					touched = true
					continue
				}
				return false
			}
			for _, def := range read.Defs {
				if defLooksTooling(graph, toolingFamily, toolingWorkspaceRoots, nonEscapingToolingDefs, def) {
					touched = true
					continue
				}
				return false
			}
		}
	}
	if idx < len(graph.ActionWrites) {
		for _, def := range graph.ActionWrites[idx] {
			if defLooksTooling(graph, toolingFamily, toolingWorkspaceRoots, nonEscapingToolingDefs, def) {
				touched = true
			} else {
				return false
			}
		}
	}
	return touched
}

func defLooksTooling(graph Graph, toolingFamily []bool, toolingWorkspaceRoots map[string]struct{}, nonEscapingToolingDefs map[PathState]struct{}, def PathState) bool {
	if _, ok := nonEscapingToolingDefs[def]; ok {
		return true
	}
	if def.Writer >= 0 {
		return false
	}
	return pathLooksToolingForFamily(graph, toolingFamily, toolingWorkspaceRoots, def.Path)
}

func pathLooksToolingForFamily(graph Graph, toolingFamily []bool, toolingWorkspaceRoots map[string]struct{}, path string) bool {
	path = normalizePath(path)
	if path == "" || pathLooksDelivery(graph, path) {
		return false
	}
	facts, ok := graph.RawPaths[path]
	if !ok {
		facts, ok = graph.Paths[path]
		if !ok {
			return false
		}
	}
	sawEndpoint := false
	sawProducer := false
	for _, writer := range facts.Writers {
		if writer < 0 || writer >= len(toolingFamily) || !toolingFamily[writer] {
			return false
		}
		sawEndpoint = true
		sawProducer = true
	}
	for _, reader := range facts.Readers {
		if reader < 0 || reader >= len(toolingFamily) || !toolingFamily[reader] {
			return false
		}
		sawEndpoint = true
	}
	for idx, action := range graph.Actions {
		if action.ExecPath != path {
			continue
		}
		if idx < 0 || idx >= len(toolingFamily) || !toolingFamily[idx] {
			return false
		}
		sawEndpoint = true
		sawProducer = true
	}
	if !sawProducer && !strings.HasPrefix(path, envNamespacePrefix) && !pathBelongsToToolingWorkspace(toolingWorkspaceRoots, path) {
		return false
	}
	return sawEndpoint
}

func pathLooksDelivery(graph Graph, path string) bool {
	path = normalizePath(path)
	if path == "" {
		return false
	}
	if isExplicitDeliveryPath(path, graph.Scope) {
		return true
	}
	facts, ok := graph.Paths[path]
	if !ok {
		return false
	}
	if pathConfinedToTransientWorkspace(graph, facts) {
		return false
	}
	executedPaths := collectExecPaths(graph.Actions)
	if isDeliveryPath(graph.Actions, graph.Outdeg, executedPaths, facts) {
		return true
	}
	if pathFeedsExplicitDelivery(graph, facts) {
		return true
	}
	return pathStaysInDeliveryPlane(graph, facts, executedPaths)
}

func pathStaysInDeliveryPlane(graph Graph, facts PathInfo, executedPaths map[string]struct{}) bool {
	path := normalizePath(facts.Path)
	if path == "" || isExplicitDeliveryPath(path, graph.Scope) {
		return false
	}
	if len(facts.Writers) == 0 {
		return false
	}
	if pathConfinedToTransientWorkspace(graph, facts) {
		return false
	}
	for _, writer := range facts.Writers {
		if !actionBelongsToDeliveryPlane(graph, writer, executedPaths) {
			return false
		}
	}
	for _, reader := range facts.Readers {
		if !actionBelongsToDeliveryPlane(graph, reader, executedPaths) {
			return false
		}
	}
	return true
}

func pathConfinedToTransientWorkspace(graph Graph, facts PathInfo) bool {
	path := normalizePath(facts.Path)
	if path == "" || !pathWithinObservedScope(path, graph.Scope) {
		return false
	}
	bestRoot := ""
	for _, idx := range slices.Concat(facts.Writers, facts.Readers) {
		root := actionTransientWorkspaceRoot(graph, idx, path)
		if root == "" {
			continue
		}
		if bestRoot == "" || len(root) > len(bestRoot) {
			bestRoot = root
		}
	}
	if bestRoot == "" {
		return false
	}
	for _, idx := range slices.Concat(facts.Writers, facts.Readers) {
		if !actionConfinedToWorkspace(graph, idx, bestRoot) {
			return false
		}
	}
	return true
}

func actionTransientWorkspaceRoot(graph Graph, idx int, path string) string {
	if idx < 0 || idx >= len(graph.Actions) {
		return ""
	}
	cwd := normalizePath(graph.Actions[idx].Cwd)
	if !toolingWorkspaceRootCandidate(graph.Scope, cwd) || !pathWithinDir(path, cwd) {
		return ""
	}
	if !actionConfinedToWorkspace(graph, idx, cwd) {
		return ""
	}
	return cwd
}

func actionConfinedToWorkspace(graph Graph, idx int, root string) bool {
	if idx < 0 || idx >= len(graph.Actions) || root == "" {
		return false
	}
	action := graph.Actions[idx]
	if cwd := normalizePath(action.Cwd); cwd != "" && pathWithinObservedScope(cwd, graph.Scope) && !pathWithinDir(cwd, root) {
		return false
	}
	sawObserved := false
	for _, path := range action.Reads {
		if !workspacePathAllowed(graph, path, root) {
			return false
		}
		if pathWithinObservedScope(path, graph.Scope) {
			sawObserved = true
		}
	}
	for _, path := range action.Writes {
		if !workspacePathAllowed(graph, path, root) {
			return false
		}
		if pathWithinObservedScope(path, graph.Scope) {
			sawObserved = true
		}
	}
	if execPath := normalizePath(action.ExecPath); execPath != "" && pathWithinObservedScope(execPath, graph.Scope) {
		if !pathWithinDir(execPath, root) {
			return false
		}
		sawObserved = true
	}
	return sawObserved
}

func workspacePathAllowed(graph Graph, path, root string) bool {
	path = normalizePath(path)
	if path == "" || strings.HasPrefix(path, envNamespacePrefix) {
		return true
	}
	if !pathWithinObservedScope(path, graph.Scope) {
		return true
	}
	return pathWithinDir(path, root)
}

func actionBelongsToDeliveryPlane(graph Graph, idx int, executedPaths map[string]struct{}) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	action := graph.Actions[idx]
	if actionWritesExecutedPath(action, executedPaths) {
		return false
	}
	switch action.Kind {
	case KindCopy, KindInstall:
		return true
	}
	if len(action.Writes) == 0 {
		return false
	}
	for _, path := range action.Writes {
		if !isExplicitDeliveryPath(path, graph.Scope) {
			return false
		}
	}
	return true
}

func pathFeedsExplicitDelivery(graph Graph, facts PathInfo) bool {
	if len(facts.Writers) == 0 || len(facts.Readers) == 0 {
		return false
	}
	for _, writer := range facts.Writers {
		if writer < 0 || writer >= len(graph.Actions) {
			return false
		}
		kind := graph.Actions[writer].Kind
		if kind != KindCopy && kind != KindInstall {
			return false
		}
	}
	sawExplicitDeliveryReader := false
	for _, reader := range facts.Readers {
		if reader < 0 || reader >= len(graph.Actions) {
			return false
		}
		explicitDelivery := false
		for _, out := range graph.Actions[reader].Writes {
			if isExplicitDeliveryPath(out, graph.Scope) {
				explicitDelivery = true
				break
			}
		}
		if !explicitDelivery {
			return false
		}
		sawExplicitDeliveryReader = true
	}
	return sawExplicitDeliveryReader
}

func roleReadersForDef(graph Graph, def PathState) []int {
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
