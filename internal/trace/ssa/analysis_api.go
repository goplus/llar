package ssa

import (
	"maps"

	"github.com/goplus/llar/internal/trace"
)

type ImpactEvidence struct {
	Changed map[string]bool
}

type ImpactStateKey struct {
	Path      string
	Tombstone bool
	Missing   bool
}

type ImpactProfile struct {
	SeedWrites map[string]struct{}
	NeedPaths  map[string]struct{}
	SlicePaths map[string]struct{}
	JoinSet    []int
	SeedStates map[ImpactStateKey]struct{}
	NeedStates map[ImpactStateKey]struct{}
	FlowStates map[ImpactStateKey]struct{}
	Ambiguous  bool
}

type ActionPair struct {
	BaseIdx  int
	ProbeIdx int
}

type WavefrontProbeClass uint8

const (
	WavefrontProbeUnknown WavefrontProbeClass = iota
	WavefrontProbeUnchanged
	WavefrontProbeMutationRoot
	WavefrontProbeFlow
)

type WavefrontStageResult struct {
	Matched        int
	BaseOnly       []int
	ProbeOnly      []int
	Pairs          []ActionPair
	RemainingBase  []int
	RemainingProbe []int
	ProbeClass     []WavefrontProbeClass
	DivergedDefs   map[PathState]struct{}
	Ambiguous      bool
	ReadAmbiguous  bool
}

type ActionRole uint8

const (
	ActionRoleMainline ActionRole = iota
	ActionRoleTooling
	ActionRoleProbe
	ActionRoleDelivery
)

type DefRole uint8

const (
	DefRoleMainline DefRole = iota
	DefRoleTooling
	DefRoleProbe
	DefRoleDelivery
)

type RoleProjection struct {
	ActionNoise        []bool
	ActionDeliveryOnly []bool
	DefNoise           map[PathState]struct{}
	ActionClass        []ActionRole
	DefClass           map[PathState]DefRole
}

type PathSSAFlow struct {
	ReachedDefs     map[PathState]struct{}
	ReachedActions  map[int]struct{}
	JoinActions     []int
	FlowActions     []int
	FrontierActions []int
	ExternalReads   map[int]map[string]struct{}
	ExternalDefs    map[int]map[PathState]struct{}
	AmbiguousReads  bool
}

type AnalysisSideInput struct {
	Records      []trace.Record
	Events       []trace.Event
	Scope        trace.Scope
	InputDigests map[string]string
}

type AnalysisInput struct {
	Base  AnalysisSideInput
	Probe AnalysisSideInput
}

type AnalysisDebug struct {
	BaseGraph      Graph
	ProbeGraph     Graph
	BaseRoles      RoleProjection
	ProbeRoles     RoleProjection
	Wavefront      WavefrontStageResult
	Flow           PathSSAFlow
	AffectedPairs  []ActionPair
	UnchangedProbe []int
	RootProbe      []int
	FlowProbe      []int
	DivergedProbe  []int
	FrontierProbe  []int
}

type AnalysisResult struct {
	Profile ImpactProfile
	Debug   AnalysisDebug
}

func Analyze(input AnalysisInput) AnalysisResult {
	return AnalyzeWithEvidence(input, nil)
}

func AnalyzeWithEvidence(input AnalysisInput, evidence *ImpactEvidence) AnalysisResult {
	base := BuildGraph(BuildInput{
		Records:      input.Base.Records,
		Events:       input.Base.Events,
		Scope:        input.Base.Scope,
		InputDigests: input.Base.InputDigests,
	})
	probe := BuildGraph(BuildInput{
		Records:      input.Probe.Records,
		Events:       input.Probe.Events,
		Scope:        input.Probe.Scope,
		InputDigests: input.Probe.InputDigests,
	})
	baseRoles := exportRoleProjection(projectRoles(base))
	probeRoles := exportRoleProjection(projectRoles(probe))
	diff := WavefrontDiffWithEvidence(base, probe, baseRoles, probeRoles, evidence)
	profile, flow := ExtractWavefrontImpact(base, baseRoles, probe, probeRoles, diff, evidence)
	internalBaseRoles := importRoleProjection(baseRoles)
	internalProbeRoles := importRoleProjection(probeRoles)
	internalDiff := importWavefrontStageResult(diff)
	return AnalysisResult{
		Profile: profile,
		Debug: AnalysisDebug{
			BaseGraph:      base,
			ProbeGraph:     probe,
			BaseRoles:      baseRoles,
			ProbeRoles:     probeRoles,
			Wavefront:      diff,
			Flow:           flow,
			AffectedPairs:  exportActionPairs(collectAffectedPairs(base, internalBaseRoles, probe, internalProbeRoles, internalDiff)),
			UnchangedProbe: wavefrontProbeIndexes(internalDiff, wavefrontProbeUnchanged),
			RootProbe:      wavefrontVisibleMutationRoots(internalDiff, internalProbeRoles),
			FlowProbe:      wavefrontProbeIndexes(internalDiff, wavefrontProbeFlow),
			DivergedProbe:  wavefrontDivergedProbe(internalDiff),
			FrontierProbe:  append([]int(nil), flow.FrontierActions...),
		},
	}
}

func ProjectRoles(graph Graph) RoleProjection {
	return exportRoleProjection(projectRoles(graph))
}

func WavefrontDiff(base, probe Graph, baseRoles, probeRoles RoleProjection) WavefrontStageResult {
	return exportWavefrontStageResult(wavefrontDiff(base, probe, importRoleProjection(baseRoles), importRoleProjection(probeRoles)))
}

func WavefrontDiffWithEvidence(base, probe Graph, baseRoles, probeRoles RoleProjection, evidence *ImpactEvidence) WavefrontStageResult {
	return exportWavefrontStageResult(wavefrontDiffWithEvidence(base, probe, importRoleProjection(baseRoles), importRoleProjection(probeRoles), importImpactEvidence(evidence)))
}

func ExtractWavefrontImpact(base Graph, baseRoles RoleProjection, probe Graph, probeRoles RoleProjection, diff WavefrontStageResult, evidence *ImpactEvidence) (ImpactProfile, PathSSAFlow) {
	profile, flow := extractWavefrontImpact(
		base,
		importRoleProjection(baseRoles),
		probe,
		importRoleProjection(probeRoles),
		importWavefrontStageResult(diff),
		importImpactEvidence(evidence),
	)
	return exportImpactProfile(profile), exportPathSSAFlow(flow)
}

func RoleActionClass(roles RoleProjection, idx int) ActionRole {
	return ActionRole(roleActionClass(importRoleProjection(roles), idx))
}

func RoleDefClass(roles RoleProjection, def PathState) DefRole {
	return DefRole(roleDefClass(importRoleProjection(roles), def))
}

func ImpactPathAllowed(graph Graph, roles RoleProjection, path string) bool {
	return impactPathAllowed(graph, importRoleProjection(roles), path)
}

func ImpactTrackedPathAllowed(graph Graph, roles RoleProjection, path string) bool {
	return impactTrackedPathAllowed(graph, importRoleProjection(roles), path)
}

func PathLooksToolingProjected(graph Graph, roles RoleProjection, path string) bool {
	return pathLooksToolingProjected(graph, importRoleProjection(roles), path)
}

func PathLooksDelivery(graph Graph, path string) bool {
	return pathLooksDelivery(graph, path)
}

func IsProbeOnlyNoisePathProjected(graph Graph, roles RoleProjection, path string) bool {
	return isProbeOnlyNoisePathProjected(graph, importRoleProjection(roles), path)
}

func ActionReadAmbiguousVisible(graph Graph, roles RoleProjection, idx int) bool {
	return actionReadAmbiguousVisible(graph, importRoleProjection(roles), idx)
}

func CanonicalImpactPath(graph Graph, path string) string {
	return canonicalImpactPath(graph, path)
}

func PathChanged(evidence *ImpactEvidence, graph Graph, path string) bool {
	return pathChanged(importImpactEvidence(evidence), graph, path)
}

func NormalizeExecEnv(env []string, scope trace.Scope) []string {
	return normalizeEnvEntries(env, scope)
}

func importImpactEvidence(evidence *ImpactEvidence) *impactEvidence {
	if evidence == nil {
		return nil
	}
	return &impactEvidence{changed: maps.Clone(evidence.Changed)}
}

func exportImpactProfile(in optionProfile) ImpactProfile {
	return ImpactProfile{
		SeedWrites: maps.Clone(in.seedWrites),
		NeedPaths:  maps.Clone(in.needPaths),
		SlicePaths: maps.Clone(in.slicePaths),
		JoinSet:    append([]int(nil), in.joinSet...),
		SeedStates: exportStateKeys(in.seedStates),
		NeedStates: exportStateKeys(in.needStates),
		FlowStates: exportStateKeys(in.flowStates),
		Ambiguous:  in.ambiguous,
	}
}

func exportStateKeys(in map[pathStateKey]struct{}) map[ImpactStateKey]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[ImpactStateKey]struct{}, len(in))
	for key := range in {
		out[ImpactStateKey{Path: key.path, Tombstone: key.tombstone, Missing: key.missing}] = struct{}{}
	}
	return out
}

func exportActionPairs(in []actionPair) []ActionPair {
	if len(in) == 0 {
		return nil
	}
	out := make([]ActionPair, 0, len(in))
	for _, pair := range in {
		out = append(out, ActionPair{BaseIdx: pair.baseIdx, ProbeIdx: pair.probeIdx})
	}
	return out
}

func exportProbeClasses(in []wavefrontProbeClass) []WavefrontProbeClass {
	if len(in) == 0 {
		return nil
	}
	out := make([]WavefrontProbeClass, 0, len(in))
	for _, class := range in {
		out = append(out, WavefrontProbeClass(class))
	}
	return out
}

func exportRoleProjection(in roleProjection) RoleProjection {
	out := RoleProjection{
		ActionNoise:        append([]bool(nil), in.ActionNoise...),
		ActionDeliveryOnly: append([]bool(nil), in.ActionDeliveryOnly...),
		ActionClass:        make([]ActionRole, len(in.ActionClass)),
	}
	if len(in.DefNoise) != 0 {
		out.DefNoise = make(map[PathState]struct{}, len(in.DefNoise))
		for def := range in.DefNoise {
			out.DefNoise[def] = struct{}{}
		}
	}
	if len(in.DefClass) != 0 {
		out.DefClass = make(map[PathState]DefRole, len(in.DefClass))
		for def, class := range in.DefClass {
			out.DefClass[def] = DefRole(class)
		}
	}
	for i, class := range in.ActionClass {
		out.ActionClass[i] = ActionRole(class)
	}
	return out
}

func importRoleProjection(in RoleProjection) roleProjection {
	out := roleProjection{
		ActionNoise:        append([]bool(nil), in.ActionNoise...),
		ActionDeliveryOnly: append([]bool(nil), in.ActionDeliveryOnly...),
		ActionClass:        make([]actionRole, len(in.ActionClass)),
	}
	if len(in.DefNoise) != 0 {
		out.DefNoise = make(map[PathState]struct{}, len(in.DefNoise))
		for def := range in.DefNoise {
			out.DefNoise[def] = struct{}{}
		}
	} else {
		out.DefNoise = make(map[PathState]struct{})
	}
	if len(in.DefClass) != 0 {
		out.DefClass = make(map[PathState]defRole, len(in.DefClass))
		for def, class := range in.DefClass {
			out.DefClass[def] = defRole(class)
		}
	} else {
		out.DefClass = make(map[PathState]defRole)
	}
	for i, class := range in.ActionClass {
		out.ActionClass[i] = actionRole(class)
	}
	return out
}

func exportWavefrontStageResult(in wavefrontStageResult) WavefrontStageResult {
	out := WavefrontStageResult{
		Matched:        in.matched,
		BaseOnly:       append([]int(nil), in.baseOnly...),
		ProbeOnly:      append([]int(nil), in.probeOnly...),
		Pairs:          exportActionPairs(in.pairs),
		RemainingBase:  append([]int(nil), in.remainingBase...),
		RemainingProbe: append([]int(nil), in.remainingProbe...),
		ProbeClass:     exportProbeClasses(in.probeClass),
		Ambiguous:      in.ambiguous,
		ReadAmbiguous:  in.readAmbiguous,
	}
	if len(in.divergedDefs) != 0 {
		out.DivergedDefs = make(map[PathState]struct{}, len(in.divergedDefs))
		for def := range in.divergedDefs {
			out.DivergedDefs[def] = struct{}{}
		}
	}
	return out
}

func importWavefrontStageResult(in WavefrontStageResult) wavefrontStageResult {
	out := wavefrontStageResult{
		matched:        in.Matched,
		baseOnly:       append([]int(nil), in.BaseOnly...),
		probeOnly:      append([]int(nil), in.ProbeOnly...),
		remainingBase:  append([]int(nil), in.RemainingBase...),
		remainingProbe: append([]int(nil), in.RemainingProbe...),
		probeClass:     make([]wavefrontProbeClass, len(in.ProbeClass)),
		ambiguous:      in.Ambiguous,
		readAmbiguous:  in.ReadAmbiguous,
	}
	for i, class := range in.ProbeClass {
		out.probeClass[i] = wavefrontProbeClass(class)
	}
	if len(in.Pairs) != 0 {
		out.pairs = make([]actionPair, 0, len(in.Pairs))
		for _, pair := range in.Pairs {
			out.pairs = append(out.pairs, actionPair{baseIdx: pair.BaseIdx, probeIdx: pair.ProbeIdx})
		}
	}
	if len(in.DivergedDefs) != 0 {
		out.divergedDefs = make(map[PathState]struct{}, len(in.DivergedDefs))
		for def := range in.DivergedDefs {
			out.divergedDefs[def] = struct{}{}
		}
	}
	return out
}

func exportPathSSAFlow(in pathSSAFlow) PathSSAFlow {
	out := PathSSAFlow{
		JoinActions:     append([]int(nil), in.joinActions...),
		FlowActions:     append([]int(nil), in.flowActions...),
		FrontierActions: append([]int(nil), in.frontierActions...),
		AmbiguousReads:  in.ambiguousReads,
	}
	if len(in.reachedDefs) != 0 {
		out.ReachedDefs = make(map[PathState]struct{}, len(in.reachedDefs))
		for def := range in.reachedDefs {
			out.ReachedDefs[def] = struct{}{}
		}
	}
	if len(in.reachedActions) != 0 {
		out.ReachedActions = make(map[int]struct{}, len(in.reachedActions))
		for idx := range in.reachedActions {
			out.ReachedActions[idx] = struct{}{}
		}
	}
	if len(in.externalReads) != 0 {
		out.ExternalReads = make(map[int]map[string]struct{}, len(in.externalReads))
		for idx, reads := range in.externalReads {
			out.ExternalReads[idx] = maps.Clone(reads)
		}
	}
	if len(in.externalDefs) != 0 {
		out.ExternalDefs = make(map[int]map[PathState]struct{}, len(in.externalDefs))
		for idx, defs := range in.externalDefs {
			copied := make(map[PathState]struct{}, len(defs))
			for def := range defs {
				copied[def] = struct{}{}
			}
			out.ExternalDefs[idx] = copied
		}
	}
	return out
}
