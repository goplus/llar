package evaluator

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/goplus/llar/internal/trace"
	tracessa "github.com/goplus/llar/internal/trace/ssa"
)

var replayEnvKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const (
	replayMaxChangedRoots   = 2
	replayMaxSelectedRoots  = 4
	replayMaxSelectedWrites = 128
)

type replayRoot struct {
	identity string
	siteKey  string
	pid      int64
	cwd      string
	argv     []string
	env      []string
	reads    []string
	writes   []string
}

type replayRootScan struct {
	candidates int
	roots      []replayRoot
	graph      tracessa.Graph
}

type replayParsedArgv struct {
	program       string
	opaque        []string
	keyedOrder    []string
	keyed         map[string]string
	additiveOrder []string
	additive      map[string]struct{}
}

type replayEnvSpec struct {
	order  []string
	values map[string]string
}

type replayPaths struct {
	sourceRoot  string
	buildRoot   string
	installRoot string
}

func synthesizeByRootReplay(ctx context.Context, base, left, right ProbeResult) (OutputSynthesisResult, error) {
	result := OutputSynthesisResult{
		Mode:   OutputSynthesisModeRootReplay,
		Status: OutputMergeStatusNeedsRebuild,
	}
	metadata, ok := mergeMetadata(base.OutputManifest.Metadata, left.OutputManifest.Metadata, right.OutputManifest.Metadata)
	if !ok {
		result.Issues = append(result.Issues, replayIssue(
			OutputMergeIssueKindRootReplayUnavailable,
			"metadata requires real pair build",
			"metadata cannot be merged before replay",
		))
		return result, nil
	}
	result.Metadata = metadata

	plan, unavailable := planRootReplay(base, left, right)
	result.Replay = plan.summary
	if unavailable != "" {
		if result.Replay == nil {
			result.Replay = &RootReplaySummary{Unavailable: unavailable}
		} else {
			result.Replay.Unavailable = unavailable
		}
		result.Issues = append(result.Issues, replayIssue(
			OutputMergeIssueKindRootReplayUnavailable,
			"root replay is unavailable",
			unavailable,
		))
		return result, nil
	}

	replayResult, err := executeRootReplay(ctx, plan, metadata)
	if err != nil {
		return OutputSynthesisResult{}, err
	}
	return replayResult, nil
}

type replayPlan struct {
	steps   []replayRoot
	base    ProbeResult
	summary *RootReplaySummary
}

type alignedReplayRoots struct {
	base  []replayRoot
	left  []replayRoot
	right []replayRoot
}

func planRootReplay(base, left, right ProbeResult) (replayPlan, string) {
	switch {
	case !base.ReplayReady || !left.ReplayReady || !right.ReplayReady:
		return replayPlan{summary: &RootReplaySummary{Unavailable: "replay-ready trace scope is required on base, left, and right probes"}}, "replay-ready trace scope is required on base, left, and right probes"
	case base.Scope.SourceRoot == "" || left.Scope.SourceRoot == "" || right.Scope.SourceRoot == "":
		return replayPlan{summary: &RootReplaySummary{Unavailable: "missing preserved source root for replay"}}, "missing preserved source root for replay"
	case base.OutputDir == "" || left.OutputDir == "" || right.OutputDir == "":
		return replayPlan{summary: &RootReplaySummary{Unavailable: "missing output directory for replay planning"}}, "missing output directory for replay planning"
	}

	baseScan := replayRoots(base)
	leftScan := replayRoots(left)
	rightScan := replayRoots(right)
	summary := &RootReplaySummary{
		CandidateRoots: maxInt(baseScan.candidates, leftScan.candidates, rightScan.candidates),
		EligibleRoots:  maxInt(len(baseScan.roots), len(leftScan.roots), len(rightScan.roots)),
	}
	if len(baseScan.roots) == 0 || len(leftScan.roots) == 0 || len(rightScan.roots) == 0 {
		summary.Unavailable = "no replayable top-level roots found"
		return replayPlan{summary: summary}, summary.Unavailable
	}
	aligned, unavailable := alignReplayRoots(baseScan.roots, leftScan.roots, rightScan.roots)
	if unavailable != "" {
		summary.Unavailable = unavailable
		return replayPlan{summary: summary}, unavailable
	}
	leftJoinRoots := replayJoinRootIndexes(base, left, leftScan)
	rightJoinRoots := replayJoinRootIndexes(base, right, rightScan)
	joinIndexes := make(map[int]struct{}, len(leftJoinRoots)+len(rightJoinRoots))
	for idx := range leftJoinRoots {
		joinIndexes[idx] = struct{}{}
	}
	for idx := range rightJoinRoots {
		joinIndexes[idx] = struct{}{}
	}

	steps := make([]replayRoot, 0, len(aligned.base))
	changedIndexes := make(map[int]struct{})
	for i := range aligned.base {
		baseStep := aligned.base[i]
		leftStep := aligned.left[i]
		rightStep := aligned.right[i]
		if baseStep.siteKey != leftStep.siteKey || baseStep.siteKey != rightStep.siteKey {
			summary.Unavailable = "eligible replay root sites do not align across probes"
			return replayPlan{summary: summary}, summary.Unavailable
		}
		if replayUsesShellCommand(baseStep.argv) || replayUsesShellCommand(leftStep.argv) || replayUsesShellCommand(rightStep.argv) {
			summary.Unavailable = fmt.Sprintf("shell command wrapper at replay root %q is unsupported", baseStep.siteKey)
			return replayPlan{summary: summary}, summary.Unavailable
		}
		merged, stepChanged, err := mergeReplayRoot(baseStep, leftStep, rightStep)
		if err != nil {
			summary.Unavailable = err.Error()
			return replayPlan{summary: summary}, summary.Unavailable
		}
		if stepChanged {
			changedIndexes[i] = struct{}{}
			summary.ChangedRoots = append(summary.ChangedRoots, merged.siteKey)
		}
		steps = append(steps, merged)
	}
	if len(changedIndexes) == 0 {
		summary.Unavailable = "no replay root parameters changed across probes"
		return replayPlan{summary: summary}, summary.Unavailable
	}
	selected := selectReplayFrontier(steps, changedIndexes)
	if len(joinIndexes) != 0 {
		selected = selectReplayJoinFrontier(steps, changedIndexes, joinIndexes)
	}
	if len(selected) == 0 {
		summary.Unavailable = "no replay roots selected after frontier planning"
		return replayPlan{summary: summary}, summary.Unavailable
	}
	for _, idx := range selected {
		summary.SelectedRoots = append(summary.SelectedRoots, steps[idx].siteKey)
		summary.SelectedCommands = append(summary.SelectedCommands, strings.Join(steps[idx].argv, " "))
	}
	summary.SelectedWrites = countReplayFrontierWrites(steps, selected)
	switch {
	case len(summary.ChangedRoots) > replayMaxChangedRoots:
		summary.Unavailable = fmt.Sprintf("replay frontier too wide: changed roots=%d exceeds limit %d", len(summary.ChangedRoots), replayMaxChangedRoots)
		return replayPlan{summary: summary}, summary.Unavailable
	case len(selected) > replayMaxSelectedRoots:
		summary.Unavailable = fmt.Sprintf("replay frontier too wide: selected roots=%d exceeds limit %d", len(selected), replayMaxSelectedRoots)
		return replayPlan{summary: summary}, summary.Unavailable
	case summary.SelectedWrites > replayMaxSelectedWrites:
		summary.Unavailable = fmt.Sprintf("replay frontier too wide: selected writes=%d exceeds limit %d", summary.SelectedWrites, replayMaxSelectedWrites)
		return replayPlan{summary: summary}, summary.Unavailable
	}
	selectedSteps := make([]replayRoot, 0, len(selected))
	for _, idx := range selected {
		selectedSteps = append(selectedSteps, steps[idx])
	}
	return replayPlan{steps: selectedSteps, base: base, summary: summary}, ""
}

func executeRootReplay(ctx context.Context, plan replayPlan, metadata string) (OutputSynthesisResult, error) {
	base := plan.base
	sourceRoot, cleanupSource, err := cloneReplaySource(base.Scope.SourceRoot)
	if err != nil {
		return OutputSynthesisResult{}, err
	}
	defer cleanupSource()

	buildRoot, cleanupBuild, err := deriveReplayBuildRoot(base.Scope, sourceRoot)
	if err != nil {
		return OutputSynthesisResult{}, err
	}
	defer cleanupBuild()
	if err := prepareReplayBuildRoot(buildRoot, plan.steps); err != nil {
		return OutputSynthesisResult{}, err
	}

	installRoot, cleanupInstall, err := cloneReplayOutput(base.OutputDir)
	if err != nil {
		return OutputSynthesisResult{}, err
	}
	keepInstall := false
	defer func() {
		if !keepInstall {
			cleanupInstall()
		}
	}()

	paths := replayPaths{
		sourceRoot:  sourceRoot,
		buildRoot:   buildRoot,
		installRoot: installRoot,
	}
	for _, step := range plan.steps {
		if err := runReplayStep(ctx, step, paths); err != nil {
			return OutputSynthesisResult{
				Mode:     OutputSynthesisModeRootReplay,
				Status:   OutputMergeStatusNeedsRebuild,
				Metadata: metadata,
				Replay:   plan.summary,
				Issues: []OutputSynthesisIssue{replayIssue(
					OutputMergeIssueKindRootReplayFailed,
					"root replay execution failed",
					err.Error(),
				)},
			}, nil
		}
	}

	manifest, err := BuildOutputManifest(installRoot, metadata)
	if err != nil {
		return OutputSynthesisResult{}, err
	}
	keepInstall = true
	return OutputSynthesisResult{
		Mode:     OutputSynthesisModeRootReplay,
		Status:   OutputMergeStatusMerged,
		Root:     installRoot,
		Metadata: metadata,
		Manifest: manifest,
		Replay:   plan.summary,
	}, nil
}

func replayRoots(probe ProbeResult) replayRootScan {
	if len(probe.Records) == 0 {
		return replayRootScan{}
	}
	graph := buildGraphForProbe(probe)
	roles := tracessa.ProjectRoles(graph)
	pids := make(map[int64]struct{}, len(probe.Records))
	for _, rec := range probe.Records {
		if rec.PID != 0 {
			pids[rec.PID] = struct{}{}
		}
	}
	candidates := 0
	roots := make([]replayRoot, 0, len(probe.Records))
	for _, rec := range probe.Records {
		if len(rec.Argv) == 0 {
			continue
		}
		if rec.ParentPID != 0 {
			if _, ok := pids[rec.ParentPID]; ok {
				continue
			}
		}
		candidates++
		argv := normalizeReplayTokens(rec.Argv, probe.Scope)
		env := normalizeReplayEnv(rec.Env, probe.Scope)
		cwd := normalizeScopeToken(rec.Cwd, probe.Scope)
		reads := filterReplayRelevantPaths(rec.Inputs, probe.Scope, graph, roles)
		writes := filterReplayRelevantPaths(rec.Changes, probe.Scope, graph, roles)
		if !replayRootRelevant(reads, writes) {
			continue
		}
		roots = append(roots, replayRoot{
			identity: replayRootIdentity(argv, cwd),
			siteKey:  replaySiteKey(argv, cwd),
			pid:      rec.PID,
			cwd:      cwd,
			argv:     argv,
			env:      env,
			reads:    reads,
			writes:   writes,
		})
	}
	return replayRootScan{candidates: candidates, roots: roots, graph: graph}
}

func selectReplayFrontier(steps []replayRoot, changed map[int]struct{}) []int {
	selected := make(map[int]struct{}, len(changed))
	for idx := range changed {
		selected[idx] = struct{}{}
	}

	queue := make([]int, 0, len(changed))
	for idx := range changed {
		queue = append(queue, idx)
	}
	slices.Sort(queue)

	for len(queue) > 0 {
		idx := queue[0]
		queue = queue[1:]
		writes := pathSet(steps[idx].writes)
		if len(writes) == 0 {
			continue
		}
		for next := idx + 1; next < len(steps); next++ {
			if _, ok := selected[next]; ok {
				continue
			}
			if !pathsOverlap(writes, steps[next].reads) {
				continue
			}
			selected[next] = struct{}{}
			queue = append(queue, next)
		}
	}

	order := make([]int, 0, len(selected))
	for idx := range selected {
		order = append(order, idx)
	}
	slices.Sort(order)
	return order
}

func selectReplayJoinFrontier(steps []replayRoot, changed, join map[int]struct{}) []int {
	if len(join) == 0 {
		return selectReplayFrontier(steps, changed)
	}
	selected := make(map[int]struct{}, len(join)+len(changed))
	for idx := range join {
		selected[idx] = struct{}{}
	}
	for {
		changedAdded := false
		for target := range selected {
			if target < 0 || target >= len(steps) {
				continue
			}
			for prev := 0; prev < target; prev++ {
				if _, ok := changed[prev]; !ok {
					continue
				}
				if _, ok := selected[prev]; ok {
					continue
				}
				if !pathsOverlap(pathSet(steps[prev].writes), steps[target].reads) {
					continue
				}
				selected[prev] = struct{}{}
				changedAdded = true
			}
		}
		if !changedAdded {
			break
		}
	}

	queue := make([]int, 0, len(join))
	for idx := range join {
		queue = append(queue, idx)
	}
	slices.Sort(queue)
	for len(queue) > 0 {
		idx := queue[0]
		queue = queue[1:]
		writes := pathSet(steps[idx].writes)
		if len(writes) == 0 {
			continue
		}
		for next := idx + 1; next < len(steps); next++ {
			if _, ok := selected[next]; ok {
				continue
			}
			if !pathsOverlap(writes, steps[next].reads) {
				continue
			}
			selected[next] = struct{}{}
			queue = append(queue, next)
		}
	}
	order := make([]int, 0, len(selected))
	for idx := range selected {
		order = append(order, idx)
	}
	slices.Sort(order)
	return order
}

func countReplayFrontierWrites(steps []replayRoot, selected []int) int {
	if len(selected) == 0 {
		return 0
	}
	materialized := make(map[string]struct{})
	seen := make(map[string]struct{})
	for _, idx := range selected {
		for _, path := range steps[idx].writes {
			if isReplayMaterializedPath(path) {
				materialized[path] = struct{}{}
			}
			seen[path] = struct{}{}
		}
	}
	if len(materialized) != 0 {
		return len(materialized)
	}
	return len(seen)
}

func isReplayMaterializedPath(path string) bool {
	path = normalizePath(path)
	return path == "$INSTALL" || strings.HasPrefix(path, "$INSTALL/")
}

func pathSet(paths []string) map[string]struct{} {
	if len(paths) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		out[path] = struct{}{}
	}
	return out
}

func pathsOverlap(left map[string]struct{}, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	for _, path := range right {
		if _, ok := left[path]; ok {
			return true
		}
	}
	return false
}

func filterReplayRelevantPaths(paths []string, scope trace.Scope, graph tracessa.Graph, roles tracessa.RoleProjection) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if !replayPathRelevant(path, graph, roles) {
			continue
		}
		token := normalizeScopeToken(path, scope)
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func replayPathRelevant(path string, graph tracessa.Graph, roles tracessa.RoleProjection) bool {
	if path == "" {
		return false
	}
	token := normalizeScopeToken(path, graph.Scope)
	if !strings.Contains(token, "$SRC") && !strings.Contains(token, "$BUILD") && !strings.Contains(token, "$INSTALL") {
		return false
	}
	path = normalizePath(path)
	if _, ok := graph.Paths[path]; !ok {
		return false
	}
	if isExplicitDeliveryPath(path, graph.Scope) {
		return true
	}
	if len(graph.DefsByPath[path]) != 0 {
		for _, def := range graph.DefsByPath[path] {
			class := tracessa.RoleDefClass(roles, def)
			if class != tracessa.DefRoleProbe && class != tracessa.DefRoleTooling {
				return true
			}
		}
		return false
	}
	if tracessa.IsProbeOnlyNoisePathProjected(graph, roles, path) {
		return false
	}
	if tracessa.PathLooksToolingProjected(graph, roles, path) {
		return false
	}
	return true
}

func replayRootRelevant(reads, writes []string) bool {
	return len(reads) != 0 || len(writes) != 0
}

func normalizeReplayTokens(tokens []string, scope trace.Scope) []string {
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, normalizeScopeToken(token, scope))
	}
	return out
}

func normalizeReplayEnv(env []string, scope trace.Scope) []string {
	return tracessa.NormalizeExecEnv(env, scope)
}

func replayJoinRootIndexes(base, probe ProbeResult, scan replayRootScan) map[int]struct{} {
	if len(scan.roots) == 0 {
		return nil
	}
	analysis := tracessa.AnalyzeWithEvidence(tracessa.AnalysisInput{
		Base: tracessa.AnalysisSideInput{
			Records:      base.Records,
			Events:       base.Events,
			Scope:        base.Scope,
			InputDigests: base.InputDigests,
		},
		Probe: tracessa.AnalysisSideInput{
			Records:      probe.Records,
			Events:       probe.Events,
			Scope:        probe.Scope,
			InputDigests: probe.InputDigests,
		},
	}, buildImpactEvidence(base, probe))
	joinActions := replayMinJoinActionIndexes(scan.graph, analysis.Profile.JoinSet)
	if len(joinActions) == 0 {
		return nil
	}
	rootByPID, ambiguousRootPID := replayRootIndexesByPID(scan.roots)
	recordPIDs, parentByPID := replayProcessTree(probe.Records)
	out := make(map[int]struct{}, len(joinActions))
	for _, actionIdx := range joinActions {
		if actionIdx < 0 || actionIdx >= len(scan.graph.Actions) {
			continue
		}
		action := scan.graph.Actions[actionIdx]
		rootPID, ok := replayTopLevelPID(action.PID, recordPIDs, parentByPID)
		if !ok {
			continue
		}
		if _, ambiguous := ambiguousRootPID[rootPID]; ambiguous {
			continue
		}
		rootIdx, ok := rootByPID[rootPID]
		if !ok {
			continue
		}
		out[rootIdx] = struct{}{}
	}
	return out
}

func replayMinJoinActionIndexes(graph tracessa.Graph, joinSet []int) []int {
	if len(joinSet) == 0 || len(graph.Actions) == 0 {
		return nil
	}
	join := make(map[int]struct{}, len(joinSet))
	for _, idx := range joinSet {
		if idx < 0 || idx >= len(graph.Actions) {
			continue
		}
		join[idx] = struct{}{}
	}
	if len(join) == 0 {
		return nil
	}
	order := make([]int, 0, len(join))
	for idx := range join {
		order = append(order, idx)
	}
	slices.Sort(order)
	minimal := make([]int, 0, len(order))
	for _, idx := range order {
		if replayHasJoinAncestor(graph, idx, join) {
			continue
		}
		minimal = append(minimal, idx)
	}
	return minimal
}

func replayHasJoinAncestor(graph tracessa.Graph, idx int, join map[int]struct{}) bool {
	if idx < 0 || idx >= len(graph.In) {
		return false
	}
	visited := map[int]struct{}{idx: {}}
	stack := []int{idx}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, edge := range graph.In[cur] {
			pred := edge.From
			if pred < 0 || pred >= len(graph.Actions) {
				continue
			}
			if _, seen := visited[pred]; seen {
				continue
			}
			if _, ok := join[pred]; ok {
				return true
			}
			visited[pred] = struct{}{}
			stack = append(stack, pred)
		}
	}
	return false
}

func replayRootIndexesByPID(roots []replayRoot) (map[int64]int, map[int64]struct{}) {
	indexes := make(map[int64]int, len(roots))
	ambiguous := make(map[int64]struct{})
	for idx, root := range roots {
		if root.pid == 0 {
			continue
		}
		if prev, ok := indexes[root.pid]; ok && prev != idx {
			ambiguous[root.pid] = struct{}{}
			delete(indexes, root.pid)
			continue
		}
		if _, ok := ambiguous[root.pid]; ok {
			continue
		}
		indexes[root.pid] = idx
	}
	return indexes, ambiguous
}

func replayProcessTree(records []trace.Record) (map[int64]struct{}, map[int64]int64) {
	pids := make(map[int64]struct{}, len(records))
	parentByPID := make(map[int64]int64, len(records))
	for _, rec := range records {
		if rec.PID == 0 {
			continue
		}
		pids[rec.PID] = struct{}{}
		if _, ok := parentByPID[rec.PID]; !ok {
			parentByPID[rec.PID] = rec.ParentPID
		}
	}
	return pids, parentByPID
}

func replayTopLevelPID(pid int64, pids map[int64]struct{}, parentByPID map[int64]int64) (int64, bool) {
	if pid == 0 {
		return 0, false
	}
	if _, ok := pids[pid]; !ok {
		return 0, false
	}
	cur := pid
	for {
		parent, ok := parentByPID[cur]
		if !ok || parent == 0 {
			return cur, true
		}
		if _, ok := pids[parent]; !ok {
			return cur, true
		}
		cur = parent
	}
}

func replaySiteKey(argv []string, cwd string) string {
	if spec, ok := parseReplayArgv(argv); ok {
		var b strings.Builder
		b.WriteString(spec.program)
		for _, token := range spec.opaque {
			b.WriteByte(' ')
			b.WriteString(token)
		}
		if len(spec.keyedOrder) != 0 {
			b.WriteString(" [")
			for i, key := range spec.keyedOrder {
				if i != 0 {
					b.WriteString(", ")
				}
				b.WriteString(key)
			}
			b.WriteByte(']')
		}
		if len(spec.additiveOrder) != 0 {
			b.WriteString(" {")
			for i, token := range spec.additiveOrder {
				if i != 0 {
					b.WriteString(", ")
				}
				b.WriteString(token)
			}
			b.WriteByte('}')
		}
		if cwd != "" {
			b.WriteString(" @ ")
			b.WriteString(cwd)
		}
		return b.String()
	}
	if len(argv) == 0 {
		return cwd
	}
	if cwd == "" {
		return strings.Join(argv, " ")
	}
	return strings.Join(argv, " ") + " @ " + cwd
}

func replayRootIdentity(argv []string, cwd string) string {
	spec, ok := parseReplayArgv(argv)
	if !ok {
		return replaySiteKey(argv, cwd)
	}
	var parts []string
	parts = append(parts, spec.program, "cwd="+cwd)
	if len(spec.opaque) != 0 {
		parts = append(parts, "opaque="+strings.Join(spec.opaque, "\x1f"))
	}
	if len(spec.keyedOrder) != 0 {
		parts = append(parts, "keys="+strings.Join(spec.keyedOrder, "\x1f"))
	}
	if len(spec.additiveOrder) != 0 {
		parts = append(parts, "add="+strings.Join(spec.additiveOrder, "\x1f"))
	}
	return strings.Join(parts, "|")
}

func replayUsesShellCommand(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	tool := filepath.Base(argv[0])
	switch tool {
	case "sh", "bash", "dash", "zsh":
		return argv[1] == "-c" || argv[1] == "-lc"
	default:
		return false
	}
}

func mergeReplayRoot(base, left, right replayRoot) (replayRoot, bool, error) {
	argv, argvChanged, err := mergeReplayArgv(base.argv, left.argv, right.argv)
	if err != nil {
		return replayRoot{}, false, err
	}
	env, envChanged, err := mergeReplayEnv(base.env, left.env, right.env)
	if err != nil {
		return replayRoot{}, false, err
	}
	return replayRoot{
		identity: base.identity,
		siteKey:  base.siteKey,
		cwd:      base.cwd,
		argv:     argv,
		env:      env,
		reads:    mergeReplayPaths(base.reads, left.reads, right.reads),
		writes:   mergeReplayPaths(base.writes, left.writes, right.writes),
	}, argvChanged || envChanged, nil
}

func alignReplayRoots(base, left, right []replayRoot) (alignedReplayRoots, string) {
	baseByID, err := replayRootIndex(base)
	if err != nil {
		return alignedReplayRoots{}, err.Error()
	}
	leftByID, err := replayRootIndex(left)
	if err != nil {
		return alignedReplayRoots{}, err.Error()
	}
	rightByID, err := replayRootIndex(right)
	if err != nil {
		return alignedReplayRoots{}, err.Error()
	}
	baseIDs := replayRootIdentities(base)
	leftIDs := replayRootIdentities(left)
	rightIDs := replayRootIdentities(right)
	if !slices.Equal(baseIDs, leftIDs) || !slices.Equal(baseIDs, rightIDs) {
		return alignedReplayRoots{}, "eligible replay root identities differ across probes"
	}
	aligned := alignedReplayRoots{
		base:  make([]replayRoot, 0, len(baseIDs)),
		left:  make([]replayRoot, 0, len(baseIDs)),
		right: make([]replayRoot, 0, len(baseIDs)),
	}
	for _, id := range baseIDs {
		aligned.base = append(aligned.base, baseByID[id])
		aligned.left = append(aligned.left, leftByID[id])
		aligned.right = append(aligned.right, rightByID[id])
	}
	return aligned, ""
}

func replayRootIndex(roots []replayRoot) (map[string]replayRoot, error) {
	out := make(map[string]replayRoot, len(roots))
	for _, root := range roots {
		if root.identity == "" {
			return nil, fmt.Errorf("replay root %q is missing identity", root.siteKey)
		}
		if _, ok := out[root.identity]; ok {
			return nil, fmt.Errorf("replay root identity %q is ambiguous", root.siteKey)
		}
		out[root.identity] = root
	}
	return out, nil
}

func replayRootIdentities(roots []replayRoot) []string {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		out = append(out, root.identity)
	}
	return out
}

func maxInt(values ...int) int {
	out := 0
	for _, value := range values {
		if value > out {
			out = value
		}
	}
	return out
}

func mergeReplayPaths(base, left, right []string) []string {
	seen := make(map[string]struct{}, len(base)+len(left)+len(right))
	out := make([]string, 0, len(base)+len(left)+len(right))
	for _, values := range [][]string{base, left, right} {
		for _, value := range values {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func mergeReplayArgv(base, left, right []string) ([]string, bool, error) {
	baseSpec, ok := parseReplayArgv(base)
	if !ok {
		return nil, false, fmt.Errorf("unmergeable base argv %q", strings.Join(base, " "))
	}
	leftSpec, ok := parseReplayArgv(left)
	if !ok {
		return nil, false, fmt.Errorf("unmergeable left argv %q", strings.Join(left, " "))
	}
	rightSpec, ok := parseReplayArgv(right)
	if !ok {
		return nil, false, fmt.Errorf("unmergeable right argv %q", strings.Join(right, " "))
	}
	if baseSpec.program != leftSpec.program || baseSpec.program != rightSpec.program {
		return nil, false, fmt.Errorf("replay root executable differs across probes")
	}
	if !slices.Equal(baseSpec.opaque, leftSpec.opaque) || !slices.Equal(baseSpec.opaque, rightSpec.opaque) {
		return nil, false, fmt.Errorf("opaque replay argv tokens differ across probes")
	}

	mergedKeyed, keyedChanged, err := mergeReplayAssignments(baseSpec.keyed, leftSpec.keyed, rightSpec.keyed)
	if err != nil {
		return nil, false, err
	}
	mergedAdditive, additiveChanged, err := mergeReplayAdditive(baseSpec.additive, leftSpec.additive, rightSpec.additive)
	if err != nil {
		return nil, false, err
	}

	merged := make([]string, 0, 1+len(baseSpec.opaque)+len(mergedKeyed)+len(mergedAdditive))
	merged = append(merged, baseSpec.program)
	merged = append(merged, baseSpec.opaque...)
	keyOrder := mergedAssignmentOrder(baseSpec.keyedOrder, leftSpec.keyedOrder, rightSpec.keyedOrder)
	for _, key := range keyOrder {
		value, ok := mergedKeyed[key]
		if !ok {
			continue
		}
		merged = append(merged, key+"="+value)
	}
	additiveOrder := mergedAssignmentOrder(baseSpec.additiveOrder, leftSpec.additiveOrder, rightSpec.additiveOrder)
	for _, token := range additiveOrder {
		if _, ok := mergedAdditive[token]; ok {
			merged = append(merged, token)
		}
	}
	return merged, keyedChanged || additiveChanged, nil
}

func parseReplayArgv(argv []string) (replayParsedArgv, bool) {
	if len(argv) == 0 {
		return replayParsedArgv{}, false
	}
	spec := replayParsedArgv{
		program:  argv[0],
		keyed:    make(map[string]string),
		additive: make(map[string]struct{}),
	}
	for _, token := range argv[1:] {
		if key, value, ok := parseReplayKeyedToken(token); ok {
			if _, exists := spec.keyed[key]; exists {
				return replayParsedArgv{}, false
			}
			spec.keyed[key] = value
			spec.keyedOrder = append(spec.keyedOrder, key)
			continue
		}
		if isReplayAdditiveToken(token) {
			if _, exists := spec.additive[token]; !exists {
				spec.additive[token] = struct{}{}
				spec.additiveOrder = append(spec.additiveOrder, token)
			}
			continue
		}
		spec.opaque = append(spec.opaque, token)
	}
	return spec, true
}

func parseReplayKeyedToken(token string) (string, string, bool) {
	switch {
	case strings.HasPrefix(token, "--") && strings.Contains(token, "="):
		key, value, _ := strings.Cut(token, "=")
		return key, value, true
	case strings.HasPrefix(token, "-D") && strings.Contains(token[2:], "="):
		key, value, _ := strings.Cut(token, "=")
		return key, value, true
	}
	key, value, ok := strings.Cut(token, "=")
	if !ok || key == "" || !replayEnvKeyRE.MatchString(key) {
		return "", "", false
	}
	return key, value, true
}

func isReplayAdditiveToken(token string) bool {
	return strings.HasPrefix(token, "--with-") && !strings.Contains(token, "=")
}

func parseReplayEnvSpec(env []string) (replayEnvSpec, bool) {
	spec := replayEnvSpec{
		order:  make([]string, 0, len(env)),
		values: make(map[string]string, len(env)),
	}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" || !replayEnvKeyRE.MatchString(key) {
			return replayEnvSpec{}, false
		}
		if _, exists := spec.values[key]; exists {
			return replayEnvSpec{}, false
		}
		spec.values[key] = value
		spec.order = append(spec.order, key)
	}
	return spec, true
}

func mergeReplayEnv(base, left, right []string) ([]string, bool, error) {
	baseSpec, ok := parseReplayEnvSpec(base)
	if !ok {
		return nil, false, fmt.Errorf("unmergeable base environment")
	}
	leftSpec, ok := parseReplayEnvSpec(left)
	if !ok {
		return nil, false, fmt.Errorf("unmergeable left environment")
	}
	rightSpec, ok := parseReplayEnvSpec(right)
	if !ok {
		return nil, false, fmt.Errorf("unmergeable right environment")
	}
	mergedValues, changed, err := mergeReplayAssignments(baseSpec.values, leftSpec.values, rightSpec.values)
	if err != nil {
		return nil, false, err
	}
	order := mergedAssignmentOrder(baseSpec.order, leftSpec.order, rightSpec.order)
	merged := make([]string, 0, len(mergedValues))
	for _, key := range order {
		value, ok := mergedValues[key]
		if !ok {
			continue
		}
		merged = append(merged, key+"="+value)
	}
	return merged, changed, nil
}

func mergeReplayAssignments(base, left, right map[string]string) (map[string]string, bool, error) {
	keys := make(map[string]struct{}, len(base)+len(left)+len(right))
	for key := range base {
		keys[key] = struct{}{}
	}
	for key := range left {
		keys[key] = struct{}{}
	}
	for key := range right {
		keys[key] = struct{}{}
	}
	merged := make(map[string]string, len(keys))
	changed := false
	for _, key := range slices.Collect(maps.Keys(keys)) {
		baseValue, baseOK := base[key]
		leftValue, leftOK := left[key]
		rightValue, rightOK := right[key]
		value, present, localChanged, ok := mergeReplayState(baseValue, baseOK, leftValue, leftOK, rightValue, rightOK)
		if !ok {
			return nil, false, fmt.Errorf("conflicting replay parameter for %q", key)
		}
		if present {
			merged[key] = value
		}
		changed = changed || localChanged
	}
	return merged, changed, nil
}

func mergeReplayAdditive(base, left, right map[string]struct{}) (map[string]struct{}, bool, error) {
	merged := make(map[string]struct{}, len(base)+len(left)+len(right))
	for token := range base {
		merged[token] = struct{}{}
	}
	changed := false
	for token := range left {
		merged[token] = struct{}{}
		if _, ok := base[token]; !ok {
			changed = true
		}
	}
	for token := range right {
		merged[token] = struct{}{}
		if _, ok := base[token]; !ok {
			changed = true
		}
	}
	for token := range base {
		if _, ok := left[token]; !ok {
			return nil, false, fmt.Errorf("left replay root removed additive token %q", token)
		}
		if _, ok := right[token]; !ok {
			return nil, false, fmt.Errorf("right replay root removed additive token %q", token)
		}
	}
	return merged, changed, nil
}

func mergeReplayState(baseValue string, baseOK bool, leftValue string, leftOK bool, rightValue string, rightOK bool) (string, bool, bool, bool) {
	leftChanged := leftOK != baseOK || leftValue != baseValue
	rightChanged := rightOK != baseOK || rightValue != baseValue
	switch {
	case !leftChanged && !rightChanged:
		return baseValue, baseOK, false, true
	case leftChanged && !rightChanged:
		return leftValue, leftOK, true, true
	case !leftChanged && rightChanged:
		return rightValue, rightOK, true, true
	case leftOK == rightOK && leftValue == rightValue:
		return leftValue, leftOK, true, true
	default:
		return "", false, false, false
	}
}

func mergedAssignmentOrder(base, left, right []string) []string {
	seen := make(map[string]struct{}, len(base)+len(left)+len(right))
	order := make([]string, 0, len(base)+len(left)+len(right))
	for _, values := range [][]string{base, left, right} {
		for _, value := range values {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			order = append(order, value)
		}
	}
	return order
}

func cloneReplaySource(srcRoot string) (string, func(), error) {
	dstRoot, err := os.MkdirTemp("", "llar-replay-src-*")
	if err != nil {
		return "", nil, err
	}
	if err := copyTreePreserveLinks(srcRoot, dstRoot); err != nil {
		_ = os.RemoveAll(dstRoot)
		return "", nil, err
	}
	return dstRoot, func() { _ = os.RemoveAll(dstRoot) }, nil
}

func cloneReplayOutput(baseOutputDir string) (string, func(), error) {
	dstRoot, err := os.MkdirTemp("", "llar-replay-out-*")
	if err != nil {
		return "", nil, err
	}
	if err := copyTreePreserveLinks(baseOutputDir, dstRoot); err != nil {
		_ = os.RemoveAll(dstRoot)
		return "", nil, err
	}
	return dstRoot, func() { _ = os.RemoveAll(dstRoot) }, nil
}

func deriveReplayBuildRoot(scope trace.Scope, sourceRoot string) (string, func(), error) {
	if scope.BuildRoot == "" {
		return filepath.Join(sourceRoot, "_build"), func() {}, nil
	}
	rel, err := filepath.Rel(scope.SourceRoot, scope.BuildRoot)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Join(sourceRoot, rel), func() {}, nil
	}
	buildRoot, err := os.MkdirTemp("", "llar-replay-build-*")
	if err != nil {
		return "", nil, err
	}
	return buildRoot, func() { _ = os.RemoveAll(buildRoot) }, nil
}

func prepareReplayBuildRoot(buildRoot string, steps []replayRoot) error {
	if buildRoot == "" {
		return nil
	}
	if err := os.RemoveAll(buildRoot); err != nil {
		return err
	}
	return os.MkdirAll(buildRoot, 0o755)
}

func replayHasBuildRootInitializer(steps []replayRoot) bool {
	for _, step := range steps {
		if replayStepInitializesBuildRoot(step) {
			return true
		}
	}
	return false
}

func replayStepInitializesBuildRoot(step replayRoot) bool {
	writesBuild := false
	for _, path := range step.writes {
		if isReplayBuildPath(path) {
			writesBuild = true
			break
		}
	}
	if !writesBuild {
		return false
	}
	for _, path := range step.reads {
		if isReplayBuildPath(path) {
			return false
		}
	}
	return true
}

func isReplayBuildPath(path string) bool {
	path = normalizePath(path)
	return path == "$BUILD" || strings.HasPrefix(path, "$BUILD/")
}

func runReplayStep(ctx context.Context, step replayRoot, paths replayPaths) error {
	cwd := materializeReplayToken(step.cwd, paths)
	argv := materializeReplayTokens(step.argv, paths)
	env := materializeReplayTokens(step.env, paths)
	if len(argv) == 0 {
		return fmt.Errorf("empty replay argv")
	}
	if strings.Contains(argv[0], "/") && !filepath.IsAbs(argv[0]) {
		argv[0] = filepath.Join(cwd, filepath.FromSlash(argv[0]))
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = filepath.Clean(filepath.FromSlash(cwd))
	if len(env) > 0 {
		cmd.Env = env
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(out.String())
		if detail == "" {
			return fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
		}
		return fmt.Errorf("%s: %w\n%s", strings.Join(argv, " "), err, detail)
	}
	return nil
}

func materializeReplayTokens(tokens []string, paths replayPaths) []string {
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, materializeReplayToken(token, paths))
	}
	return out
}

func materializeReplayToken(token string, paths replayPaths) string {
	token = strings.ReplaceAll(token, "$BUILD", paths.buildRoot)
	token = strings.ReplaceAll(token, "$INSTALL", paths.installRoot)
	token = strings.ReplaceAll(token, "$SRC", paths.sourceRoot)
	return filepath.FromSlash(token)
}

func replayIssue(kind OutputMergeIssueKind, reason, detail string) OutputSynthesisIssue {
	return OutputSynthesisIssue{
		Kind:   kind,
		Path:   "<root-replay>",
		Reason: reason,
		Detail: detail,
	}
}

func copyTreePreserveLinks(srcRoot, dstRoot string) error {
	return filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dstPath := filepath.Join(dstRoot, rel)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		switch mode := info.Mode(); {
		case mode.IsDir():
			return os.MkdirAll(dstPath, mode.Perm())
		case mode&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
				return err
			}
			return os.Symlink(target, dstPath)
		case mode.IsRegular():
			if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
				return err
			}
			return copyRegularFile(path, dstPath, mode.Perm())
		default:
			return nil
		}
	})
}

func copyRegularFile(srcPath, dstPath string, perm fs.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}
