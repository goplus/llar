package evaluator

import (
	"maps"
	"slices"
	"strings"

	"github.com/goplus/llar/internal/trace"
)

type graphRoleProjection struct {
	tooling  []bool
	probe    []bool
	mainline []bool
	paths    map[string]pathFacts
}

func classifyGraphRoles(graph actionGraph) actionGraph {
	projection := deriveGraphRoleProjection(graph)
	graph.tooling = projection.tooling
	graph.probe = projection.probe
	graph.mainline = projection.mainline
	graph.paths = projection.paths
	return graph
}

func deriveGraphRoleProjection(graph actionGraph) graphRoleProjection {
	paths := clonePathFactsMap(graph.rawPaths)
	executedPaths := collectExecPaths(graph.actions)
	seedTooling := classifyToolingActions(graph.actions, paths)
	mainline := classifyMainlineActions(graph.actions, seedTooling)
	blockedActions, blockedPaths := classifyToolingHardNegatives(graph.actions, paths, graph.outdeg, executedPaths, graph.scope, mainline)
	for i, blocked := range blockedActions {
		if blocked {
			seedTooling[i] = false
		}
	}
	mainline = classifyMainlineActions(graph.actions, seedTooling)
	probeInputs := classifyProbeInputPaths(paths, graph.actions, graph.parentAction, seedTooling, blockedPaths)
	controlPlane := classifyControlPlanePaths(paths, graph.actions, graph.parentAction, mainline, seedTooling, blockedPaths, probeInputs)
	probeSeed := classifyProbeSubgraphActions(graph.actions, paths, graph.parentAction, seedTooling, controlPlane, probeInputs, blockedActions, blockedPaths)
	probeSeed = promoteProbeIslandActions(graph.actions, paths, graph.parentAction, graph.scope, seedTooling, probeSeed, blockedActions, blockedPaths)
	tooling := finalizeToolingActions(graph.actions, paths, graph.parentAction, graph.in, graph.outdeg, graph.scope, seedTooling, probeSeed, blockedActions, blockedPaths)
	mainline = classifyMainlineActions(graph.actions, tooling)
	probeInputs = classifyProbeInputPaths(paths, graph.actions, graph.parentAction, tooling, blockedPaths)
	controlPlane = classifyControlPlanePaths(paths, graph.actions, graph.parentAction, mainline, tooling, blockedPaths, probeInputs)
	probe := classifyProbeSubgraphActions(graph.actions, paths, graph.parentAction, tooling, controlPlane, probeInputs, blockedActions, blockedPaths)

	for path, facts := range paths {
		switch {
		case isExplicitDeliveryPath(path, graph.scope):
			facts.role = roleDelivery
		case isDeliveryPath(graph.actions, graph.outdeg, executedPaths, facts):
			facts.role = roleDelivery
		case isStagedDeliveryPath(graph.actions, tooling, mainline, facts, graph.scope):
			facts.role = roleDelivery
		case isMainlineSidecarPath(graph.actions, tooling, mainline, facts):
			facts.role = roleTooling
		case isToolingRelayPath(graph.actions, paths, tooling, facts):
			facts.role = roleTooling
		case isToolingPath(graph.actions, paths, tooling, facts, controlPlane, probeInputs):
			facts.role = roleTooling
		case isMainlinePath(mainline, facts):
			facts.role = rolePropagating
		default:
			facts.role = rolePropagating
		}
		paths[path] = facts
	}

	return graphRoleProjection{
		tooling:  tooling,
		probe:    probe,
		mainline: mainline,
		paths:    paths,
	}
}

func classifyToolingActions(actions []actionNode, paths map[string]pathFacts) []bool {
	tooling := make([]bool, len(actions))
	for i, action := range actions {
		tooling[i] = action.kind == kindConfigure
	}
	for {
		changed := false
		for i, action := range actions {
			if action.execPath != "" {
				facts, ok := paths[action.execPath]
				if ok && len(facts.writers) != 0 {
					if !tooling[i] {
						tooling[i] = true
						changed = true
					}
					for _, writer := range facts.writers {
						if writer < 0 || writer >= len(tooling) || tooling[writer] {
							continue
						}
						tooling[writer] = true
						changed = true
					}
				}
			}
			if !tooling[i] && actionCopiesToolingInputs(action, paths, tooling) {
				tooling[i] = true
				changed = true
				continue
			}
			if tooling[i] || !actionWritesConsumedByTooling(action, paths, tooling) {
				continue
			}
			tooling[i] = true
			changed = true
		}
		if !changed {
			return tooling
		}
	}
}

func actionCopiesToolingInputs(action actionNode, paths map[string]pathFacts, tooling []bool) bool {
	if action.kind != kindCopy || len(action.reads) == 0 || len(action.writes) == 0 {
		return false
	}
	for _, path := range action.reads {
		facts, ok := paths[path]
		if !ok || !writersAllTooling(facts.writers, tooling) {
			return false
		}
	}
	return true
}

func isToolingRelayPath(actions []actionNode, paths map[string]pathFacts, tooling []bool, facts pathFacts) bool {
	if !writersAllTooling(facts.writers, tooling) || len(facts.readers) == 0 {
		return false
	}
	for _, reader := range facts.readers {
		if reader < 0 || reader >= len(actions) {
			return false
		}
		if tooling[reader] || actionCopiesToolingInputs(actions[reader], paths, tooling) {
			continue
		}
		return false
	}
	return true
}

func classifyMainlineActions(actions []actionNode, tooling []bool) []bool {
	mainline := make([]bool, len(actions))
	for i := range actions {
		mainline[i] = i < len(tooling) && !tooling[i]
	}
	return mainline
}

func collectExecPaths(actions []actionNode) map[string]struct{} {
	executed := make(map[string]struct{})
	for _, action := range actions {
		if action.execPath == "" {
			continue
		}
		executed[action.execPath] = struct{}{}
	}
	return executed
}

func actionWritesExecutedPath(action actionNode, executedPaths map[string]struct{}) bool {
	if len(executedPaths) == 0 {
		return false
	}
	for _, path := range action.writes {
		if _, ok := executedPaths[path]; ok {
			return true
		}
	}
	return false
}

func actionWritesDelivery(action actionNode, outdeg int, executedPaths map[string]struct{}, scope trace.Scope) bool {
	for _, path := range action.writes {
		if isExplicitDeliveryPath(path, scope) {
			return true
		}
	}
	if action.kind != kindCopy && action.kind != kindInstall {
		return false
	}
	if len(action.writes) == 0 || outdeg != 0 {
		return false
	}
	return !actionWritesExecutedPath(action, executedPaths)
}

func isExplicitDeliveryPath(path string, scope trace.Scope) bool {
	root := strings.TrimSuffix(normalizePath(scope.InstallRoot), "/")
	if root == "" {
		return false
	}
	path = normalizePath(path)
	return path == root || strings.HasPrefix(path, root+"/")
}

func isDeliveryPath(actions []actionNode, outdeg []int, executedPaths map[string]struct{}, facts pathFacts) bool {
	for _, writer := range facts.writers {
		if writer < 0 || writer >= len(actions) {
			continue
		}
		action := actions[writer]
		if (action.kind == kindCopy || action.kind == kindInstall) && outdeg[writer] == 0 && !actionWritesExecutedPath(action, executedPaths) {
			return true
		}
	}
	return false
}

func isStagedDeliveryPath(actions []actionNode, tooling []bool, mainline []bool, facts pathFacts, scope trace.Scope) bool {
	for _, reader := range facts.readers {
		if reader < 0 || reader >= len(actions) || tooling[reader] || !mainline[reader] {
			continue
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
		return true
	}
	return false
}

func isMainlineSidecarPath(actions []actionNode, tooling []bool, mainline []bool, facts pathFacts) bool {
	if len(facts.writers) == 0 {
		return false
	}
	for _, writer := range facts.writers {
		if writer < 0 || writer >= len(actions) {
			return false
		}
		if writer >= len(mainline) || !mainline[writer] {
			return false
		}
		if writer < len(tooling) && tooling[writer] {
			return false
		}
		if len(actions[writer].writes) <= 1 {
			return false
		}
	}
	for _, reader := range facts.readers {
		if reader < 0 || reader >= len(actions) {
			continue
		}
		if reader >= len(mainline) || !mainline[reader] {
			continue
		}
		if actionConsumesMainlineData(actions[reader]) {
			return false
		}
	}
	return true
}

func isMainlinePath(mainline []bool, facts pathFacts) bool {
	for _, writer := range facts.writers {
		if writer >= 0 && writer < len(mainline) && mainline[writer] {
			return true
		}
	}
	for _, reader := range facts.readers {
		if reader >= 0 && reader < len(mainline) && mainline[reader] {
			return true
		}
	}
	return false
}

func actionConsumesMainlineData(action actionNode) bool {
	switch action.kind {
	case kindCopy, kindInstall, kindGeneric:
		return true
	default:
		return false
	}
}

func writersAllTooling(writers []int, tooling []bool) bool {
	if len(writers) == 0 {
		return false
	}
	for _, writer := range writers {
		if writer < 0 || writer >= len(tooling) || !tooling[writer] {
			return false
		}
	}
	return true
}

func actionWritesConsumedByTooling(action actionNode, paths map[string]pathFacts, tooling []bool) bool {
	hasToolingReader := false
	for _, path := range action.writes {
		facts, ok := paths[path]
		if !ok || len(facts.readers) == 0 {
			continue
		}
		for _, reader := range facts.readers {
			if reader < 0 || reader >= len(tooling) || !tooling[reader] {
				return false
			}
			hasToolingReader = true
		}
	}
	return hasToolingReader
}

func pathTouchesMainlineData(actions []actionNode, mainline []bool, facts pathFacts) bool {
	for _, writer := range facts.writers {
		if writer >= 0 && writer < len(mainline) && mainline[writer] && actionConsumesMainlineData(actions[writer]) {
			return true
		}
	}
	for _, reader := range facts.readers {
		if reader >= 0 && reader < len(mainline) && mainline[reader] && actionConsumesMainlineData(actions[reader]) {
			return true
		}
	}
	return false
}

func classifyToolingHardNegatives(actions []actionNode, paths map[string]pathFacts, outdeg []int, executedPaths map[string]struct{}, scope trace.Scope, mainline []bool) ([]bool, map[string]struct{}) {
	blockedPathSet := make(map[string]struct{}, len(paths))
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		if pathBlockedFromTooling(actions, scope, mainline, paths[path]) {
			blockedPathSet[path] = struct{}{}
		}
	}

	blockedActions := make([]bool, len(actions))
	for i, action := range actions {
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
		case actionWritesDelivery(action, outdeg[i], executedPaths, scope):
			blockedActions[i] = true
		case writesOnlyBlockedPaths:
			blockedActions[i] = true
		}
	}

	return blockedActions, blockedPathSet
}

func pathBlockedFromTooling(actions []actionNode, scope trace.Scope, mainline []bool, facts pathFacts) bool {
	return isExplicitDeliveryPath(facts.path, scope)
}

func classifyProbeInputPaths(paths map[string]pathFacts, actions []actionNode, parentAction []int, tooling []bool, blocked map[string]struct{}) map[string]struct{} {
	probeInputs := make(map[string]struct{})
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		if _, ok := blocked[path]; ok {
			continue
		}
		facts := paths[path]
		if len(facts.writers) != 0 {
			continue
		}
		if !pathLooksLikeCompilationInput(path) {
			continue
		}
		hasToolingReader := false
		allReadersEligible := true
		for _, reader := range facts.readers {
			if reader < 0 || reader >= len(actions) {
				allReadersEligible = false
				break
			}
			if tooling[reader] {
				hasToolingReader = true
				continue
			}
			if actionLaunchedByTooling(reader, parentAction, tooling) || actionWritesConsumedByTooling(actions[reader], paths, tooling) {
				hasToolingReader = true
				continue
			}
			allReadersEligible = false
			break
		}
		if allReadersEligible && hasToolingReader {
			probeInputs[path] = struct{}{}
		}
	}
	return probeInputs
}

func classifyControlPlanePaths(paths map[string]pathFacts, actions []actionNode, parentAction []int, mainline []bool, tooling []bool, blocked map[string]struct{}, probeInputs map[string]struct{}) map[string]struct{} {
	controlPlane := make(map[string]struct{})
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		if _, ok := blocked[path]; ok {
			continue
		}
		if _, ok := probeInputs[path]; ok {
			continue
		}
		facts := paths[path]
		if pathTouchesStrictMainlineData(actions, parentAction, mainline, tooling, paths, facts) {
			continue
		}
		touchesTooling := false
		for _, writer := range facts.writers {
			if writer >= 0 && writer < len(tooling) && tooling[writer] {
				touchesTooling = true
				break
			}
		}
		if !touchesTooling {
			for _, reader := range facts.readers {
				if reader >= 0 && reader < len(tooling) && tooling[reader] {
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

func pathTouchesStrictMainlineData(actions []actionNode, parentAction []int, mainline []bool, tooling []bool, paths map[string]pathFacts, facts pathFacts) bool {
	for _, writer := range facts.writers {
		if writer >= 0 && writer < len(mainline) && mainline[writer] && actionConsumesMainlineData(actions[writer]) && !actionHasProbeRelationEvidence(writer, actions[writer], parentAction, paths, tooling) {
			return true
		}
	}
	for _, reader := range facts.readers {
		if reader >= 0 && reader < len(mainline) && mainline[reader] && actionConsumesMainlineData(actions[reader]) && !actionHasProbeRelationEvidence(reader, actions[reader], parentAction, paths, tooling) {
			return true
		}
	}
	return false
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

func actionHasProbeRelationEvidence(idx int, action actionNode, parentAction []int, paths map[string]pathFacts, tooling []bool) bool {
	if actionLaunchedByTooling(idx, parentAction, tooling) || actionWritesConsumedByTooling(action, paths, tooling) {
		return true
	}
	if action.execPath == "" {
		return false
	}
	facts, ok := paths[action.execPath]
	return ok && writersAllTooling(facts.writers, tooling)
}

func actionBelongsToProbeSubgraph(idx int, action actionNode, parentAction []int, paths map[string]pathFacts, tooling []bool, controlPlane map[string]struct{}, probeInputs map[string]struct{}, blocked map[string]struct{}) bool {
	if !actionHasProbeRelationEvidence(idx, action, parentAction, paths, tooling) {
		return false
	}
	return actionTouchesOnlyControlPlane(action, paths, controlPlane, probeInputs, blocked)
}

func promoteProbeIslandActions(actions []actionNode, paths map[string]pathFacts, parentAction []int, scope trace.Scope, seedTooling, probeSeed, blockedActions []bool, blockedPaths map[string]struct{}) []bool {
	probe := slices.Clone(probeSeed)
	selected := make([]bool, len(seedTooling))
	for i := range selected {
		selected[i] = seedTooling[i] || probe[i]
	}
	for {
		mainline := classifyMainlineActions(actions, selected)
		probeInputs := classifyProbeInputPaths(paths, actions, parentAction, selected, blockedPaths)
		controlPlane := classifyControlPlanePaths(paths, actions, parentAction, mainline, selected, blockedPaths, probeInputs)
		changed := false
		for i, action := range actions {
			if i >= len(actions) || selected[i] || blockedActions[i] {
				continue
			}
			if !actionBelongsToProbeIsland(i, action, parentAction, paths, selected, controlPlane, probeInputs, blockedPaths, scope) {
				continue
			}
			selected[i] = true
			probe[i] = true
			changed = true
		}
		if !changed {
			return probe
		}
	}
}

func actionBelongsToProbeIsland(idx int, action actionNode, parentAction []int, paths map[string]pathFacts, selected []bool, controlPlane map[string]struct{}, probeInputs map[string]struct{}, blocked map[string]struct{}, scope trace.Scope) bool {
	if !actionHasProbeRelationEvidence(idx, action, parentAction, paths, selected) &&
		!actionRunsWithinProbeSubtree(action, controlPlane, probeInputs, scope) {
		return false
	}
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
			if pathLooksLikeCompilationInput(path) && pathWithinObservedScope(path, scope) {
				return false
			}
			continue
		}
		if !writersAllTooling(facts.writers, selected) {
			return false
		}
	}
	for _, path := range action.writes {
		if _, ok := blocked[path]; ok {
			return false
		}
		if isExplicitDeliveryPath(path, scope) {
			return false
		}
	}
	return true
}

func actionRunsWithinProbeSubtree(action actionNode, controlPlane map[string]struct{}, probeInputs map[string]struct{}, scope trace.Scope) bool {
	cwd := normalizePath(action.cwd)
	if cwd == "" || !pathWithinObservedScope(cwd, scope) {
		return false
	}
	for _, root := range []string{
		normalizePath(scope.SourceRoot),
		normalizePath(scope.BuildRoot),
		normalizePath(scope.InstallRoot),
	} {
		if root != "" && cwd == root {
			return false
		}
	}
	if pathSetContainsChild(controlPlane, cwd) {
		return true
	}
	return pathSetContainsChild(probeInputs, cwd)
}

func pathSetContainsChild(paths map[string]struct{}, dir string) bool {
	if len(paths) == 0 || dir == "" {
		return false
	}
	prefix := dir + "/"
	for path := range paths {
		path = normalizePath(path)
		if path == "" || path == dir {
			continue
		}
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func pathWithinObservedScope(path string, scope trace.Scope) bool {
	path = normalizePath(path)
	if path == "" {
		return false
	}
	roots := make([]string, 0, 3+len(scope.KeepRoots))
	if scope.SourceRoot != "" {
		roots = append(roots, normalizePath(scope.SourceRoot))
	}
	if scope.BuildRoot != "" {
		roots = append(roots, normalizePath(scope.BuildRoot))
	}
	if scope.InstallRoot != "" {
		roots = append(roots, normalizePath(scope.InstallRoot))
	}
	for _, root := range scope.KeepRoots {
		if root = normalizePath(root); root != "" {
			roots = append(roots, root)
		}
	}
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

func classifyProbeSubgraphActions(actions []actionNode, paths map[string]pathFacts, parentAction []int, tooling []bool, controlPlane map[string]struct{}, probeInputs map[string]struct{}, blockedActions []bool, blockedPaths map[string]struct{}) []bool {
	probe := make([]bool, len(actions))
	for i, action := range actions {
		if i >= len(actions) || tooling[i] || blockedActions[i] {
			continue
		}
		if actionBelongsToProbeSubgraph(i, action, parentAction, paths, tooling, controlPlane, probeInputs, blockedPaths) {
			probe[i] = true
		}
	}
	return probe
}

func finalizeToolingActions(actions []actionNode, paths map[string]pathFacts, parentAction []int, in [][]graphEdge, outdeg []int, scope trace.Scope, seedTooling, probeSeed, blocked []bool, blockedPaths map[string]struct{}) []bool {
	tooling := make([]bool, len(seedTooling))
	for i := range seedTooling {
		tooling[i] = seedTooling[i] || probeSeed[i]
	}
	for {
		mainline := classifyMainlineActions(actions, tooling)
		probeInputs := classifyProbeInputPaths(paths, actions, parentAction, tooling, blockedPaths)
		controlPlane := classifyControlPlanePaths(paths, actions, parentAction, mainline, tooling, blockedPaths, probeInputs)
		next := slices.Clone(tooling)
		changed := false
		for i, action := range actions {
			if next[i] || blocked[i] {
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

func isToolingPath(actions []actionNode, paths map[string]pathFacts, tooling []bool, facts pathFacts, controlPlane map[string]struct{}, probeInputs map[string]struct{}) bool {
	if len(facts.writers) == 0 && len(facts.readers) == 0 {
		return false
	}
	if _, ok := controlPlane[facts.path]; !ok {
		if _, ok := probeInputs[facts.path]; !ok {
			return false
		}
	}
	for _, writer := range facts.writers {
		if writer >= 0 && writer < len(actions) && !tooling[writer] && actionConsumesMainlineData(actions[writer]) && !actionCopiesToolingInputs(actions[writer], paths, tooling) {
			return false
		}
	}
	for _, reader := range facts.readers {
		if reader >= 0 && reader < len(actions) && !tooling[reader] && actionConsumesMainlineData(actions[reader]) && !actionCopiesToolingInputs(actions[reader], paths, tooling) {
			return false
		}
	}
	return true
}
