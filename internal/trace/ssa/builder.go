package ssa

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/goplus/llar/internal/trace"
)

var (
	reBuildTmpPIDNoise = regexp.MustCompile(`\.tmp\.[0-9]+$`)
)

const (
	buildTransientDirToken = "$TMPDIR"
	buildGeneratedIDToken  = "$ID"
)

type BuildInput struct {
	Records      []trace.Record
	Events       []trace.Event
	Scope        trace.Scope
	InputDigests map[string]string
}

func BuildGraph(input BuildInput) Graph {
	scope := normalizeScope(input.Scope)
	var obs observation
	source := SourceRecords
	if len(input.Events) != 0 {
		source = SourceEvents
		obs = observationFromEvents(input.Events, input.Records, scope, input.InputDigests)
	} else {
		obs = observationFromRecords(input.Records, scope, input.InputDigests)
	}

	graph := buildFromObservation(obs)
	graph.Source = source
	graph.Records = len(obs.Nodes)
	graph.Events = len(input.Events)
	graph.Scope = scope
	graph.InputDigests = normalizeInputDigests(input.InputDigests)
	graph.RawRecords = slices.Clone(input.Records)
	graph.RawEvents = slices.Clone(input.Events)
	graph.Actions = cloneNodes(graph.Nodes)
	graph.ParentAction = slices.Clone(graph.Parent)
	graph.Out = cloneDeps(graph.Deps)
	graph.In = invertDeps(graph.Deps)
	graph.Indeg = edgeCounts(graph.In)
	graph.Outdeg = edgeCounts(graph.Out)
	graph.RawPaths = clonePaths(graph.Paths)
	return graph
}

func invertDeps(out [][]ExecEdge) [][]ExecEdge {
	if len(out) == 0 {
		return nil
	}
	in := make([][]ExecEdge, len(out))
	for from, edges := range out {
		for _, edge := range edges {
			if edge.To < 0 || edge.To >= len(in) {
				continue
			}
			in[edge.To] = append(in[edge.To], ExecEdge{From: from, To: edge.To, Path: edge.Path})
		}
	}
	return in
}

func edgeCounts(edges [][]ExecEdge) []int {
	if len(edges) == 0 {
		return nil
	}
	out := make([]int, len(edges))
	for i := range edges {
		out[i] = len(edges[i])
	}
	return out
}

func normalizeInputDigests(inputDigests map[string]string) map[string]string {
	if len(inputDigests) == 0 {
		return nil
	}
	out := make(map[string]string, len(inputDigests))
	for path, sum := range inputDigests {
		normalized := normalizePath(path)
		if normalized == "" {
			continue
		}
		out[normalized] = sum
	}
	return out
}

func buildExecNode(record normalizedRecord, scope trace.Scope, inputDigests map[string]string) ExecNode {
	kind := classifyActionKindWithScope(record, scope)
	tool := ""
	if len(record.argv) != 0 {
		tool = filepath.Base(record.argv[0])
	}
	return ExecNode{
		PID:          record.pid,
		ParentPID:    record.parentPID,
		Argv:         slices.Clone(record.argv),
		Cwd:          record.cwd,
		Env:          normalizeEnvEntries(record.env, scope),
		Reads:        slices.Clone(record.inputs),
		ReadMisses:   slices.Clone(record.readMisses),
		Writes:       slices.Clone(record.changes),
		Deletes:      slices.Clone(record.deletions),
		ExecPath:     resolveExecPath(record.cwd, record.argv),
		Tool:         tool,
		Kind:         kind,
		ActionKey:    buildActionKey(kind, tool, record, scope),
		StructureKey: scopedStructureKey(record, scope),
		Fingerprint:  scopedFingerprint(record, scope, inputDigests),
	}
}

func classifyActionKindWithScope(record normalizedRecord, scope trace.Scope) ActionKind {
	if len(record.argv) == 0 {
		return KindGeneric
	}

	tool := filepath.Base(record.argv[0])
	switch {
	case tool == "cp":
		if len(record.inputs) != 0 && len(record.changes) != 0 {
			return KindCopy
		}
	case tool == "install":
		if len(record.changes) != 0 {
			return KindInstall
		}
	case tool == "cmake" && len(record.argv) > 1 && record.argv[1] == "--install":
		if len(record.changes) != 0 {
			return KindInstall
		}
	case tool == "cmake" || tool == "ninja" || tool == "make" || tool == "gmake":
		return KindConfigure
	}

	if kind, ok := classifyScriptWrapperAction(record, scope); ok {
		return kind
	}
	return KindGeneric
}

func classifyScriptWrapperAction(record normalizedRecord, scope trace.Scope) (ActionKind, bool) {
	if len(record.argv) == 0 {
		return KindGeneric, false
	}
	switch filepath.Base(record.argv[0]) {
	case "perl", "sh", "bash", "dash":
	default:
		return KindGeneric, false
	}

	for _, path := range record.changes {
		if isExplicitDeliveryPath(path, scope) {
			return KindInstall, true
		}
	}
	if scriptWrapperLooksConfigureLike(record, scope) {
		return KindConfigure, true
	}
	return KindGeneric, false
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

func buildActionKey(kind ActionKind, tool string, record normalizedRecord, scope trace.Scope) string {
	switch kind {
	case KindCopy:
		for _, dst := range record.changes {
			if dst == "" {
				continue
			}
			return "copy|" + tool + "|dst=" + normalizeScopeToken(dst, scope)
		}
	case KindInstall:
		for _, dst := range record.changes {
			if dst == "" {
				continue
			}
			return "install|" + tool + "|dst=" + normalizeScopeToken(dst, scope)
		}
	case KindConfigure:
		return "configure|" + tool + "|cwd=" + normalizeScopeToken(record.cwd, scope) + "|argv=" + argvSkeletonScoped(record.argv, scope)
	}
	return "generic|" + tool + "|cwd=" + normalizeScopeToken(record.cwd, scope) + "|argv=" + argvFullScoped(record.argv, scope)
}

func scopedFingerprint(record normalizedRecord, scope trace.Scope, inputDigests map[string]string) string {
	argv := make([]string, 0, len(record.argv))
	for _, arg := range record.argv {
		argv = append(argv, normalizeScopeToken(arg, scope))
	}
	env := normalizeEnvEntries(record.env, scope)
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
	parts = append(parts, env...)
	parts = append(parts, "@")
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
	env := normalizeEnvEntries(record.env, scope)
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
	parts = append(parts, env...)
	parts = append(parts, "@")
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

const envNamespacePrefix = "$ENV/"

func envStatePath(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return envNamespacePrefix + name
}

func envStatePathFromEntry(entry string) string {
	name, _, ok := strings.Cut(entry, "=")
	if !ok {
		return ""
	}
	return envStatePath(name)
}

func normalizeEnvEntries(env []string, scope trace.Scope) []string {
	if len(env) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || ignoredExecEnvKey(key) {
			continue
		}
		normalized = append(normalized, key+"="+normalizeScopeToken(value, scope))
	}
	return uniqueSorted(normalized)
}

func ignoredExecEnvKey(key string) bool {
	switch key {
	case "_", "OLDPWD", "PWD", "SHLVL", "TERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION", "COLORTERM":
		return true
	default:
		return false
	}
}

func isExplicitDeliveryPath(path string, scope trace.Scope) bool {
	root := strings.TrimSuffix(normalizePath(scope.InstallRoot), "/")
	if root == "" {
		return false
	}
	path = normalizePath(path)
	return path == root || strings.HasPrefix(path, root+"/")
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
