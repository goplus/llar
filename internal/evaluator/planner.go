package evaluator

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/trace"
	tracessa "github.com/goplus/llar/internal/trace/ssa"
)

type ProbeResult struct {
	Records          []trace.Record
	Events           []trace.Event
	OutputDir        string
	Scope            trace.Scope
	TraceDiagnostics trace.ParseDiagnostics
	InputDigests     map[string]string
	OutputManifest   OutputManifest
	ReplayReady      bool
}

type ProbeFunc func(context.Context, string) (ProbeResult, error)
type SynthesizedPairValidator func(context.Context, string, OutputSynthesisResult) (bool, error)
type SynthesizedPairObserver func(SynthesizedPairObservation)
type MergedPairValidator func(context.Context, string, OutputMergeResult) (bool, error)
type MergedPairObserver func(MergedPairObservation)

type SynthesizedPairObservation struct {
	Combo               string
	LeftID              string
	RightID             string
	SynthesisResult     OutputSynthesisResult
	ValidationAttempted bool
	Validated           bool
	ValidationDetail    string
}

type MergedPairObservation struct {
	Combo               string
	LeftID              string
	RightID             string
	MergeResult         OutputMergeResult
	ValidationAttempted bool
	Validated           bool
}

type WatchOptions struct {
	ValidateSynthesizedPair SynthesizedPairValidator
	ObserveSynthesizedPair  SynthesizedPairObserver
	ValidateMergedPair      MergedPairValidator
	ObserveMergedPair       MergedPairObserver
}

func buildGraphForProbe(probe ProbeResult) tracessa.Graph {
	return tracessa.BuildGraph(tracessa.BuildInput{
		Records:      probe.Records,
		Events:       probe.Events,
		Scope:        probe.Scope,
		InputDigests: probe.InputDigests,
	})
}

type optionVariant struct {
	profile           tracessa.ImpactProfile
	outputDiff        outputManifestDiff
	mergeSurfacePaths map[string]struct{}
}

type collisionHazardKind string

const (
	collisionHazardAmbiguous            collisionHazardKind = "ambiguous"
	collisionHazardSeedWAW              collisionHazardKind = "seed-waw"
	collisionHazardLeftFlowRightNeedRAW collisionHazardKind = "left-flow-right-need-raw"
	collisionHazardRightFlowLeftNeedRAW collisionHazardKind = "right-flow-left-need-raw"
	collisionHazardSharedFlowWAW        collisionHazardKind = "shared-flow-waw"
)

type collisionAssessment struct {
	hazards []collisionHazardKind
}

func (assessment collisionAssessment) collide() bool {
	return len(assessment.hazards) != 0
}

var (
	reTmpUnix          = regexp.MustCompile(`^/tmp/[^/]+`)
	reTmpMac           = regexp.MustCompile(`^/var/folders/[^/]+/[^/]+/[^/]+`)
	reBuildTmpPIDNoise = regexp.MustCompile(`\.tmp\.[0-9]+$`)
)

const (
	buildTransientDirToken = "$TMPDIR"
	buildGeneratedIDToken  = "$ID"
)

func Watch(ctx context.Context, matrix formula.Matrix, probe ProbeFunc) ([]string, bool, error) {
	return WatchWithOptions(ctx, matrix, probe, WatchOptions{})
}

func WatchWithOptions(ctx context.Context, matrix formula.Matrix, probe ProbeFunc, opts WatchOptions) ([]string, bool, error) {
	validateSynthesized, observeSynthesized := normalizeSynthesisHooks(opts)
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
		trusted = trusted && baseResult.TraceDiagnostics.Trusted()
		if len(optionKeys) == 0 {
			execute[baselineCombo] = struct{}{}
			continue
		}

		profiles := make(map[string][]optionVariant, len(optionKeys))
		singletons := make(map[string]map[string]ProbeResult, len(optionKeys))
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
				trusted = trusted && result.TraceDiagnostics.Trusted()
				profiles[key] = append(profiles[key], optionVariant{
					profile:           diffProfileForProbes(baseResult, result),
					outputDiff:        diffOutputManifest(baseResult.OutputManifest, result.OutputManifest),
					mergeSurfacePaths: mergeSurfacePaths(result.Scope, baseResult.OutputManifest, result.OutputManifest),
				})
				if singletons[key] == nil {
					singletons[key] = make(map[string]ProbeResult, len(values))
				}
				singletons[key][value] = result
			}
		}

		zeroDiff := zeroDiffOptionKeys(profiles)
		if validateSynthesized != nil || observeSynthesized != nil {
			components, err := validatedCollisionComponents(
				ctx,
				requireCombo,
				defaults,
				matrix.Options,
				optionKeys,
				profiles,
				baseResult,
				singletons,
				validateSynthesized,
				observeSynthesized,
			)
			if err != nil {
				return nil, false, err
			}
			for _, combo := range componentCombos(
				requireCombo,
				matrix.Options,
				defaults,
				optionKeys,
				components,
				nil,
			) {
				execute[combo] = struct{}{}
			}
			continue
		}

		components := collisionComponents(optionKeys, profiles, zeroDiff, false)
		orthogonalKeys := orthogonalOptionKeys(optionKeys, zeroDiff)
		for _, combo := range componentCombos(
			requireCombo,
			matrix.Options,
			defaults,
			optionKeys,
			components,
			orthogonalKeys,
		) {
			execute[combo] = struct{}{}
		}
	}

	return slices.Sorted(maps.Keys(execute)), trusted, nil
}

func diffProfileForProbes(baseProbe, probeProbe ProbeResult) tracessa.ImpactProfile {
	return tracessa.AnalyzeWithEvidence(tracessa.AnalysisInput{
		Base: tracessa.AnalysisSideInput{
			Records:      baseProbe.Records,
			Events:       baseProbe.Events,
			Scope:        baseProbe.Scope,
			InputDigests: baseProbe.InputDigests,
		},
		Probe: tracessa.AnalysisSideInput{
			Records:      probeProbe.Records,
			Events:       probeProbe.Events,
			Scope:        probeProbe.Scope,
			InputDigests: probeProbe.InputDigests,
		},
	}, buildImpactEvidence(baseProbe, probeProbe)).Profile
}

func buildImpactEvidence(baseProbe, probeProbe ProbeResult) *tracessa.ImpactEvidence {
	changed := make(map[string]bool)
	addDigestEvidence := func(scope trace.Scope, digests map[string]string) map[string]string {
		out := make(map[string]string, len(digests))
		for path, sum := range digests {
			key := normalizeScopeToken(path, scope)
			if key == "" {
				continue
			}
			out[key] = sum
		}
		return out
	}
	baseDigests := addDigestEvidence(baseProbe.Scope, baseProbe.InputDigests)
	probeDigests := addDigestEvidence(probeProbe.Scope, probeProbe.InputDigests)
	for key, left := range baseDigests {
		right, ok := probeDigests[key]
		if !ok || left != right {
			changed[key] = true
			continue
		}
		changed[key] = false
	}
	for key, right := range probeDigests {
		if left, ok := baseDigests[key]; ok {
			changed[key] = left != right
			continue
		}
		changed[key] = true
	}

	for _, key := range slices.Sorted(maps.Keys(baseProbe.OutputManifest.Entries)) {
		baseEntry := baseProbe.OutputManifest.Entries[key]
		probeEntry, ok := probeProbe.OutputManifest.Entries[key]
		if !ok || baseEntry != probeEntry {
			changed[outputManifestKey(baseProbe.Scope, key)] = true
			continue
		}
		changed[outputManifestKey(baseProbe.Scope, key)] = false
	}
	for key, probeEntry := range probeProbe.OutputManifest.Entries {
		baseEntry, ok := baseProbe.OutputManifest.Entries[key]
		if !ok {
			changed[outputManifestKey(probeProbe.Scope, key)] = true
			continue
		}
		changed[outputManifestKey(probeProbe.Scope, key)] = baseEntry != probeEntry
	}
	if len(changed) == 0 {
		return nil
	}
	return &tracessa.ImpactEvidence{Changed: changed}
}

func outputManifestKey(scope trace.Scope, path string) string {
	if path == "" {
		return ""
	}
	if scope.InstallRoot == "" {
		return normalizePath(path)
	}
	return normalizeScopeToken(filepath.Join(scope.InstallRoot, filepath.FromSlash(path)), scope)
}

func isDeliveryOnlyAction(graph tracessa.Graph, idx int) bool {
	action := graph.Actions[idx]
	if len(action.Writes) == 0 {
		return false
	}
	explicitDeliveryOnly := true
	for _, changed := range action.Writes {
		if !tracessa.PathLooksDelivery(graph, changed) {
			return false
		}
		if !isExplicitDeliveryPath(changed, graph.Scope) {
			explicitDeliveryOnly = false
		}
	}
	if action.Kind == tracessa.KindCopy || action.Kind == tracessa.KindInstall {
		return true
	}
	return explicitDeliveryOnly
}

func mergeSurfacePaths(scope trace.Scope, base, probe OutputManifest) map[string]struct{} {
	paths := make(map[string]struct{}, len(base.Entries)+len(probe.Entries))
	for path := range base.Entries {
		addMergeSurfacePath(paths, scope, path)
	}
	for path := range probe.Entries {
		addMergeSurfacePath(paths, scope, path)
	}
	return paths
}

func addMergeSurfacePath(paths map[string]struct{}, scope trace.Scope, path string) {
	if path == "" {
		return
	}
	path = normalizePath(path)
	paths[path] = struct{}{}
	if scope.InstallRoot == "" {
		return
	}
	full := filepath.Join(scope.InstallRoot, filepath.FromSlash(path))
	paths[normalizeScopeToken(full, scope)] = struct{}{}
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

func collisionComponents(optionKeys []string, profiles map[string][]optionVariant, zeroDiff map[string]struct{}, allowMergeSurface bool) [][]string {
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
			if !profilesCollide(profiles[left], profiles[right], allowMergeSurface) {
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

func profilesCollide(left, right []optionVariant, allowMergeSurface bool) bool {
	for _, l := range left {
		for _, r := range right {
			if optionVariantsCollide(l, r, allowMergeSurface) {
				return true
			}
		}
	}
	return false
}

func optionVariantsCollide(left, right optionVariant, allowMergeSurface bool) bool {
	return assessOptionVariantCollision(left, right, allowMergeSurface).collide()
}

func assessOptionVariantCollision(left, right optionVariant, allowMergeSurface bool) collisionAssessment {
	var hazards []collisionHazardKind
	if left.profile.Ambiguous || right.profile.Ambiguous {
		hazards = append(hazards, collisionHazardAmbiguous)
	}
	if conservativeStateOrPathOverlap(left.profile.SeedStates, right.profile.SeedStates, left.profile.SeedWrites, right.profile.SeedWrites) {
		hazards = append(hazards, collisionHazardSeedWAW)
	}
	if conservativeStateOrPathOverlap(left.profile.FlowStates, right.profile.NeedStates, left.profile.SlicePaths, right.profile.NeedPaths) ||
		conservativeStateOrPathOverlap(right.profile.FlowStates, left.profile.NeedStates, right.profile.SlicePaths, left.profile.NeedPaths) {
		if conservativeStateOrPathOverlap(left.profile.FlowStates, right.profile.NeedStates, left.profile.SlicePaths, right.profile.NeedPaths) {
			hazards = append(hazards, collisionHazardLeftFlowRightNeedRAW)
		}
		if conservativeStateOrPathOverlap(right.profile.FlowStates, left.profile.NeedStates, right.profile.SlicePaths, left.profile.NeedPaths) {
			hazards = append(hazards, collisionHazardRightFlowLeftNeedRAW)
		}
	}
	shared := compatibleAwareSharedPaths(left.profile.FlowStates, right.profile.FlowStates, left.profile.SlicePaths, right.profile.SlicePaths)
	if len(shared) == 0 {
		return collisionAssessment{hazards: uniqueHazards(hazards)}
	}
	if !allowMergeSurface {
		hazards = append(hazards, collisionHazardSharedFlowWAW)
		return collisionAssessment{hazards: uniqueHazards(hazards)}
	}
	for path := range shared {
		if _, ok := left.mergeSurfacePaths[path]; !ok {
			hazards = append(hazards, collisionHazardSharedFlowWAW)
			return collisionAssessment{hazards: uniqueHazards(hazards)}
		}
		if _, ok := right.mergeSurfacePaths[path]; !ok {
			hazards = append(hazards, collisionHazardSharedFlowWAW)
			return collisionAssessment{hazards: uniqueHazards(hazards)}
		}
	}
	return collisionAssessment{hazards: uniqueHazards(hazards)}
}

func uniqueHazards(hazards []collisionHazardKind) []collisionHazardKind {
	if len(hazards) <= 1 {
		return hazards
	}
	seen := make(map[collisionHazardKind]struct{}, len(hazards))
	out := make([]collisionHazardKind, 0, len(hazards))
	for _, hazard := range hazards {
		if _, ok := seen[hazard]; ok {
			continue
		}
		seen[hazard] = struct{}{}
		out = append(out, hazard)
	}
	return out
}

func statesConflict(left, right tracessa.ImpactStateKey) bool {
	if left.Path != right.Path {
		return false
	}
	if left.Tombstone && right.Tombstone {
		return false
	}
	return true
}

func conservativeStateOrPathOverlap(leftStates, rightStates map[tracessa.ImpactStateKey]struct{}, leftPaths, rightPaths map[string]struct{}) bool {
	for path := range sharedPaths(leftPaths, rightPaths) {
		if statePathConflicts(leftStates, rightStates, path) {
			return true
		}
	}
	return false
}

func compatibleAwareSharedPaths(leftStates, rightStates map[tracessa.ImpactStateKey]struct{}, leftPaths, rightPaths map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{})
	for path := range sharedPaths(leftPaths, rightPaths) {
		if statePathConflicts(leftStates, rightStates, path) {
			out[path] = struct{}{}
		}
	}
	return out
}

func statePathConflicts(leftStates, rightStates map[tracessa.ImpactStateKey]struct{}, path string) bool {
	left := statesForPath(leftStates, path)
	right := statesForPath(rightStates, path)
	if len(left) == 0 || len(right) == 0 {
		return true
	}
	for _, leftState := range left {
		for _, rightState := range right {
			if statesConflict(leftState, rightState) {
				return true
			}
		}
	}
	return false
}

func statesForPath(states map[tracessa.ImpactStateKey]struct{}, path string) []tracessa.ImpactStateKey {
	out := make([]tracessa.ImpactStateKey, 0, 1)
	for state := range states {
		if state.Path == path {
			out = append(out, state)
		}
	}
	return out
}

func (variant optionVariant) empty() bool {
	return !variant.profile.Ambiguous &&
		len(variant.profile.SeedWrites) == 0 &&
		len(variant.profile.NeedPaths) == 0 &&
		len(variant.profile.SlicePaths) == 0 &&
		len(variant.profile.SeedStates) == 0 &&
		len(variant.profile.NeedStates) == 0 &&
		len(variant.profile.FlowStates) == 0 &&
		variant.outputDiff.empty()
}

func overlap(left, right map[string]struct{}) bool {
	for path := range left {
		if _, ok := right[path]; ok {
			return true
		}
	}
	return false
}

func sharedPaths(left, right map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{})
	for path := range left {
		if _, ok := right[path]; ok {
			out[path] = struct{}{}
		}
	}
	return out
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
	seen[composeCombo(requireCombo, defaults, optionKeys)] = struct{}{}
	for _, component := range components {
		for _, selection := range expandComponentSelections(component, options) {
			merged := maps.Clone(defaults)
			for key, value := range selection {
				merged[key] = value
			}
			seen[composeCombo(requireCombo, merged, optionKeys)] = struct{}{}
		}
	}
	for _, key := range orthogonalKeys {
		for _, value := range options[key] {
			merged := maps.Clone(defaults)
			merged[key] = value
			seen[composeCombo(requireCombo, merged, optionKeys)] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

func singletonUnitComponents(units []sampleUnit) [][]string {
	keys := make([]string, 0, len(units))
	adj := make(map[string]map[string]struct{}, len(units))
	for _, unit := range units {
		if len(unit.keys) != 1 {
			continue
		}
		key := unit.keys[0]
		keys = append(keys, key)
		adj[key] = make(map[string]struct{})
	}
	return buildComponentsFromAdj(keys, adj)
}

func connectUnitToFollowing(adj map[string]map[string]struct{}, units []sampleUnit, idx int) {
	for next := idx + 1; next < len(units); next++ {
		connectUnits(adj, units[idx], units[next])
	}
}

func connectUnits(adj map[string]map[string]struct{}, left, right sampleUnit) {
	for _, leftKey := range left.keys {
		leftAdj := adj[leftKey]
		if leftAdj == nil {
			leftAdj = make(map[string]struct{})
			adj[leftKey] = leftAdj
		}
		for _, rightKey := range right.keys {
			if leftKey == rightKey {
				continue
			}
			rightAdj := adj[rightKey]
			if rightAdj == nil {
				rightAdj = make(map[string]struct{})
				adj[rightKey] = rightAdj
			}
			leftAdj[rightKey] = struct{}{}
			rightAdj[leftKey] = struct{}{}
		}
	}
}

func buildComponentsFromAdj(keys []string, adj map[string]map[string]struct{}) [][]string {
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

func rootReplayAvailability(base, left, right ProbeResult) (bool, *RootReplaySummary, string) {
	plan, unavailable := planRootReplay(base, left, right)
	return unavailable == "", plan.summary, unavailable
}

func expandComponentSelections(component []string, options map[string][]string) []map[string]string {
	if len(component) == 0 {
		return []map[string]string{{}}
	}
	var out []map[string]string
	var expand func(int, map[string]string)
	expand = func(i int, selected map[string]string) {
		if i == len(component) {
			out = append(out, maps.Clone(selected))
			return
		}
		key := component[i]
		for _, value := range options[key] {
			selected[key] = value
			expand(i+1, selected)
		}
		delete(selected, key)
	}
	expand(0, make(map[string]string, len(component)))
	return out
}

type sampleUnit struct {
	id        string
	keys      []string
	selection map[string]string
}

func validatedCollisionComponents(
	ctx context.Context,
	requireCombo string,
	defaults map[string]string,
	options map[string][]string,
	optionKeys []string,
	profiles map[string][]optionVariant,
	base ProbeResult,
	singletons map[string]map[string]ProbeResult,
	validate SynthesizedPairValidator,
	observe SynthesizedPairObserver,
) ([][]string, error) {
	units := singletonSampleUnits(optionKeys, options, defaults)
	if base.OutputDir == "" {
		return singletonUnitComponents(units), nil
	}
	keys := make([]string, 0, len(units))
	adj := make(map[string]map[string]struct{}, len(units))
	for _, unit := range units {
		if len(unit.keys) != 1 {
			continue
		}
		key := unit.keys[0]
		keys = append(keys, key)
		if _, ok := adj[key]; !ok {
			adj[key] = make(map[string]struct{})
		}
	}
	for i := 0; i < len(units); i++ {
		leftProbe, ok := sampleUnitProbe(units[i], singletons)
		if !ok {
			connectUnitToFollowing(adj, units, i)
			continue
		}
		for j := i + 1; j < len(units); j++ {
			pairCombo := composePairCombo(requireCombo, defaults, optionKeys, units[i], units[j])
			stage2Collide := sampleUnitsCollide(units[i], units[j], profiles)
			rightProbe, ok := sampleUnitProbe(units[j], singletons)
			if !ok {
				connectUnits(adj, units[i], units[j])
				continue
			}
			replayAvailable, replaySummary, replayUnavailable := rootReplayAvailability(base, leftProbe, rightProbe)
			if stage2Collide && !replayAvailable {
				if observe != nil {
					observe(SynthesizedPairObservation{
						Combo:   pairCombo,
						LeftID:  units[i].id,
						RightID: units[j].id,
						SynthesisResult: OutputSynthesisResult{
							Mode:   OutputSynthesisModeRootReplay,
							Status: OutputMergeStatusNeedsRebuild,
							Replay: replaySummary,
							Issues: []OutputSynthesisIssue{replayIssue(
								OutputMergeIssueKindRootReplayUnavailable,
								"root replay is unavailable",
								replayUnavailable,
							)},
						},
					})
				}
				connectUnits(adj, units[i], units[j])
				continue
			}
			synthesisResult, err := synthesizeOutputTrees(ctx, base, leftProbe, rightProbe)
			if err != nil {
				return nil, err
			}
			observation := SynthesizedPairObservation{
				Combo:           pairCombo,
				LeftID:          units[i].id,
				RightID:         units[j].id,
				SynthesisResult: synthesisResult,
			}
			if synthesisResult.Clean() {
				validated := true
				if validate != nil {
					observation.ValidationAttempted = true
					validated, err = validate(ctx, pairCombo, synthesisResult)
					observation.Validated = validated
					if !validated && err == nil {
						observation.ValidationDetail = "validator rejected synthesized output"
					}
					_ = os.RemoveAll(synthesisResult.Root)
					if err != nil {
						return nil, err
					}
				} else {
					_ = os.RemoveAll(synthesisResult.Root)
				}
				if observe != nil {
					observe(observation)
				}
				if !validated || (stage2Collide && synthesisResult.Mode != OutputSynthesisModeRootReplay) {
					connectUnits(adj, units[i], units[j])
				}
				continue
			}
			if observe != nil {
				observe(observation)
			}
			connectUnits(adj, units[i], units[j])
		}
	}
	return buildComponentsFromAdj(keys, adj), nil
}

func sampleUnitsCollide(left, right sampleUnit, profiles map[string][]optionVariant) bool {
	for _, leftKey := range left.keys {
		leftVariants, ok := profiles[leftKey]
		if !ok {
			continue
		}
		for _, rightKey := range right.keys {
			rightVariants, ok := profiles[rightKey]
			if !ok {
				continue
			}
			if profilesCollide(leftVariants, rightVariants, true) {
				return true
			}
		}
	}
	return false
}

func singletonSampleUnits(optionKeys []string, options map[string][]string, defaults map[string]string) []sampleUnit {
	units := make([]sampleUnit, 0, len(optionKeys))
	for _, key := range optionKeys {
		selection := representativeSelection([]string{key}, options, defaults)
		if len(selection) == 0 {
			continue
		}
		units = append(units, sampleUnit{
			id:        "key:" + key,
			keys:      []string{key},
			selection: selection,
		})
	}
	return units
}

func pairCombos(requireCombo string, defaults map[string]string, optionKeys []string, units []sampleUnit) []string {
	var out []string
	for i := 0; i < len(units); i++ {
		out = append(out, pairCombosFrom(requireCombo, defaults, optionKeys, units, i)...)
	}
	result := uniqueStrings(out)
	slices.Sort(result)
	return result
}

func pairCombosFrom(requireCombo string, defaults map[string]string, optionKeys []string, units []sampleUnit, i int) []string {
	var out []string
	for j := i + 1; j < len(units); j++ {
		out = append(out, composePairCombo(requireCombo, defaults, optionKeys, units[i], units[j]))
	}
	return out
}

func composePairCombo(requireCombo string, defaults map[string]string, optionKeys []string, left, right sampleUnit) string {
	selection := maps.Clone(defaults)
	for key, value := range left.selection {
		selection[key] = value
	}
	for key, value := range right.selection {
		selection[key] = value
	}
	return composeCombo(requireCombo, selection, optionKeys)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeSynthesisHooks(opts WatchOptions) (SynthesizedPairValidator, SynthesizedPairObserver) {
	validate := opts.ValidateSynthesizedPair
	if validate == nil && opts.ValidateMergedPair != nil {
		validate = func(ctx context.Context, combo string, synthesized OutputSynthesisResult) (bool, error) {
			mergeResult, ok := synthesized.AsMergeResult()
			if !ok {
				return false, nil
			}
			return opts.ValidateMergedPair(ctx, combo, mergeResult)
		}
	}

	observe := opts.ObserveSynthesizedPair
	if observe == nil && opts.ObserveMergedPair != nil {
		observe = func(observation SynthesizedPairObservation) {
			mergeResult, ok := observation.SynthesisResult.AsMergeResult()
			if !ok {
				return
			}
			opts.ObserveMergedPair(MergedPairObservation{
				Combo:               observation.Combo,
				LeftID:              observation.LeftID,
				RightID:             observation.RightID,
				MergeResult:         mergeResult,
				ValidationAttempted: observation.ValidationAttempted,
				Validated:           observation.Validated,
			})
		}
	}
	return validate, observe
}

func sampleUnitProbe(unit sampleUnit, singletons map[string]map[string]ProbeResult) (ProbeResult, bool) {
	if len(unit.keys) != 1 {
		return ProbeResult{}, false
	}
	key := unit.keys[0]
	value, ok := unit.selection[key]
	if !ok {
		return ProbeResult{}, false
	}
	variants, ok := singletons[key]
	if !ok {
		return ProbeResult{}, false
	}
	probe, ok := variants[value]
	if !ok || probe.OutputDir == "" {
		return ProbeResult{}, false
	}
	return probe, true
}

func representativeSelection(keys []string, options map[string][]string, defaults map[string]string) map[string]string {
	selection := make(map[string]string, len(keys))
	for _, key := range keys {
		value, ok := firstNonDefaultValue(options[key], defaults[key])
		if !ok {
			return nil
		}
		selection[key] = value
	}
	return selection
}

func firstNonDefaultValue(values []string, def string) (string, bool) {
	for _, value := range values {
		if value == "" || value == def {
			continue
		}
		return value, true
	}
	return "", false
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

func normalizePath(path string) string {
	if path == "" {
		return ""
	}
	path = filepath.ToSlash(path)
	if strings.HasPrefix(path, "/tmp/$$TMP") || strings.HasPrefix(path, "/var/folders/$$TMP") {
		return path
	}
	path = reTmpUnix.ReplaceAllString(path, "/tmp/$$$$TMP")
	path = reTmpMac.ReplaceAllString(path, "/var/folders/$$$$TMP")
	return path
}

func normalizeScopeToken(token string, scope trace.Scope) string {
	if token == "" {
		return ""
	}
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
	token = normalizePath(token)
	for _, item := range replacements {
		root := normalizePath(item.root)
		if root == "" {
			continue
		}
		token = replaceScopeRootToken(token, root, item.placeholder)
	}
	return normalizeScopedBuildNoise(token)
}

func replaceScopeRootToken(token, root, placeholder string) string {
	if !strings.Contains(root, "$$TMP") {
		idx := strings.Index(token, root)
		if !validScopedRootMatch(token, idx, len(root)) {
			return token
		}
		return token[:idx] + placeholder + token[idx+len(root):]
	}
	pattern := regexp.QuoteMeta(root)
	pattern = strings.ReplaceAll(pattern, `\$\$TMP`, `[^/]+`)
	re := regexp.MustCompile(pattern)
	loc := re.FindStringIndex(token)
	if loc == nil || !validScopedRootMatch(token, loc[0], loc[1]-loc[0]) {
		return token
	}
	return token[:loc[0]] + placeholder + token[loc[1]:]
}

func validScopedRootMatch(token string, start, length int) bool {
	if start < 0 {
		return false
	}
	if start != 0 {
		firstSlash := strings.IndexByte(token, '/')
		if firstSlash != start {
			return false
		}
	}
	end := start + length
	return end == len(token) || token[end] == '/'
}

func normalizeScopedBuildNoise(token string) string {
	if !strings.Contains(token, "$BUILD") {
		return token
	}
	parts := strings.Split(token, "/")
	transientDepth := -1
	for idx, part := range parts {
		if part == "" || part == "$BUILD" {
			continue
		}
		part = normalizeBuildTempPIDPart(part)
		if looksTransientBuildDir(part) {
			parts[idx] = buildTransientDirToken
			transientDepth = 0
			continue
		}
		if transientDepth >= 0 {
			parts[idx] = normalizeTransientBuildPart(part, transientDepth == 0)
			transientDepth++
			continue
		}
		parts[idx] = part
	}
	return strings.Join(parts, "/")
}

func normalizeBuildTempPIDPart(part string) string {
	if !reBuildTmpPIDNoise.MatchString(part) {
		return part
	}
	loc := strings.LastIndex(part, ".tmp.")
	if loc < 0 {
		return part
	}
	return part[:loc] + ".tmp." + buildGeneratedIDToken
}

func looksTransientBuildDir(part string) bool {
	if part == "" || strings.Contains(part, ".") {
		return false
	}
	part = strings.ToLower(part)
	switch {
	case part == "tmp", part == "temp":
		return true
	case strings.Contains(part, "scratch"):
		return true
	case strings.HasSuffix(part, "tmp"), strings.HasSuffix(part, "temp"):
		return true
	default:
		return false
	}
}

func normalizeTransientBuildPart(part string, firstChild bool) string {
	if part == "" || strings.HasPrefix(part, "$") {
		return part
	}
	base := part
	ext := ""
	if suffix := filepath.Ext(part); suffix == ".dir" {
		base = strings.TrimSuffix(part, suffix)
		ext = suffix
	}
	prefix, sep, suffix, ok := splitGeneratedSuffix(base)
	if ok && (firstChild || looksGeneratedBuildID(suffix)) {
		return prefix + sep + buildGeneratedIDToken + ext
	}
	if !firstChild && looksGeneratedBuildID(base) {
		return buildGeneratedIDToken + ext
	}
	return part
}

func splitGeneratedSuffix(part string) (prefix, sep, suffix string, ok bool) {
	idx := strings.LastIndexAny(part, "-_")
	if idx <= 0 || idx >= len(part)-1 {
		return "", "", "", false
	}
	return part[:idx], part[idx : idx+1], part[idx+1:], true
}

func looksGeneratedBuildID(part string) bool {
	if len(part) < 6 {
		return false
	}
	hasLetter := false
	hasDigit := false
	hexOnly := true
	for _, r := range part {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasLetter = true
			if !(r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
				hexOnly = false
			}
		default:
			return false
		}
	}
	return (hasDigit && hasLetter) || (hexOnly && len(part) >= 8)
}
