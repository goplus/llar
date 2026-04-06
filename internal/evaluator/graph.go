package evaluator

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/goplus/llar/internal/trace"
)

type actionKind uint8

const (
	kindGeneric actionKind = iota
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
	pid          int64
	parentPID    int64
	argv         []string
	cwd          string
	reads        []string
	writes       []string
	deletes      []string
	execPath     string
	tool         string
	kind         actionKind
	actionKey    string
	structureKey string
	fingerprint  string
}

type actionGraph struct {
	source       graphObservationSource
	records      int
	events       int
	scope        trace.Scope
	actions      []actionNode
	parentAction []int
	out          [][]graphEdge
	in           [][]graphEdge
	indeg        []int
	outdeg       []int
	tooling      []bool
	probe        []bool
	mainline     []bool
	rawPaths     map[string]pathFacts
	paths        map[string]pathFacts
}

type pathRole uint8

const (
	roleTooling pathRole = iota
	rolePropagating
	roleDelivery
)

type pathFacts struct {
	path    string
	writers []int
	readers []int
	role    pathRole
}

func buildGraph(records []trace.Record) actionGraph {
	return buildGraphWithScope(records, trace.Scope{})
}

func buildGraphWithScope(records []trace.Record, scope trace.Scope) actionGraph {
	return buildGraphWithScopeAndDigests(records, scope, nil)
}

func buildGraphWithScopeAndDigests(records []trace.Record, scope trace.Scope, inputDigests map[string]string) actionGraph {
	return buildGraphFromObservation(buildObservationFromRecords(records, inputDigests), scope)
}

func buildGraphWithEvents(events []trace.Event) actionGraph {
	return buildGraphWithEventsAndDigests(events, trace.Scope{}, nil)
}

func buildGraphWithEventsAndDigests(events []trace.Event, scope trace.Scope, inputDigests map[string]string) actionGraph {
	return buildGraphFromObservation(buildObservationFromEvents(events, nil, inputDigests), scope)
}

func buildGraphFromObservation(observation graphObservation, scope trace.Scope) actionGraph {
	raw := buildRawGraphFromObservation(observation, scope)
	return classifyGraphRoles(raw)
}

func buildRawGraphFromObservation(observation graphObservation, scope trace.Scope) actionGraph {
	scope = normalizeScope(scope)
	directories := inferDirectoryLikePaths(observation.actions)
	actions := make([]actionNode, 0, len(observation.actions))
	for _, record := range observation.actions {
		filtered := filterDirectoryPaths(record, directories)
		kind := classifyActionKindWithScope(filtered, scope)
		actions = append(actions, buildActionNode(filtered, kind, scope, observation.inputDigests))
	}

	out := make([][]graphEdge, len(actions))
	in := make([][]graphEdge, len(actions))
	indeg := make([]int, len(actions))
	outdeg := make([]int, len(actions))
	rawPaths := make(map[string]pathFacts)

	lastWriter := make(map[string]int)
	for i, action := range actions {
		for _, read := range action.reads {
			facts := rawPaths[read]
			facts.path = read
			facts.readers = append(facts.readers, i)
			rawPaths[read] = facts
		}
		for _, write := range action.writes {
			facts := rawPaths[write]
			facts.path = write
			facts.writers = append(facts.writers, i)
			rawPaths[write] = facts
		}
		for _, read := range action.reads {
			writer, ok := lastWriter[read]
			if !ok {
				continue
			}
			edge := graphEdge{from: writer, to: i, path: read}
			out[writer] = append(out[writer], edge)
			in[i] = append(in[i], edge)
			outdeg[writer]++
			indeg[i]++
		}
		for _, write := range action.writes {
			lastWriter[write] = i
		}
	}

	parentAction := make([]int, len(actions))
	for i := range parentAction {
		parentAction[i] = -1
	}
	lastByPID := make(map[int64]int, len(actions))
	for i, action := range actions {
		if action.parentPID != 0 {
			if idx, ok := lastByPID[action.parentPID]; ok {
				parentAction[i] = idx
			}
		}
		if action.pid != 0 {
			lastByPID[action.pid] = i
		}
	}

	return actionGraph{
		source:       observation.source,
		records:      observation.records,
		events:       observation.events,
		scope:        scope,
		actions:      actions,
		parentAction: parentAction,
		out:          out,
		in:           in,
		indeg:        indeg,
		outdeg:       outdeg,
		rawPaths:     rawPaths,
	}
}

func clonePathFactsMap(src map[string]pathFacts) map[string]pathFacts {
	if len(src) == 0 {
		return map[string]pathFacts{}
	}
	out := make(map[string]pathFacts, len(src))
	for path, facts := range src {
		cloned := facts
		cloned.writers = slices.Clone(facts.writers)
		cloned.readers = slices.Clone(facts.readers)
		out[path] = cloned
	}
	return out
}

func isProbeOnlyNoisePath(graph actionGraph, path string) bool {
	if path == "" || isExplicitDeliveryPath(path, graph.scope) {
		return false
	}
	facts, ok := graph.paths[path]
	if !ok {
		return false
	}
	if facts.role == roleTooling {
		return true
	}
	sawEndpoint := false
	for _, idx := range facts.writers {
		if !actionIsProbeOrTooling(graph, idx) {
			return false
		}
		sawEndpoint = true
	}
	for _, idx := range facts.readers {
		if !actionIsProbeOrTooling(graph, idx) {
			return false
		}
		sawEndpoint = true
	}
	return sawEndpoint
}

func actionIsProbeOrTooling(graph actionGraph, idx int) bool {
	if idx < 0 || idx >= len(graph.actions) {
		return false
	}
	if idx < len(graph.tooling) && graph.tooling[idx] {
		return true
	}
	return idx < len(graph.probe) && graph.probe[idx]
}

func buildActionNode(record normalizedRecord, kind actionKind, scope trace.Scope, inputDigests map[string]string) actionNode {
	fingerprint := scopedFingerprint(record, scope, inputDigests)
	tool := ""
	if len(record.argv) != 0 {
		tool = filepath.Base(record.argv[0])
	}
	return actionNode{
		pid:          record.pid,
		parentPID:    record.parentPID,
		argv:         record.argv,
		cwd:          record.cwd,
		reads:        record.inputs,
		writes:       record.changes,
		deletes:      record.deletions,
		execPath:     resolveExecPath(record.cwd, record.argv),
		tool:         tool,
		kind:         kind,
		actionKey:    buildActionKey(kind, tool, record, scope),
		structureKey: scopedStructureKey(record, scope),
		fingerprint:  fingerprint,
	}
}

func classifyActionKindWithScope(record normalizedRecord, scope trace.Scope) actionKind {
	if len(record.argv) == 0 {
		return kindGeneric
	}

	tool := filepath.Base(record.argv[0])
	switch {
	case tool == "cp":
		if len(record.inputs) != 0 && len(record.changes) != 0 {
			return kindCopy
		}
	case tool == "install":
		if len(record.changes) != 0 {
			return kindInstall
		}
	case tool == "cmake" && len(record.argv) > 1 && record.argv[1] == "--install":
		if len(record.changes) != 0 {
			return kindInstall
		}
	case tool == "cmake" || tool == "ninja" || tool == "make" || tool == "gmake":
		return kindConfigure
	}

	if kind, ok := classifyScriptWrapperAction(record, scope); ok {
		return kind
	}
	return kindGeneric
}

func classifyScriptWrapperAction(record normalizedRecord, scope trace.Scope) (actionKind, bool) {
	if len(record.argv) == 0 {
		return kindGeneric, false
	}
	switch filepath.Base(record.argv[0]) {
	case "perl", "sh", "bash", "dash":
	default:
		return kindGeneric, false
	}

	for _, path := range record.changes {
		if isExplicitDeliveryPath(path, scope) {
			return kindInstall, true
		}
	}
	if scriptWrapperLooksConfigureLike(record, scope) {
		return kindConfigure, true
	}
	return kindGeneric, false
}

func scriptWrapperLooksConfigureLike(record normalizedRecord, scope trace.Scope) bool {
	if len(record.changes) == 0 {
		return false
	}
	for _, path := range record.changes {
		if isExplicitDeliveryPath(path, scope) || isArtifactPath(path) || pathLooksLikeCompilationInput(path) {
			return false
		}
	}
	for _, path := range record.inputs {
		if isExplicitDeliveryPath(path, scope) {
			return false
		}
	}
	return true
}

func buildActionKey(kind actionKind, tool string, record normalizedRecord, scope trace.Scope) string {
	switch kind {
	case kindCopy:
		for _, dst := range record.changes {
			if dst == "" {
				continue
			}
			return "copy|" + tool + "|dst=" + normalizeScopeToken(dst, scope)
		}
	case kindInstall:
		for _, dst := range record.changes {
			if dst == "" {
				continue
			}
			return "install|" + tool + "|dst=" + normalizeScopeToken(dst, scope)
		}
	case kindConfigure:
		return "configure|" + tool + "|cwd=" + normalizeScopeToken(record.cwd, scope) + "|argv=" + argvSkeletonScoped(record.argv, scope)
	}
	return "generic|" + tool + "|cwd=" + normalizeScopeToken(record.cwd, scope) + "|argv=" + argvFullScoped(record.argv, scope)
}

func scopedFingerprint(record normalizedRecord, scope trace.Scope, inputDigests map[string]string) string {
	argv := make([]string, 0, len(record.argv))
	for _, arg := range record.argv {
		argv = append(argv, normalizeScopeToken(arg, scope))
	}
	inputs := make([]string, 0, len(record.inputs))
	for _, path := range record.inputs {
		inputs = append(inputs, fingerprintInputToken(path, record.inputOrigin[path], scope, inputDigests))
	}
	changes := make([]string, 0, len(record.changes))
	for _, path := range fingerprintChanges(record) {
		changes = append(changes, normalizeScopeToken(path, scope))
	}
	parts := append([]string{}, argv...)
	parts = append(parts, "@", normalizeScopeToken(record.cwd, scope), "@")
	parts = append(parts, inputs...)
	parts = append(parts, "@")
	parts = append(parts, changes...)
	parts = append(parts, "@")
	for _, path := range record.deletions {
		parts = append(parts, "!"+normalizeScopeToken(path, scope))
	}
	return strings.Join(parts, "\x1f")
}

func scopedStructureKey(record normalizedRecord, scope trace.Scope) string {
	argv := make([]string, 0, len(record.argv))
	for _, arg := range record.argv {
		argv = append(argv, normalizeScopeToken(arg, scope))
	}
	inputs := make([]string, 0, len(record.inputs))
	for _, path := range record.inputs {
		inputs = append(inputs, normalizeScopeToken(path, scope))
	}
	changes := make([]string, 0, len(record.changes))
	for _, path := range fingerprintChanges(record) {
		changes = append(changes, normalizeScopeToken(path, scope))
	}
	parts := append([]string{}, argv...)
	parts = append(parts, "@", normalizeScopeToken(record.cwd, scope), "@")
	parts = append(parts, inputs...)
	parts = append(parts, "@")
	parts = append(parts, changes...)
	parts = append(parts, "@")
	for _, path := range record.deletions {
		parts = append(parts, "!"+normalizeScopeToken(path, scope))
	}
	return strings.Join(parts, "\x1f")
}

func fingerprintInputToken(path, origin string, scope trace.Scope, inputDigests map[string]string) string {
	token := normalizeScopeToken(path, scope)
	if !shouldHashFingerprintInput(path, origin, scope, inputDigests) {
		return token
	}
	if sum, ok := inputDigests[origin]; ok && sum != "" {
		return token + "#" + sum
	}
	if origin == "" {
		return token + "#missing-origin"
	}
	return token + "#missing-digest"
}

func shouldHashFingerprintInput(path, origin string, scope trace.Scope, inputDigests map[string]string) bool {
	if len(inputDigests) == 0 || scope.BuildRoot == "" {
		return false
	}
	if origin == "" {
		origin = path
	}
	origin = normalizePath(origin)
	buildRoot := strings.TrimSuffix(normalizePath(scope.BuildRoot), "/")
	if buildRoot == "" {
		return false
	}
	return origin == buildRoot || strings.HasPrefix(origin, buildRoot+"/")
}

func fingerprintChanges(record normalizedRecord) []string {
	if len(record.argv) != 0 {
		switch filepath.Base(record.argv[0]) {
		case "ar", "ranlib":
			filtered := make([]string, 0, len(record.changes))
			for _, path := range record.changes {
				if isArchivePath(path) {
					filtered = append(filtered, path)
				}
			}
			if len(filtered) != 0 {
				return filtered
			}
		}
	}
	return record.changes
}

func normalizeScope(scope trace.Scope) trace.Scope {
	scope.SourceRoot = normalizeRootPath(scope.SourceRoot)
	scope.BuildRoot = normalizeRootPath(scope.BuildRoot)
	scope.InstallRoot = normalizeRootPath(scope.InstallRoot)
	scope.KeepRoots = slices.Clone(scope.KeepRoots)
	for i := range scope.KeepRoots {
		scope.KeepRoots[i] = normalizeRootPath(scope.KeepRoots[i])
	}
	return scope
}

func normalizeRootPath(path string) string {
	path = normalizePath(path)
	if path == "" {
		return ""
	}
	return strings.TrimSuffix(path, "/")
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

func argvSkeleton(argv []string) string {
	return argvSkeletonScoped(argv, trace.Scope{})
}

func argvSkeletonScoped(argv []string, scope trace.Scope) string {
	if len(argv) == 0 {
		return ""
	}
	limit := min(len(argv), 4)
	out := make([]string, 0, limit)
	for _, arg := range argv[:limit] {
		out = append(out, normalizeScopeToken(arg, scope))
	}
	return strings.Join(out, " ")
}

func argvFullScoped(argv []string, scope trace.Scope) string {
	if len(argv) == 0 {
		return ""
	}
	out := make([]string, 0, len(argv))
	for _, arg := range argv {
		out = append(out, normalizeScopeToken(arg, scope))
	}
	return strings.Join(out, " ")
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

func filterDirectoryPaths(record normalizedRecord, directories map[string]struct{}) normalizedRecord {
	if len(directories) == 0 {
		return record
	}
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
	return record
}

func pathLooksLikeCompilationInput(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".c", ".cc", ".cpp", ".cxx", ".h", ".hh", ".hpp", ".hxx", ".inc", ".inl", ".s", ".S", ".asm":
		return true
	default:
		return false
	}
}

func isArtifactPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".o", ".obj", ".a", ".so", ".dylib", ".dll", ".exe":
		return true
	default:
		return false
	}
}

func isArchivePath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".a")
}

func (kind actionKind) String() string {
	switch kind {
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

func (role pathRole) String() string {
	switch role {
	case roleTooling:
		return "tooling"
	case rolePropagating:
		return "propagating"
	case roleDelivery:
		return "delivery"
	default:
		return "propagating"
	}
}
