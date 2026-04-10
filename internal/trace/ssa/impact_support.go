package ssa

import (
	"slices"
	"sort"
	"strings"

	"github.com/goplus/llar/internal/trace"
)

func isDeliveryOnlyAction(graph Graph, idx int) bool {
	if idx < 0 || idx >= len(graph.Actions) {
		return false
	}
	action := graph.Actions[idx]
	if len(action.Writes) == 0 {
		return false
	}
	explicitDeliveryOnly := true
	for _, changed := range action.Writes {
		if !pathLooksDelivery(graph, changed) {
			return false
		}
		if !isExplicitDeliveryPath(changed, graph.Scope) {
			explicitDeliveryOnly = false
		}
	}
	if action.Kind == KindCopy || action.Kind == KindInstall {
		return true
	}
	return explicitDeliveryOnly
}

func collectExecPaths(actions []ExecNode) map[string]struct{} {
	executed := make(map[string]struct{})
	for _, action := range actions {
		if action.ExecPath == "" {
			continue
		}
		executed[action.ExecPath] = struct{}{}
	}
	return executed
}

func actionWritesExecutedPath(action ExecNode, executedPaths map[string]struct{}) bool {
	if len(executedPaths) == 0 {
		return false
	}
	for _, path := range action.Writes {
		if _, ok := executedPaths[path]; ok {
			return true
		}
	}
	return false
}

func isDeliveryPath(actions []ExecNode, outdeg []int, executedPaths map[string]struct{}, facts PathInfo) bool {
	for _, writer := range facts.Writers {
		if writer < 0 || writer >= len(actions) {
			continue
		}
		action := actions[writer]
		if (action.Kind == KindCopy || action.Kind == KindInstall) && outdeg[writer] == 0 && !actionWritesExecutedPath(action, executedPaths) {
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
	if strings.HasPrefix(path, envNamespacePrefix) {
		return true
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

func roleActionNoise(roles roleProjection, idx int) bool {
	return idx >= 0 && idx < len(roles.ActionNoise) && roles.ActionNoise[idx]
}

func roleActionDeliveryOnly(roles roleProjection, idx int) bool {
	return idx >= 0 && idx < len(roles.ActionDeliveryOnly) && roles.ActionDeliveryOnly[idx]
}

func roleActionClass(roles roleProjection, idx int) actionRole {
	if idx >= 0 && idx < len(roles.ActionClass) {
		return roles.ActionClass[idx]
	}
	if roleActionDeliveryOnly(roles, idx) {
		return actionRoleDelivery
	}
	if roleActionNoise(roles, idx) {
		return actionRoleProbe
	}
	return actionRoleMainline
}

func roleDefClass(roles roleProjection, def PathState) defRole {
	if class, ok := roles.DefClass[def]; ok {
		return class
	}
	if _, noise := roles.DefNoise[def]; noise {
		return defRoleProbe
	}
	return defRoleMainline
}

func visibleBindingDefs(defs []PathState, roles roleProjection) []PathState {
	out := make([]PathState, 0, len(defs))
	for _, def := range defs {
		if _, noise := roles.DefNoise[def]; noise {
			continue
		}
		out = append(out, def)
	}
	return out
}

func impactTrackedPathAllowed(graph Graph, roles roleProjection, path string) bool {
	if !impactPathAllowed(graph, roles, path) {
		return false
	}
	if graph.Scope.SourceRoot == "" && graph.Scope.BuildRoot == "" && graph.Scope.InstallRoot == "" && len(graph.Scope.KeepRoots) == 0 {
		return true
	}
	return pathWithinObservedScope(path, graph.Scope)
}

func impactPathAllowed(graph Graph, roles roleProjection, path string) bool {
	path = normalizePath(path)
	if path == "" {
		return false
	}
	if _, ok := graph.Paths[path]; !ok {
		return false
	}
	if pathLooksDelivery(graph, path) && !isExplicitDeliveryPath(path, graph.Scope) {
		return false
	}
	if pathLooksConfigureSidecarProjected(graph, roles, path) {
		return false
	}
	visibleDef := false
	for _, def := range graph.DefsByPath[path] {
		if _, noise := roles.DefNoise[def]; noise {
			continue
		}
		visibleDef = true
		break
	}
	if visibleDef {
		return true
	}
	if len(graph.DefsByPath[path]) != 0 {
		return false
	}
	if isProbeOnlyNoisePathProjected(graph, roles, path) {
		return false
	}
	if pathLooksToolingProjected(graph, roles, path) {
		return false
	}
	return true
}

func isProbeOnlyNoisePathProjected(graph Graph, roles roleProjection, path string) bool {
	if path == "" || isExplicitDeliveryPath(path, graph.Scope) {
		return false
	}
	facts, ok := graph.Paths[path]
	if !ok {
		return false
	}
	if len(graph.DefsByPath[path]) != 0 {
		return false
	}
	sawEndpoint := false
	for _, idx := range facts.Writers {
		class := roleActionClass(roles, idx)
		if class != actionRoleTooling && class != actionRoleProbe {
			return false
		}
		sawEndpoint = true
	}
	for _, idx := range facts.Readers {
		class := roleActionClass(roles, idx)
		if class != actionRoleTooling && class != actionRoleProbe {
			return false
		}
		sawEndpoint = true
	}
	return sawEndpoint
}

func pathLooksToolingProjected(graph Graph, roles roleProjection, path string) bool {
	path = normalizePath(path)
	if path == "" || len(graph.DefsByPath[path]) != 0 {
		return false
	}
	facts, ok := graph.Paths[path]
	if !ok {
		return false
	}
	sawEndpoint := false
	for _, idx := range facts.Writers {
		class := roleActionClass(roles, idx)
		if class != actionRoleTooling && class != actionRoleProbe {
			return false
		}
		sawEndpoint = true
	}
	for _, idx := range facts.Readers {
		class := roleActionClass(roles, idx)
		if class != actionRoleTooling && class != actionRoleProbe {
			return false
		}
		sawEndpoint = true
	}
	return sawEndpoint
}

func pathLooksConfigureSidecarProjected(graph Graph, roles roleProjection, path string) bool {
	path = normalizePath(path)
	if path == "" || isExplicitDeliveryPath(path, graph.Scope) {
		return false
	}
	facts, ok := graph.Paths[path]
	if !ok || len(facts.Writers) == 0 {
		return false
	}
	for _, writer := range facts.Writers {
		if writer < 0 || writer >= len(graph.Actions) {
			return false
		}
		if roleActionClass(roles, writer) != actionRoleMainline {
			return false
		}
		if graph.Actions[writer].Kind != KindConfigure {
			return false
		}
		if len(graph.Actions[writer].Writes) <= 1 {
			return false
		}
	}
	for _, reader := range facts.Readers {
		if reader < 0 || reader >= len(graph.Actions) {
			continue
		}
		switch roleActionClass(roles, reader) {
		case actionRoleTooling, actionRoleProbe, actionRoleDelivery:
			continue
		case actionRoleMainline:
			if actionConsumesMainlineData(graph.Actions[reader]) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func actionConsumesMainlineData(action ExecNode) bool {
	switch action.Kind {
	case KindCopy, KindInstall, KindGeneric:
		return true
	default:
		return false
	}
}

func actionReadAmbiguousVisible(graph Graph, roles roleProjection, idx int) bool {
	if idx < 0 || idx >= len(graph.ActionReads) {
		return false
	}
	for _, read := range graph.ActionReads[idx] {
		if !impactTrackedPathAllowed(graph, roles, read.Path) {
			continue
		}
		if len(visibleBindingDefs(read.Defs, roles)) > 1 {
			return true
		}
	}
	return false
}

func actionDependsOnDivergedRead(graph Graph, roles roleProjection, diverged map[PathState]struct{}, idx int) bool {
	if idx < 0 || idx >= len(graph.ActionReads) {
		return false
	}
	for _, read := range graph.ActionReads[idx] {
		if !impactTrackedPathAllowed(graph, roles, read.Path) {
			continue
		}
		for _, def := range read.Defs {
			if _, noise := roles.DefNoise[def]; noise {
				continue
			}
			if _, ok := diverged[def]; ok {
				return true
			}
		}
	}
	return false
}

func canonicalActionWriteSet(graph Graph, roles roleProjection, idx int) []string {
	if idx < 0 || idx >= len(graph.Actions) {
		return nil
	}
	out := make([]string, 0, len(graph.Actions[idx].Writes))
	for _, path := range graph.Actions[idx].Writes {
		if !impactPathAllowed(graph, roles, path) {
			continue
		}
		out = append(out, canonicalImpactPath(graph, path))
	}
	sort.Strings(out)
	return out
}

func wavefrontActionEquivalentWithChanged(base, probe Graph, baseRoles, probeRoles roleProjection, baseIdx, probeIdx int, changed map[string]bool) bool {
	if baseIdx < 0 || baseIdx >= len(base.Actions) || probeIdx < 0 || probeIdx >= len(probe.Actions) {
		return false
	}
	if intrinsicBehaviorSignature(base.Actions[baseIdx]) != intrinsicBehaviorSignature(probe.Actions[probeIdx]) {
		return false
	}
	return actionOutputsEquivalent(base, probe, baseRoles, probeRoles, baseIdx, probeIdx, changed)
}

func intrinsicBehaviorSignature(action ExecNode) string {
	return action.ActionKey + "\x1f" + action.StructureKey
}

func actionOutputsEquivalent(base, probe Graph, baseRoles, probeRoles roleProjection, baseIdx, probeIdx int, changed map[string]bool) bool {
	baseWrites := canonicalActionWriteSet(base, baseRoles, baseIdx)
	probeWrites := canonicalActionWriteSet(probe, probeRoles, probeIdx)
	if !slices.Equal(baseWrites, probeWrites) {
		return false
	}
	if len(baseWrites) == 0 && len(probeWrites) == 0 {
		return true
	}
	fallbackEquivalent := canAssumeOutputEquivalentWithoutEvidence(base, baseRoles, baseIdx) &&
		canAssumeOutputEquivalentWithoutEvidence(probe, probeRoles, probeIdx)
	if changed == nil {
		return fallbackEquivalent
	}
	sawEvidence := false
	for _, path := range probe.Actions[probeIdx].Writes {
		if !impactPathAllowed(probe, probeRoles, path) {
			continue
		}
		key := canonicalImpactPath(probe, path)
		pathChanged, ok := changed[key]
		if !ok {
			continue
		}
		sawEvidence = true
		if pathChanged {
			return false
		}
	}
	if sawEvidence {
		return true
	}
	return fallbackEquivalent
}

func canAssumeOutputEquivalentWithoutEvidence(graph Graph, roles roleProjection, idx int) bool {
	return actionHasVisibleReaders(graph, roles, idx) || actionHasVisibleNonInitialInput(graph, roles, idx)
}

func actionHasVisibleReaders(graph Graph, roles roleProjection, idx int) bool {
	if idx < 0 || idx >= len(graph.ActionWrites) {
		return false
	}
	for _, def := range graph.ActionWrites[idx] {
		if _, noise := roles.DefNoise[def]; noise {
			continue
		}
		for _, reader := range roleReadersForDef(graph, def) {
			if roleActionNoise(roles, reader) || roleActionDeliveryOnly(roles, reader) {
				continue
			}
			return true
		}
	}
	return false
}

func actionHasVisibleNonInitialInput(graph Graph, roles roleProjection, idx int) bool {
	if idx < 0 || idx >= len(graph.ActionReads) {
		return false
	}
	for _, read := range graph.ActionReads[idx] {
		if !impactPathAllowed(graph, roles, read.Path) {
			continue
		}
		for _, def := range visibleBindingDefs(read.Defs, roles) {
			if def.Writer >= 0 {
				return true
			}
		}
	}
	return false
}
