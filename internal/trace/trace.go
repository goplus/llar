package trace

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

type Record struct {
	PID       int64 `json:"pid,omitempty"`
	ParentPID int64 `json:"parent_pid,omitempty"`

	Argv []string `json:"argv"`
	Env  []string `json:"env,omitempty"`
	Cwd  string   `json:"cwd"`

	Inputs  []string `json:"inputs,omitempty"`
	Changes []string `json:"changes,omitempty"`
}

type EventKind uint8

const (
	EventExec EventKind = iota
	EventChdir
	EventRead
	EventWrite
	EventRename
	EventUnlink
	EventMkdir
	EventSymlink
	EventClone
)

type Event struct {
	Seq         int64     `json:"seq"`
	PID         int64     `json:"pid,omitempty"`
	ParentPID   int64     `json:"parent_pid,omitempty"`
	Cwd         string    `json:"cwd,omitempty"`
	Kind        EventKind `json:"kind"`
	Path        string    `json:"path,omitempty"`
	RelatedPath string    `json:"related_path,omitempty"`
	Argv        []string  `json:"argv,omitempty"`
	ChildPID    int64     `json:"child_pid,omitempty"`
}

func (kind EventKind) String() string {
	switch kind {
	case EventExec:
		return "exec"
	case EventChdir:
		return "chdir"
	case EventRead:
		return "read"
	case EventWrite:
		return "write"
	case EventRename:
		return "rename"
	case EventUnlink:
		return "unlink"
	case EventMkdir:
		return "mkdir"
	case EventSymlink:
		return "symlink"
	case EventClone:
		return "clone"
	default:
		return "unknown"
	}
}

type ParseDiagnostics struct {
	UnrecognizedLines int `json:"unrecognized_lines,omitempty"`
	ResumedMismatches int `json:"resumed_mismatches,omitempty"`
	InvalidCalls      int `json:"invalid_calls,omitempty"`
	MissingPIDLines   int `json:"missing_pid_lines,omitempty"`
	PIDStateResets    int `json:"pid_state_resets,omitempty"`
}

func (d ParseDiagnostics) Trusted() bool {
	return d.UnrecognizedLines == 0 &&
		d.ResumedMismatches == 0 &&
		d.InvalidCalls == 0 &&
		d.MissingPIDLines == 0
}

type CaptureResult struct {
	Records     []Record
	Events      []Event
	Diagnostics ParseDiagnostics
}

type Scope struct {
	SourceRoot  string   `json:"source_root,omitempty"`
	BuildRoot   string   `json:"build_root,omitempty"`
	InstallRoot string   `json:"install_root,omitempty"`
	KeepRoots   []string `json:"keep_roots,omitempty"`
}

type CaptureOptions struct {
	RootCwd   string
	KeepRoots []string
}

type parseOptions struct {
	rootCwd   string
	keepRoots []string
}

type procState struct {
	parentPID int64
	cwd       string
	current   *Record
}

type parseResult struct {
	records     []Record
	events      []Event
	diagnostics ParseDiagnostics
}

type parsedCall struct {
	name string
	args []string
	ret  string
}

const syntheticMainPID int64 = 0

var (
	straceLinePrefixRE = regexp.MustCompile(`^\s*(?:(?:\[pid\s+)?(\d+)(?:\])?\s+)?(?:\d+\.\d+\s+)?(.*)$`)
	resumedCallRE      = regexp.MustCompile(`^<\.\.\.\s+([A-Za-z_][A-Za-z0-9_]*)\s+resumed>\s*(.*)$`)
)

const unfinishedSuffix = " <unfinished ...>"

// Watch observes a build-only execution for one module/matrix combination and
// returns command-level records in execution order.
func Watch(ctx context.Context, moduleArg, combo string) ([]Record, error) {
	switch runtime.GOOS {
	case "linux":
		result, err := watchWithStrace(ctx, moduleArg, combo)
		return result.Records, err
	default:
		return nil, fmt.Errorf("trace is unsupported on %s", runtime.GOOS)
	}
}

func watchWithStrace(ctx context.Context, moduleArg, combo string) (CaptureResult, error) {
	if _, err := exec.LookPath("strace"); err != nil {
		return CaptureResult{}, fmt.Errorf("strace not found: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return CaptureResult{}, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return CaptureResult{}, err
	}

	tmpDir, err := os.MkdirTemp("", "llar-trace-*")
	if err != nil {
		return CaptureResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	outFile := filepath.Join(tmpDir, "trace.log")
	args := []string{
		"-f",
		"-ttt",
		"-yy",
		"-v",
		"-s", "65535",
		"-e", "trace=execve,execveat,chdir,open,openat,openat2,creat,rename,renameat,renameat2,unlink,unlinkat,mkdir,mkdirat,symlink,symlinkat,clone,fork,vfork",
		"-o", outFile,
		exe,
		"make",
		"--matrix", combo,
		moduleArg,
	}
	cmd := exec.CommandContext(ctx, "strace", args...)
	cmd.Dir = wd

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return CaptureResult{}, fmt.Errorf("strace failed: %w, output: %s", err, out.String())
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		return CaptureResult{}, err
	}
	parsed := parseStraceOutputDetailed(string(data), parseOptions{rootCwd: wd})
	return CaptureResult{Records: parsed.records, Events: parsed.events, Diagnostics: parsed.diagnostics}, nil
}

func parseStraceRecords(content string, opts parseOptions) []Record {
	return parseStraceOutputDetailed(content, opts).records
}

func parseStraceEvents(content string, opts parseOptions) []Event {
	return parseStraceOutputDetailed(content, opts).events
}

func parseStraceRecordsDetailed(content string, opts parseOptions) ([]Record, ParseDiagnostics) {
	parsed := parseStraceOutputDetailed(content, opts)
	return parsed.records, parsed.diagnostics
}

func parseStraceOutputDetailed(content string, opts parseOptions) parseResult {
	states := map[int64]*procState{}
	unfinished := map[int64]string{}
	var ordered []*Record
	var events []Event
	var diagnostics ParseDiagnostics
	var fallbackPID int64 = syntheticMainPID
	var nextSeq int64 = 1
	opts.keepRoots = normalizeKeepRoots(opts.keepRoots)

	stateOf := func(pid int64) *procState {
		if st, ok := states[pid]; ok {
			return st
		}
		st := &procState{cwd: opts.rootCwd}
		states[pid] = st
		return st
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		pid, hasPID, rawCall, ok := splitStraceLine(line)
		if !ok {
			diagnostics.UnrecognizedLines++
			continue
		}
		if !hasPID {
			diagnostics.MissingPIDLines++
			pid = fallbackPID
		} else {
			fallbackPID = pid
		}
		if strings.HasSuffix(rawCall, unfinishedSuffix) {
			unfinished[pid] = strings.TrimSuffix(rawCall, unfinishedSuffix)
			continue
		}
		if resumed, ok := mergeResumedCall(rawCall, unfinished[pid]); ok {
			rawCall = resumed
			delete(unfinished, pid)
		} else if isResumedCall(rawCall) {
			delete(unfinished, pid)
			diagnostics.ResumedMismatches++
			continue
		}
		call, ok := parseCall(rawCall)
		if !ok {
			diagnostics.InvalidCalls++
			continue
		}

		state := stateOf(pid)
		switch call.name {
		case "clone", "fork", "vfork":
			if !callSucceeded(call) {
				continue
			}
			fields := strings.Fields(call.ret)
			if len(fields) == 0 {
				continue
			}
			childPID, err := strconv.ParseInt(fields[0], 10, 64)
			if err == nil && childPID > 0 {
				events = appendEvent(events, &nextSeq, Event{
					PID:       pid,
					ParentPID: state.parentPID,
					Cwd:       state.cwd,
					Kind:      EventClone,
					ChildPID:  childPID,
				})
				childState, ok := states[childPID]
				_, childHasUnfinished := unfinished[childPID]
				if ok && !childHasUnfinished {
					diagnostics.PIDStateResets++
					childState = &procState{}
					states[childPID] = childState
				}
				if !ok {
					childState = &procState{}
					states[childPID] = childState
				}
				if childState.parentPID == 0 {
					childState.parentPID = pid
				}
				if childState.cwd == "" || childState.cwd == opts.rootCwd {
					childState.cwd = state.cwd
				}
				if childState.current != nil {
					if childState.current.ParentPID == 0 {
						childState.current.ParentPID = pid
					}
					if childState.current.Cwd == "" || childState.current.Cwd == opts.rootCwd {
						childState.current.Cwd = state.cwd
					}
				}
			}
		case "chdir":
			if callSucceeded(call) && len(call.args) > 0 {
				state.cwd = resolvePath(state.cwd, parseQuoted(call.args[0]))
				events = appendEvent(events, &nextSeq, Event{
					PID:       pid,
					ParentPID: state.parentPID,
					Cwd:       state.cwd,
					Kind:      EventChdir,
					Path:      state.cwd,
				})
			}
		case "execve", "execveat":
			if !callSucceeded(call) {
				continue
			}
			path, argv, env := parseExecArgs(call)
			if len(argv) == 0 && path != "" {
				argv = []string{path}
			}
			if len(argv) == 0 {
				continue
			}
			rec := &Record{
				PID:       pid,
				ParentPID: state.parentPID,
				Argv:      argv,
				Env:       env,
				Cwd:       state.cwd,
			}
			state.current = rec
			ordered = append(ordered, rec)
			events = appendEvent(events, &nextSeq, Event{
				PID:       pid,
				ParentPID: state.parentPID,
				Cwd:       state.cwd,
				Kind:      EventExec,
				Path:      resolvePath(state.cwd, path),
				Argv:      slices.Clone(argv),
			})
		case "open", "openat", "openat2", "creat":
			if !callSucceeded(call) {
				continue
			}
			path := parseResolvedOpenPath(state.cwd, call)
			if path == "" {
				continue
			}
			if !shouldKeepPath(path, opts.keepRoots) {
				continue
			}
			write := isWriteOpen(call)
			if write {
				events = appendEvent(events, &nextSeq, Event{
					PID:       pid,
					ParentPID: state.parentPID,
					Cwd:       state.cwd,
					Kind:      EventWrite,
					Path:      path,
				})
			} else {
				events = appendEvent(events, &nextSeq, Event{
					PID:       pid,
					ParentPID: state.parentPID,
					Cwd:       state.cwd,
					Kind:      EventRead,
					Path:      path,
				})
			}
			if state.current == nil {
				continue
			}
			if write {
				if !slices.Contains(state.current.Changes, path) {
					state.current.Changes = append(state.current.Changes, path)
				}
			} else {
				if !slices.Contains(state.current.Inputs, path) {
					state.current.Inputs = append(state.current.Inputs, path)
				}
			}
		case "rename", "renameat", "renameat2":
			if !callSucceeded(call) {
				continue
			}
			paths := parseResolvedRenamePaths(state.cwd, call)
			if len(paths) >= 2 && (shouldKeepPath(paths[0], opts.keepRoots) || shouldKeepPath(paths[1], opts.keepRoots)) {
				events = appendEvent(events, &nextSeq, Event{
					PID:         pid,
					ParentPID:   state.parentPID,
					Cwd:         state.cwd,
					Kind:        EventRename,
					Path:        paths[1],
					RelatedPath: paths[0],
				})
			}
			if state.current == nil {
				continue
			}
			for _, path := range paths {
				if path == "" || !shouldKeepPath(path, opts.keepRoots) {
					continue
				}
				if !slices.Contains(state.current.Changes, path) {
					state.current.Changes = append(state.current.Changes, path)
				}
			}
		case "unlink", "unlinkat", "mkdir", "mkdirat", "symlink", "symlinkat":
			if !callSucceeded(call) {
				continue
			}
			path := parseResolvedChangePath(state.cwd, call)
			if path == "" {
				continue
			}
			if !shouldKeepPath(path, opts.keepRoots) {
				continue
			}
			events = appendEvent(events, &nextSeq, Event{
				PID:         pid,
				ParentPID:   state.parentPID,
				Cwd:         state.cwd,
				Kind:        eventKindForChangeCall(call.name),
				Path:        path,
				RelatedPath: parseRelatedChangePath(call),
			})
			if state.current == nil {
				continue
			}
			if !slices.Contains(state.current.Changes, path) {
				state.current.Changes = append(state.current.Changes, path)
			}
		}
	}

	out := make([]Record, 0, len(ordered))
	for _, rec := range ordered {
		if rec == nil || len(rec.Argv) == 0 {
			continue
		}
		out = append(out, *rec)
	}
	return parseResult{
		records:     out,
		events:      events,
		diagnostics: diagnostics,
	}
}

func appendEvent(events []Event, nextSeq *int64, event Event) []Event {
	event.Seq = *nextSeq
	*nextSeq = *nextSeq + 1
	return append(events, event)
}

func splitStraceLine(line string) (int64, bool, string, bool) {
	matches := straceLinePrefixRE.FindStringSubmatch(line)
	if len(matches) != 3 {
		return 0, false, "", false
	}
	var pid int64
	hasPID := matches[1] != ""
	if matches[1] != "" {
		pid, _ = strconv.ParseInt(matches[1], 10, 64)
	}
	raw := strings.TrimSpace(matches[2])
	if raw == "" {
		return 0, hasPID, "", false
	}
	return pid, hasPID, raw, true
}

func parseCall(line string) (parsedCall, bool) {
	open := strings.IndexByte(line, '(')
	if open <= 0 {
		return parsedCall{}, false
	}
	name := strings.TrimSpace(line[:open])
	closeIdx := findClosingParen(line, open)
	if closeIdx < 0 {
		return parsedCall{}, false
	}
	argsBody := line[open+1 : closeIdx]
	ret := ""
	if eq := strings.LastIndex(line[closeIdx+1:], "="); eq >= 0 {
		ret = strings.TrimSpace(line[closeIdx+1+eq+1:])
	}
	return parsedCall{
		name: name,
		args: splitTopLevel(argsBody),
		ret:  ret,
	}, true
}

func isResumedCall(raw string) bool {
	return resumedCallRE.MatchString(raw)
}

func mergeResumedCall(raw, partial string) (string, bool) {
	matches := resumedCallRE.FindStringSubmatch(raw)
	if len(matches) != 3 || partial == "" {
		return "", false
	}
	if name := callName(partial); name != "" && name != matches[1] {
		return "", false
	}
	return partial + matches[2], true
}

func callName(line string) string {
	if open := strings.IndexByte(line, '('); open > 0 {
		return strings.TrimSpace(line[:open])
	}
	return ""
}

func findClosingParen(line string, open int) int {
	depth := 0
	inQuote := false
	escaped := false
	for i := open; i < len(line); i++ {
		ch := line[i]
		if inQuote {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inQuote = false
			}
			continue
		}
		switch ch {
		case '"':
			inQuote = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func splitTopLevel(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var parts []string
	start := 0
	depthParen, depthBracket, depthBrace := 0, 0, 0
	inQuote := false
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inQuote {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inQuote = false
			}
			continue
		}
		switch ch {
		case '"':
			inQuote = true
		case '(':
			depthParen++
		case ')':
			depthParen--
		case '[':
			depthBracket++
		case ']':
			depthBracket--
		case '{':
			depthBrace++
		case '}':
			depthBrace--
		case ',':
			if depthParen == 0 && depthBracket == 0 && depthBrace == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}

func parseExecArgs(call parsedCall) (string, []string, []string) {
	switch call.name {
	case "execve":
		path := ""
		if len(call.args) > 0 {
			path = parseQuoted(call.args[0])
		}
		var argv []string
		if len(call.args) > 1 {
			argv = parseStringArray(call.args[1])
		}
		var env []string
		if len(call.args) > 2 {
			env = parseStringArray(call.args[2])
		}
		return path, argv, env
	case "execveat":
		path := ""
		if len(call.args) > 1 {
			path = parseQuoted(call.args[1])
		}
		var argv []string
		if len(call.args) > 2 {
			argv = parseStringArray(call.args[2])
		}
		var env []string
		if len(call.args) > 3 {
			env = parseStringArray(call.args[3])
		}
		return path, argv, env
	default:
		return "", nil, nil
	}
}

func parseOpenPath(call parsedCall) string {
	switch call.name {
	case "open", "creat":
		if len(call.args) > 0 {
			return parseQuoted(call.args[0])
		}
	case "openat", "openat2":
		if len(call.args) > 1 {
			return parseQuoted(call.args[1])
		}
	}
	return ""
}

func parseResolvedOpenPath(cwd string, call parsedCall) string {
	if resolved := parseReturnedFDPath(call.ret); resolved != "" {
		return resolved
	}
	switch call.name {
	case "open", "creat":
		return resolvePath(cwd, parseOpenPath(call))
	case "openat", "openat2":
		if len(call.args) <= 1 {
			return ""
		}
		return resolveDirfdRelativePath(cwd, call.args[0], call.args[1])
	default:
		return ""
	}
}

func parseReturnedFDPath(ret string) string {
	ret = strings.TrimSpace(ret)
	start := strings.IndexByte(ret, '<')
	end := strings.LastIndexByte(ret, '>')
	if start < 0 || end <= start+1 {
		return ""
	}
	path := strings.TrimSpace(ret[start+1 : end])
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	return filepath.Clean(path)
}

func parseDirfdBasePath(arg string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" || arg == "AT_FDCWD" {
		return ""
	}
	start := strings.IndexByte(arg, '<')
	end := strings.LastIndexByte(arg, '>')
	if start < 0 || end <= start+1 {
		return ""
	}
	base := filepath.Clean(arg[start+1 : end])
	if base == "." || base == "" {
		return ""
	}
	return base
}

func resolveDirfdRelativePath(cwd, dirfdArg, pathArg string) string {
	path := parseQuoted(pathArg)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if base := parseDirfdBasePath(dirfdArg); base != "" {
		return filepath.Clean(filepath.Join(base, path))
	}
	return resolvePath(cwd, path)
}

func parseResolvedRenamePaths(cwd string, call parsedCall) []string {
	switch call.name {
	case "rename":
		if len(call.args) >= 2 {
			return []string{
				resolvePath(cwd, parseQuoted(call.args[0])),
				resolvePath(cwd, parseQuoted(call.args[1])),
			}
		}
	case "renameat", "renameat2":
		if len(call.args) >= 4 {
			return []string{
				resolveDirfdRelativePath(cwd, call.args[0], call.args[1]),
				resolveDirfdRelativePath(cwd, call.args[2], call.args[3]),
			}
		}
	}
	return nil
}

func parseResolvedChangePath(cwd string, call parsedCall) string {
	switch call.name {
	case "unlink", "mkdir":
		if len(call.args) > 0 {
			return resolvePath(cwd, parseQuoted(call.args[0]))
		}
	case "symlink":
		if len(call.args) > 1 {
			return resolvePath(cwd, parseQuoted(call.args[1]))
		}
	case "unlinkat", "mkdirat":
		if len(call.args) > 1 {
			return resolveDirfdRelativePath(cwd, call.args[0], call.args[1])
		}
	case "symlinkat":
		if len(call.args) > 2 {
			return resolveDirfdRelativePath(cwd, call.args[1], call.args[2])
		}
	}
	return ""
}

func parseRelatedChangePath(call parsedCall) string {
	switch call.name {
	case "symlink", "symlinkat":
		if len(call.args) > 0 {
			return parseQuoted(call.args[0])
		}
	}
	return ""
}

func eventKindForChangeCall(name string) EventKind {
	switch name {
	case "mkdir", "mkdirat":
		return EventMkdir
	case "symlink", "symlinkat":
		return EventSymlink
	default:
		return EventUnlink
	}
}

func callSucceeded(call parsedCall) bool {
	ret := strings.TrimSpace(call.ret)
	return ret != "" && !strings.HasPrefix(ret, "-1")
}

func isWriteOpen(call parsedCall) bool {
	if call.name == "creat" {
		return true
	}
	flags := strings.ToUpper(strings.Join(call.args, " "))
	return strings.Contains(flags, "O_WRONLY") ||
		strings.Contains(flags, "O_RDWR") ||
		strings.Contains(flags, "O_TRUNC") ||
		strings.Contains(flags, "O_APPEND") ||
		(strings.Contains(flags, "O_CREAT") && strings.Contains(flags, "O_EXCL"))
}

func parseStringArray(arg string) []string {
	arg = strings.TrimSpace(arg)
	if !strings.HasPrefix(arg, "[") || !strings.HasSuffix(arg, "]") {
		return nil
	}
	parts := splitTopLevel(strings.TrimSpace(arg[1 : len(arg)-1]))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = parseQuoted(part)
		if part == "" || part == "NULL" || part == "0x0" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func parseQuoted(arg string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" || arg == "NULL" || arg == "0x0" {
		return ""
	}
	if strings.HasPrefix(arg, "\"") {
		if end := strings.LastIndex(arg, "\""); end > 0 {
			quoted := arg[:end+1]
			if unquoted, err := strconv.Unquote(quoted); err == nil {
				return unquoted
			}
			return strings.Trim(quoted, "\"")
		}
	}
	return arg
}

func resolvePath(cwd, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if cwd == "" {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
}

func normalizeKeepRoots(roots []string) []string {
	if len(roots) == 0 {
		return nil
	}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" || root == "." {
			continue
		}
		if slices.Contains(out, root) {
			continue
		}
		out = append(out, root)
	}
	return out
}

func shouldKeepPath(path string, keepRoots []string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	if len(keepRoots) == 0 {
		return true
	}
	for _, root := range keepRoots {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
