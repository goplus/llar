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

func buildGraphForProbe(probe ProbeResult) actionGraph {
	return buildGraphFromObservation(buildObservationFromProbe(probe), probe.Scope)
}

type optionProfile struct {
	seedWrites map[string]struct{}
	needPaths  map[string]struct{}
	slicePaths map[string]struct{}
	seedStates map[pathStateKey]struct{}
	needStates map[pathStateKey]struct{}
	flowStates map[pathStateKey]struct{}
	ambiguous  bool
}

type pathStateKey struct {
	path      string
	tombstone bool
}

type optionVariant struct {
	profile           optionProfile
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
	reTmpUnix              = regexp.MustCompile(`^/tmp/[^/]+`)
	reTmpMac               = regexp.MustCompile(`^/var/folders/[^/]+/[^/]+/[^/]+`)
	reBuildTryCompileNoise = regexp.MustCompile(`(^|/)TryCompile-[^/]+(/|$)`)
	reBuildCmTCNoise       = regexp.MustCompile(`(^|/)cmTC_[^/]+(/|$)`)
	reBuildCMakeTmpNoise   = regexp.MustCompile(`(^|/)CMakeTmp(/|$)`)
	reBuildTmpPIDNoise     = regexp.MustCompile(`\.tmp\.[0-9]+($|/)`)
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
		baseGraph := buildGraphForProbe(baseResult)
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
				probeGraph := buildGraphForProbe(result)
				trusted = trusted && result.TraceDiagnostics.Trusted()
				profiles[key] = append(profiles[key], optionVariant{
					profile:           diffProfileForProbes(baseResult, result, baseGraph, probeGraph),
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

func pathTouchesMainline(mainline []bool, facts pathFacts) bool {
	for _, writer := range facts.writers {
		if mainline[writer] {
			return true
		}
	}
	for _, reader := range facts.readers {
		if mainline[reader] {
			return true
		}
	}
	return false
}

func diffProfile(base, probe actionGraph) optionProfile {
	return analyzeImpact(base, probe).profile
}

func diffProfileForProbes(baseProbe, probeProbe ProbeResult, baseGraph, probeGraph actionGraph) optionProfile {
	return analyzeImpactWithEvidence(baseGraph, probeGraph, buildImpactEvidence(baseProbe, probeProbe)).profile
}

func buildImpactEvidence(baseProbe, probeProbe ProbeResult) *impactEvidence {
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
	return &impactEvidence{changed: changed}
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
	if left.profile.ambiguous || right.profile.ambiguous {
		hazards = append(hazards, collisionHazardAmbiguous)
	}
	if conservativeStateOrPathOverlap(left.profile.seedStates, right.profile.seedStates, left.profile.seedWrites, right.profile.seedWrites) {
		hazards = append(hazards, collisionHazardSeedWAW)
	}
	if conservativeStateOrPathOverlap(left.profile.flowStates, right.profile.needStates, left.profile.slicePaths, right.profile.needPaths) ||
		conservativeStateOrPathOverlap(right.profile.flowStates, left.profile.needStates, right.profile.slicePaths, left.profile.needPaths) {
		if conservativeStateOrPathOverlap(left.profile.flowStates, right.profile.needStates, left.profile.slicePaths, right.profile.needPaths) {
			hazards = append(hazards, collisionHazardLeftFlowRightNeedRAW)
		}
		if conservativeStateOrPathOverlap(right.profile.flowStates, left.profile.needStates, right.profile.slicePaths, left.profile.needPaths) {
			hazards = append(hazards, collisionHazardRightFlowLeftNeedRAW)
		}
	}
	shared := compatibleAwareSharedPaths(left.profile.flowStates, right.profile.flowStates, left.profile.slicePaths, right.profile.slicePaths)
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

func statesConflict(left, right pathStateKey) bool {
	if left.path != right.path {
		return false
	}
	if left.tombstone && right.tombstone {
		return false
	}
	return true
}

func conservativeStateOrPathOverlap(leftStates, rightStates map[pathStateKey]struct{}, leftPaths, rightPaths map[string]struct{}) bool {
	for path := range sharedPaths(leftPaths, rightPaths) {
		if statePathConflicts(leftStates, rightStates, path) {
			return true
		}
	}
	return false
}

func compatibleAwareSharedPaths(leftStates, rightStates map[pathStateKey]struct{}, leftPaths, rightPaths map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{})
	for path := range sharedPaths(leftPaths, rightPaths) {
		if statePathConflicts(leftStates, rightStates, path) {
			out[path] = struct{}{}
		}
	}
	return out
}

func statePathConflicts(leftStates, rightStates map[pathStateKey]struct{}, path string) bool {
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

func statesForPath(states map[pathStateKey]struct{}, path string) []pathStateKey {
	out := make([]pathStateKey, 0, 1)
	for state := range states {
		if state.path == path {
			out = append(out, state)
		}
	}
	return out
}

func (profile optionProfile) empty() bool {
	return !profile.ambiguous &&
		len(profile.seedWrites) == 0 &&
		len(profile.needPaths) == 0 &&
		len(profile.slicePaths) == 0 &&
		len(profile.seedStates) == 0 &&
		len(profile.needStates) == 0 &&
		len(profile.flowStates) == 0
}

func (variant optionVariant) empty() bool {
	return variant.profile.empty() && variant.outputDiff.empty()
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

func fullComponentCombos(
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
		componentSelections := expandComponentSelections(component, options)
		next := make([]map[string]string, 0, len(selections)*len(componentSelections))
		for _, existing := range selections {
			for _, selection := range componentSelections {
				merged := maps.Clone(existing)
				for key, value := range selection {
					merged[key] = value
				}
				next = append(next, merged)
			}
		}
		selections = next
	}
	for _, selection := range selections {
		merged := maps.Clone(defaults)
		for key, value := range selection {
			merged[key] = value
		}
		seen[composeCombo(requireCombo, merged, optionKeys)] = struct{}{}
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

func singletonCombos(requireCombo string, options map[string][]string, defaults map[string]string, optionKeys []string) []string {
	seen := map[string]struct{}{
		composeCombo(requireCombo, defaults, optionKeys): {},
	}
	for _, key := range optionKeys {
		for _, value := range options[key] {
			if value == "" || value == defaults[key] {
				continue
			}
			selection := maps.Clone(defaults)
			selection[key] = value
			seen[composeCombo(requireCombo, selection, optionKeys)] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(seen))
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
