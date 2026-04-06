package evaluator

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goplus/llar/internal/trace"
)

type graphObservationSource uint8

const (
	graphSourceRecords graphObservationSource = iota
	graphSourceEvents
)

type graphObservation struct {
	source       graphObservationSource
	records      int
	events       int
	inputDigests map[string]string
	actions      []normalizedRecord
}

type normalizedRecord struct {
	pid         int64
	parentPID   int64
	argv        []string
	cwd         string
	inputs      []string
	changes     []string
	deletions   []string
	inputOrigin map[string]string
	fingerprint string
}

func (source graphObservationSource) String() string {
	switch source {
	case graphSourceEvents:
		return "events"
	default:
		return "records"
	}
}

func buildObservationFromProbe(probe ProbeResult) graphObservation {
	if len(probe.Events) > 0 {
		return buildObservationFromEvents(probe.Events, probe.Records, probe.InputDigests)
	}
	return buildObservationFromRecords(probe.Records, probe.InputDigests)
}

func buildObservationFromRecords(records []trace.Record, inputDigests map[string]string) graphObservation {
	normalized := make([]normalizedRecord, 0, len(records))
	for _, record := range records {
		normalized = append(normalized, normalizeRecordWithDeletes(record, nil))
	}
	return graphObservation{
		source:       graphSourceRecords,
		records:      len(records),
		inputDigests: maps.Clone(inputDigests),
		actions:      normalized,
	}
}

func buildObservationFromEvents(events []trace.Event, fallbackRecords []trace.Record, inputDigests map[string]string) graphObservation {
	records := recordsFromEventsDetailed(events, fallbackRecords)
	normalized := make([]normalizedRecord, 0, len(records))
	for _, record := range records {
		normalized = append(normalized, normalizeRecordWithDeletes(record.record, record.deletions))
	}
	return graphObservation{
		source:       graphSourceEvents,
		records:      len(records),
		events:       len(events),
		inputDigests: maps.Clone(inputDigests),
		actions:      normalized,
	}
}

type eventRecord struct {
	record    trace.Record
	deletions []string
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

func recordsFromEvents(events []trace.Event, fallbackRecords []trace.Record) []trace.Record {
	detailed := recordsFromEventsDetailed(events, fallbackRecords)
	out := make([]trace.Record, 0, len(detailed))
	for _, rec := range detailed {
		out = append(out, rec.record)
	}
	return out
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

func normalizeRecord(record trace.Record) normalizedRecord {
	return normalizeRecordWithDeletes(record, nil)
}

func normalizeRecordWithDeletes(record trace.Record, deletions []string) normalizedRecord {
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
	changes = uniqueSorted(changes)
	normalizedDeletes := make([]string, 0, len(deletions))
	for _, path := range deletions {
		normalizedDeletes = append(normalizedDeletes, normalizePath(path))
	}
	normalizedDeletes = uniqueSorted(normalizedDeletes)
	parts := append([]string{}, argv...)
	parts = append(parts, "@", cwd, "@")
	parts = append(parts, inputs...)
	parts = append(parts, "@")
	parts = append(parts, changes...)
	parts = append(parts, "@")
	for _, path := range normalizedDeletes {
		parts = append(parts, "!"+path)
	}
	return normalizedRecord{
		pid:         record.PID,
		parentPID:   record.ParentPID,
		argv:        argv,
		cwd:         cwd,
		inputs:      inputs,
		changes:     changes,
		deletions:   normalizedDeletes,
		inputOrigin: inputOrigin,
		fingerprint: strings.Join(parts, "\x1f"),
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
