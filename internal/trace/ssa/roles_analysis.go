package ssa

import (
	"path/filepath"
	"slices"
	"strings"
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
	nonEscapingToolingDefs := classifyNonEscapingToolingDefs(graph, toolingFamily, projection.ActionDeliveryOnly)
	for idx := range graph.Actions {
		if toolingFamily[idx] && !projection.ActionDeliveryOnly[idx] && !actionHasEscapingWrite(graph, nonEscapingToolingDefs, idx) {
			projection.ActionNoise[idx] = true
		}
		if !projection.ActionNoise[idx] {
			projection.ActionNoise[idx] = !projection.ActionDeliveryOnly[idx] &&
				!actionHasMainlineWrites(graph, toolingFamily, nonEscapingToolingDefs, idx) &&
				actionTouchesOnlyToolingPaths(graph, toolingFamily, nonEscapingToolingDefs, idx)
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
	for def := range classifyNonEscapingSiblingDefs(graph, projection.ActionNoise, projection.ActionDeliveryOnly) {
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
				} else if pathLooksToolingForFamily(graph, toolingFamily, def.Path) {
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

func classifyNonEscapingSiblingDefs(graph Graph, actionNoise, actionDeliveryOnly []bool) map[PathState]struct{} {
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
			escaping[i] = mainlineDefEscapes(graph, actionNoise, actionDeliveryOnly, def)
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

func mainlineDefEscapes(graph Graph, actionNoise, actionDeliveryOnly []bool, start PathState) bool {
	if isExplicitDeliveryPath(start.Path, graph.Scope) {
		return true
	}
	if defAnchorsMainline(graph, start.Path) {
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
			if actionProducesMainlineArtifacts(graph, reader) {
				return true
			}
			if idx := reader; idx >= len(actionNoise) || !actionNoise[idx] {
				if idx >= len(actionDeliveryOnly) || !actionDeliveryOnly[idx] {
					if actionConsumesMainlineData(graph.Actions[idx]) {
						return true
					}
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
	toolingFamily := make([]bool, len(graph.Actions))
	if len(graph.Actions) == 0 {
		return toolingFamily
	}
	children := make([][]int, len(graph.Actions))
	for idx, parent := range graph.ParentAction {
		if parent < 0 || parent >= len(graph.Actions) {
			continue
		}
		children[parent] = append(children[parent], idx)
	}
	queue := make([]int, 0, len(graph.Actions))
	for idx, action := range graph.Actions {
		if action.Kind != KindConfigure {
			continue
		}
		toolingFamily[idx] = true
		queue = append(queue, idx)
	}
	for len(queue) > 0 {
		idx := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, child := range children[idx] {
			if toolingFamily[child] {
				continue
			}
			toolingFamily[child] = true
			queue = append(queue, child)
		}
	}
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
			if pathLooksToolingForFamily(graph, toolingFamily, read.Path) {
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

func classifyNonEscapingToolingDefs(graph Graph, toolingFamily, deliveryOnly []bool) map[PathState]struct{} {
	out := make(map[PathState]struct{})
	for idx := range graph.Actions {
		if idx >= len(toolingFamily) || !toolingFamily[idx] || idx >= len(graph.ActionWrites) {
			continue
		}
		for _, def := range graph.ActionWrites[idx] {
			if toolingDefEscapes(graph, toolingFamily, deliveryOnly, def) {
				continue
			}
			out[def] = struct{}{}
		}
	}
	return out
}

func toolingDefEscapes(graph Graph, toolingFamily, deliveryOnly []bool, start PathState) bool {
	if defAnchorsMainline(graph, start.Path) {
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
			if actionProducesMainlineArtifacts(graph, reader) {
				return true
			}
			if reader >= len(toolingFamily) || !toolingFamily[reader] {
				if reader >= len(deliveryOnly) || !deliveryOnly[reader] {
					if actionConsumesMainlineData(graph.Actions[reader]) {
						return true
					}
				}
			}
			if reader < len(deliveryOnly) && deliveryOnly[reader] {
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

func actionProducesMainlineArtifacts(graph Graph, idx int) bool {
	if idx < 0 || idx >= len(graph.ActionWrites) {
		return false
	}
	for _, def := range graph.ActionWrites[idx] {
		if pathLooksDelivery(graph, def.Path) || isExplicitDeliveryPath(def.Path, graph.Scope) {
			continue
		}
		if pathLooksProbeArtifact(def.Path) {
			continue
		}
		if pathAnchorsMainline(graph, def.Path) {
			return true
		}
	}
	return false
}

func defAnchorsMainline(graph Graph, path string) bool {
	path = normalizePath(path)
	if path == "" || isExplicitDeliveryPath(path, graph.Scope) {
		return false
	}
	if pathLooksProbeArtifact(path) {
		return false
	}
	return pathAnchorsMainline(graph, path)
}

func pathAnchorsMainline(graph Graph, path string) bool {
	return pathLooksFinalArtifact(path) ||
		pathLooksLikeCompilationInput(path) ||
		pathLooksTopLevelBuildArtifact(graph, path)
}

func pathLooksFinalArtifact(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".a", ".so", ".dylib", ".dll", ".exe":
		return true
	default:
		return false
	}
}

func pathLooksTopLevelBuildArtifact(graph Graph, path string) bool {
	path = normalizePath(path)
	buildRoot := strings.TrimSuffix(normalizePath(graph.Scope.BuildRoot), "/")
	if path == "" || buildRoot == "" || path == buildRoot || !strings.HasPrefix(path, buildRoot+"/") {
		return false
	}
	rel := strings.TrimPrefix(path, buildRoot+"/")
	if rel == "" || strings.Contains(rel, "/") {
		return false
	}
	if filepath.Ext(rel) != "" {
		return false
	}
	switch rel {
	case "Makefile", "pkgRedirects", "Progress":
		return false
	}
	if strings.HasPrefix(rel, "cmTC_") || strings.HasPrefix(rel, "CMake") || strings.HasPrefix(rel, "cmake") {
		return false
	}
	return pathHasBuildRootConsumers(graph, path)
}

func pathHasBuildRootConsumers(graph Graph, path string) bool {
	facts, ok := graph.Paths[path]
	if ok && len(facts.Readers) != 0 {
		return true
	}
	for _, action := range graph.Actions {
		if normalizePath(action.ExecPath) == path {
			return true
		}
	}
	return false
}

func pathLooksProbeArtifact(path string) bool {
	path = normalizePath(path)
	if path == "" {
		return false
	}
	return reBuildTryCompileNoise.MatchString(path) ||
		reBuildCmTCNoise.MatchString(path) ||
		strings.Contains(path, "/CMakeTmp/")
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

func actionHasMainlineWrites(graph Graph, toolingFamily []bool, nonEscapingToolingDefs map[PathState]struct{}, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) || idx >= len(graph.ActionWrites) {
		return false
	}
	for _, def := range graph.ActionWrites[idx] {
		if defLooksTooling(graph, toolingFamily, nonEscapingToolingDefs, def) || pathLooksDelivery(graph, def.Path) {
			continue
		}
		return true
	}
	return false
}

func actionTouchesOnlyToolingPaths(graph Graph, toolingFamily []bool, nonEscapingToolingDefs map[PathState]struct{}, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	touched := false
	if idx < len(graph.ActionReads) {
		for _, read := range graph.ActionReads[idx] {
			if len(read.Defs) == 0 {
				if pathLooksToolingForFamily(graph, toolingFamily, read.Path) {
					touched = true
					continue
				}
				return false
			}
			for _, def := range read.Defs {
				if defLooksTooling(graph, toolingFamily, nonEscapingToolingDefs, def) {
					touched = true
					continue
				}
				return false
			}
		}
	}
	if idx < len(graph.ActionWrites) {
		for _, def := range graph.ActionWrites[idx] {
			if defLooksTooling(graph, toolingFamily, nonEscapingToolingDefs, def) {
				touched = true
			} else {
				return false
			}
		}
	}
	return touched
}

func defLooksTooling(graph Graph, toolingFamily []bool, nonEscapingToolingDefs map[PathState]struct{}, def PathState) bool {
	if _, ok := nonEscapingToolingDefs[def]; ok {
		return true
	}
	return pathLooksToolingForFamily(graph, toolingFamily, def.Path)
}

func pathLooksToolingForFamily(graph Graph, toolingFamily []bool, path string) bool {
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
	for _, writer := range facts.Writers {
		if writer < 0 || writer >= len(toolingFamily) || !toolingFamily[writer] {
			return false
		}
		sawEndpoint = true
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
	if pathAnchorsMainline(graph, path) || pathLooksLikeCompilationInput(path) || pathLooksProbeArtifact(path) {
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
