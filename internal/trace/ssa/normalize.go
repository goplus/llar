package ssa

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/goplus/llar/internal/trace"
)

var (
	reTmpUnix = regexp.MustCompile(`^/tmp/[^/]+`)
	reTmpMac  = regexp.MustCompile(`^/var/folders/[^/]+/[^/]+/[^/]+`)
)

type normalizedRecord struct {
	pid         int64
	parentPID   int64
	argv        []string
	cwd         string
	env         []string
	inputs      []string
	readMisses  []string
	changes     []string
	deletions   []string
	inputOrigin map[string]string
}

func observationFromRecords(records []trace.Record, scope trace.Scope, inputDigests map[string]string) observation {
	normalized := make([]normalizedRecord, 0, len(records))
	for _, record := range records {
		normalized = append(normalized, normalizeRecordWithFacts(record, nil, nil))
	}
	return observationFromNormalized(normalized, scope, inputDigests)
}

func observationFromEvents(events []trace.Event, fallbackRecords []trace.Record, scope trace.Scope, inputDigests map[string]string) observation {
	detailed := recordsFromEventsDetailed(events, fallbackRecords)
	normalized := make([]normalizedRecord, 0, len(detailed))
	for _, record := range detailed {
		normalized = append(normalized, normalizeRecordWithFacts(record.record, record.deletions, record.readMisses))
	}
	return observationFromNormalized(normalized, scope, inputDigests)
}

func observationFromNormalized(records []normalizedRecord, scope trace.Scope, inputDigests map[string]string) observation {
	directories := inferDirectoryLikePaths(records)
	nodes := make([]ExecNode, 0, len(records))
	for _, record := range records {
		filtered := filterDirectoryPaths(record, directories)
		nodes = append(nodes, buildExecNode(filtered, scope, inputDigests))
	}

	deps := make([][]ExecEdge, len(nodes))
	paths := make(map[string]PathInfo)
	lastWriter := make(map[string]int)
	for i, node := range nodes {
		for _, entry := range node.Env {
			path := envStatePathFromEntry(entry)
			if path == "" {
				continue
			}
			facts := paths[path]
			facts.Path = path
			facts.Readers = append(facts.Readers, i)
			paths[path] = facts
		}
		for _, read := range node.Reads {
			facts := paths[read]
			facts.Path = read
			facts.Readers = append(facts.Readers, i)
			paths[read] = facts
		}
		for _, miss := range node.ReadMisses {
			facts := paths[miss]
			facts.Path = miss
			facts.Readers = append(facts.Readers, i)
			paths[miss] = facts
		}
		for _, write := range node.Writes {
			facts := paths[write]
			facts.Path = write
			facts.Writers = append(facts.Writers, i)
			paths[write] = facts
		}
		for _, read := range node.Reads {
			writer, ok := lastWriter[read]
			if !ok {
				continue
			}
			deps[writer] = append(deps[writer], ExecEdge{From: writer, To: i, Path: read})
		}
		for _, write := range node.Writes {
			lastWriter[write] = i
		}
	}

	parent := make([]int, len(nodes))
	for i := range parent {
		parent[i] = -1
	}
	lastByPID := make(map[int64]int, len(nodes))
	for i, node := range nodes {
		if node.ParentPID != 0 {
			if idx, ok := lastByPID[node.ParentPID]; ok {
				parent[i] = idx
			}
		}
		if node.PID != 0 {
			lastByPID[node.PID] = i
		}
	}

	return observation{
		Nodes:  nodes,
		Parent: parent,
		Paths:  paths,
		Deps:   deps,
	}
}

type eventRecord struct {
	record     trace.Record
	deletions  []string
	readMisses []string
}

type eventProcState struct {
	parentPID int64
	cwd       string
	current   *eventRecord
}

type rawExecKey struct {
	pid  int64
	cwd  string
	argv string
}

func recordsFromEventsDetailed(events []trace.Event, fallbackRecords []trace.Record) []eventRecord {
	states := map[int64]*eventProcState{}
	ordered := make([]*eventRecord, 0, len(events))
	rawRecords := indexRawExecRecords(fallbackRecords)

	stateOf := func(pid int64) *eventProcState {
		if state, ok := states[pid]; ok {
			return state
		}
		state := &eventProcState{}
		states[pid] = state
		return state
	}

	for _, event := range events {
		if event.PID == 0 {
			continue
		}
		state := stateOf(event.PID)
		if event.ParentPID != 0 && state.parentPID == 0 {
			state.parentPID = event.ParentPID
		}
		switch event.Kind {
		case trace.EventClone:
			if event.ChildPID == 0 {
				continue
			}
			child := stateOf(event.ChildPID)
			if child.parentPID == 0 {
				child.parentPID = event.PID
			}
			if child.cwd == "" {
				child.cwd = firstNonEmpty(event.Cwd, state.cwd)
			}
		case trace.EventChdir:
			state.cwd = event.Path
		case trace.EventExec:
			cwd := firstNonEmpty(event.Cwd, state.cwd)
			rawRecord, hasRawRecord := consumeRawExecRecord(rawRecords, event.PID, cwd, event.Argv)
			parentPID := firstNonZero(event.ParentPID, state.parentPID)
			if hasRawRecord {
				parentPID = firstNonZero(parentPID, rawRecord.ParentPID)
			}
			if parentPID != 0 {
				if parent, ok := states[parentPID]; ok && parent.current != nil && shouldCollapseExecIntoParent(parent.current.record.Argv, event.Argv) {
					state.parentPID = parentPID
					state.cwd = cwd
					if hasRawRecord {
						mergeFallbackExecRecord(&parent.current.record, rawRecord)
					}
					state.current = parent.current
					continue
				}
			}
			rec := &eventRecord{
				record: trace.Record{
					PID:       event.PID,
					ParentPID: parentPID,
					Argv:      slices.Clone(event.Argv),
					Cwd:       cwd,
				},
			}
			if hasRawRecord {
				mergeFallbackExecRecord(&rec.record, rawRecord)
			}
			state.parentPID = rec.record.ParentPID
			state.cwd = cwd
			state.current = rec
			ordered = append(ordered, rec)
		case trace.EventRead:
			if state.current == nil {
				continue
			}
			path := event.Path
			if path == "" || slices.Contains(state.current.record.Inputs, path) {
				continue
			}
			state.current.record.Inputs = append(state.current.record.Inputs, path)
		case trace.EventReadMiss:
			if state.current == nil {
				continue
			}
			path := event.Path
			if path == "" || slices.Contains(state.current.readMisses, path) {
				continue
			}
			state.current.readMisses = append(state.current.readMisses, path)
		case trace.EventWrite:
			if state.current == nil {
				continue
			}
			path := event.Path
			if path == "" || slices.Contains(state.current.record.Changes, path) {
				continue
			}
			state.current.record.Changes = append(state.current.record.Changes, path)
		case trace.EventRename:
			if state.current == nil {
				continue
			}
			for _, path := range []string{event.RelatedPath, event.Path} {
				if path == "" || slices.Contains(state.current.record.Changes, path) {
					continue
				}
				state.current.record.Changes = append(state.current.record.Changes, path)
			}
			if event.RelatedPath != "" && !slices.Contains(state.current.deletions, event.RelatedPath) {
				state.current.deletions = append(state.current.deletions, event.RelatedPath)
			}
		case trace.EventUnlink, trace.EventMkdir, trace.EventSymlink:
			if state.current == nil {
				continue
			}
			path := event.Path
			if path == "" || slices.Contains(state.current.record.Changes, path) {
				continue
			}
			state.current.record.Changes = append(state.current.record.Changes, path)
			if event.Kind == trace.EventUnlink && !slices.Contains(state.current.deletions, path) {
				state.current.deletions = append(state.current.deletions, path)
			}
		}
	}

	out := make([]eventRecord, 0, len(ordered))
	for _, rec := range ordered {
		if rec == nil || len(rec.record.Argv) == 0 {
			continue
		}
		out = append(out, *rec)
	}
	return out
}

func indexRawExecRecords(records []trace.Record) map[rawExecKey][]trace.Record {
	if len(records) == 0 {
		return nil
	}
	byKey := make(map[rawExecKey][]trace.Record, len(records))
	for _, record := range records {
		if len(record.Argv) == 0 || record.PID == 0 {
			continue
		}
		key := rawExecKey{
			pid:  record.PID,
			cwd:  normalizePath(record.Cwd),
			argv: strings.Join(record.Argv, "\x1f"),
		}
		byKey[key] = append(byKey[key], record)
	}
	return byKey
}

func consumeRawExecRecord(records map[rawExecKey][]trace.Record, pid int64, cwd string, argv []string) (trace.Record, bool) {
	if len(records) == 0 || pid == 0 || len(argv) == 0 {
		return trace.Record{}, false
	}
	key := rawExecKey{
		pid:  pid,
		cwd:  normalizePath(cwd),
		argv: strings.Join(argv, "\x1f"),
	}
	queue := records[key]
	if len(queue) == 0 {
		return trace.Record{}, false
	}
	record := queue[0]
	if len(queue) == 1 {
		delete(records, key)
	} else {
		records[key] = queue[1:]
	}
	return record, true
}

func mergeFallbackExecRecord(dst *trace.Record, fallback trace.Record) {
	if dst == nil {
		return
	}
	if dst.ParentPID == 0 {
		dst.ParentPID = fallback.ParentPID
	}
	if dst.Cwd == "" {
		dst.Cwd = fallback.Cwd
	}
	if len(dst.Env) == 0 && len(fallback.Env) != 0 {
		dst.Env = slices.Clone(fallback.Env)
	}
	for _, path := range fallback.Inputs {
		if path == "" || slices.Contains(dst.Inputs, path) {
			continue
		}
		dst.Inputs = append(dst.Inputs, path)
	}
	for _, path := range fallback.Changes {
		if path == "" || slices.Contains(dst.Changes, path) {
			continue
		}
		dst.Changes = append(dst.Changes, path)
	}
}

func shouldCollapseExecIntoParent(parentArgv, childArgv []string) bool {
	if !isCompilerDriverArgv(parentArgv) || len(childArgv) == 0 {
		return false
	}
	switch filepath.Base(childArgv[0]) {
	case "cc1", "cc1plus", "as":
		return true
	default:
		return false
	}
}

func isCompilerDriverArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	switch filepath.Base(argv[0]) {
	case "cc", "c++", "gcc", "g++", "clang", "clang++":
		return true
	default:
		return false
	}
}

func normalizeRecordWithFacts(record trace.Record, deletions, readMisses []string) normalizedRecord {
	normalizer := resolveNormalizer(record)
	argv, cwd, inputs, changes := normalizer.normalize(record)
	inputOrigin := make(map[string]string, len(record.Inputs))
	for _, path := range record.Inputs {
		normalized := normalizePath(path)
		if normalized == "" {
			continue
		}
		if _, ok := inputOrigin[normalized]; !ok {
			inputOrigin[normalized] = path
		}
	}
	inputs = uniqueSorted(inputs)
	normalizedMisses := make([]string, 0, len(readMisses))
	for _, path := range readMisses {
		normalizedMisses = append(normalizedMisses, normalizePath(path))
	}
	normalizedMisses = uniqueSorted(normalizedMisses)
	changes = uniqueSorted(changes)
	normalizedDeletes := make([]string, 0, len(deletions))
	for _, path := range deletions {
		normalizedDeletes = append(normalizedDeletes, normalizePath(path))
	}
	normalizedDeletes = uniqueSorted(normalizedDeletes)
	return normalizedRecord{
		pid:         record.PID,
		parentPID:   record.ParentPID,
		argv:        argv,
		cwd:         cwd,
		env:         slices.Clone(record.Env),
		inputs:      inputs,
		readMisses:  normalizedMisses,
		changes:     changes,
		deletions:   normalizedDeletes,
		inputOrigin: inputOrigin,
	}
}

type recordNormalizer interface {
	match(trace.Record) bool
	normalize(trace.Record) ([]string, string, []string, []string)
}

func resolveNormalizer(record trace.Record) recordNormalizer {
	for _, normalizer := range []recordNormalizer{
		ccNormalizer{},
		cmakeNormalizer{},
		pythonNormalizer{},
		goNormalizer{},
		genericNormalizer{},
	} {
		if normalizer.match(record) {
			return normalizer
		}
	}
	return genericNormalizer{}
}

type genericNormalizer struct{}

func (genericNormalizer) match(trace.Record) bool { return true }

func (genericNormalizer) normalize(record trace.Record) ([]string, string, []string, []string) {
	argv := make([]string, 0, len(record.Argv))
	for _, arg := range record.Argv {
		argv = append(argv, strings.ReplaceAll(normalizePath(arg), `\`, `/`))
	}
	inputs := make([]string, 0, len(record.Inputs))
	for _, path := range record.Inputs {
		inputs = append(inputs, normalizePath(path))
	}
	changes := make([]string, 0, len(record.Changes))
	for _, path := range record.Changes {
		changes = append(changes, normalizePath(path))
	}
	return argv, normalizePath(record.Cwd), inputs, changes
}

type ccNormalizer struct{ genericNormalizer }

func (ccNormalizer) match(record trace.Record) bool {
	tool := ""
	if len(record.Argv) > 0 {
		tool = filepath.Base(record.Argv[0])
	}
	switch tool {
	case "cc", "c++", "gcc", "g++", "clang", "clang++", "ld", "ar":
		return true
	default:
		return false
	}
}

type cmakeNormalizer struct{ genericNormalizer }

func (cmakeNormalizer) match(record trace.Record) bool {
	tool := ""
	if len(record.Argv) > 0 {
		tool = filepath.Base(record.Argv[0])
	}
	return tool == "cmake" || tool == "ninja" || tool == "make"
}

type pythonNormalizer struct{ genericNormalizer }

func (pythonNormalizer) match(record trace.Record) bool {
	tool := ""
	if len(record.Argv) > 0 {
		tool = filepath.Base(record.Argv[0])
	}
	if strings.HasPrefix(tool, "python") {
		return true
	}
	return tool == "pip"
}

type goNormalizer struct{ genericNormalizer }

func (goNormalizer) match(record trace.Record) bool {
	if len(record.Argv) == 0 {
		return false
	}
	return filepath.Base(record.Argv[0]) == "go"
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
		for _, path := range record.readMisses {
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
	record.readMisses = filter(record.readMisses)
	record.changes = filter(record.changes)
	return record
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func uniqueSorted(values []string) []string {
	out := slices.Clone(values)
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	out = slices.DeleteFunc(out, func(value string) bool {
		return value == ""
	})
	slices.Sort(out)
	return slices.Compact(out)
}
