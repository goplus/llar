package evaluator

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

type OutputMergeStatus string

const (
	OutputMergeStatusMerged       OutputMergeStatus = "merged"
	OutputMergeStatusNeedsRebuild OutputMergeStatus = "needs-rebuild"
)

type OutputMergeIssueKind string

const (
	OutputMergeIssueKindMetadataUnmergeable   OutputMergeIssueKind = "metadata-unmergeable"
	OutputMergeIssueKindPathAddedDifferently  OutputMergeIssueKind = "path-added-differently"
	OutputMergeIssueKindPathDeleteChange      OutputMergeIssueKind = "path-delete-change"
	OutputMergeIssueKindPathKindMismatch      OutputMergeIssueKind = "path-kind-mismatch"
	OutputMergeIssueKindArchiveUnmergeable    OutputMergeIssueKind = "archive-unmergeable"
	OutputMergeIssueKindFileTextUnmergeable   OutputMergeIssueKind = "file-text-unmergeable"
	OutputMergeIssueKindFileBinaryUnmergeable OutputMergeIssueKind = "file-binary-unmergeable"
	OutputMergeIssueKindRootReplayUnavailable OutputMergeIssueKind = "root-replay-unavailable"
	OutputMergeIssueKindRootReplayFailed      OutputMergeIssueKind = "root-replay-failed"
	OutputMergeIssueKindUnsupportedKind       OutputMergeIssueKind = "unsupported-kind"
)

type OutputMergeIssue struct {
	Kind   OutputMergeIssueKind
	Path   string
	Reason string
	Detail string
	Base   string
	Left   string
	Right  string
}

type OutputMergeResult struct {
	Status   OutputMergeStatus
	Root     string
	Metadata string
	Manifest OutputManifest
	Issues   []OutputMergeIssue
}

var postProcessMergedArchive = runRanlib

func (r OutputMergeResult) Clean() bool {
	return r.Status == OutputMergeStatusMerged
}

func (r OutputMergeResult) NeedsRebuild() bool {
	return r.Status == OutputMergeStatusNeedsRebuild
}

func MergeOutputTrees(baseDir string, base OutputManifest, leftDir string, left OutputManifest, rightDir string, right OutputManifest) (OutputMergeResult, error) {
	result := OutputMergeResult{Status: OutputMergeStatusMerged}

	metadata, ok := mergeMetadata(base.Metadata, left.Metadata, right.Metadata)
	if !ok {
		result.Status = OutputMergeStatusNeedsRebuild
		result.Issues = append(result.Issues, OutputMergeIssue{
			Kind:   OutputMergeIssueKindMetadataUnmergeable,
			Path:   "<metadata>",
			Reason: "metadata requires real pair build",
			Detail: describeMetadataConflictDetail(base.Metadata, left.Metadata, right.Metadata),
			Base:   summarizeMetadata(base.Metadata),
			Left:   summarizeMetadata(left.Metadata),
			Right:  summarizeMetadata(right.Metadata),
		})
		return result, nil
	}
	result.Metadata = metadata

	mergedEntries, content, issues, err := mergeManifestEntries(baseDir, base, leftDir, left, rightDir, right)
	if err != nil {
		return OutputMergeResult{}, err
	}
	if len(issues) > 0 {
		result.Status = OutputMergeStatusNeedsRebuild
		result.Issues = issues
		return result, nil
	}

	root, err := os.MkdirTemp("", "llar-merge-*")
	if err != nil {
		return OutputMergeResult{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(root)
		}
	}()

	for path, data := range content {
		dst := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return OutputMergeResult{}, err
		}
		if data.symlink {
			if err := os.Symlink(data.target, dst); err != nil {
				return OutputMergeResult{}, err
			}
			continue
		}
		mode := fs.FileMode(0o644)
		if data.executable {
			mode = 0o755
		}
		if err := os.WriteFile(dst, data.bytes, mode); err != nil {
			return OutputMergeResult{}, err
		}
		if data.ranlib {
			if err := postProcessMergedArchive(dst); err != nil {
				return OutputMergeResult{}, err
			}
		}
	}

	manifest, err := BuildOutputManifest(root, metadata)
	if err != nil {
		return OutputMergeResult{}, err
	}
	if len(mergedEntries) == 0 {
		manifest.Entries = nil
	}
	result.Root = root
	result.Manifest = manifest
	cleanup = false
	return result, nil
}

type outputContent struct {
	bytes      []byte
	executable bool
	symlink    bool
	target     string
	ranlib     bool
}

type manifestState struct {
	present bool
	entry   OutputEntry
}

func mergeManifestEntries(baseDir string, base OutputManifest, leftDir string, left OutputManifest, rightDir string, right OutputManifest) (map[string]OutputEntry, map[string]outputContent, []OutputMergeIssue, error) {
	allPaths := make(map[string]struct{}, len(base.Entries)+len(left.Entries)+len(right.Entries))
	for path := range base.Entries {
		allPaths[path] = struct{}{}
	}
	for path := range left.Entries {
		allPaths[path] = struct{}{}
	}
	for path := range right.Entries {
		allPaths[path] = struct{}{}
	}

	mergedEntries := make(map[string]OutputEntry, len(allPaths))
	content := make(map[string]outputContent, len(allPaths))
	issues := make([]OutputMergeIssue, 0)
	paths := mapsKeys(allPaths)
	slices.Sort(paths)
	for _, path := range paths {
		baseState := manifestStateForPath(base.Entries, path)
		leftState := manifestStateForPath(left.Entries, path)
		rightState := manifestStateForPath(right.Entries, path)

		mergedState, data, ok, err := mergeManifestPath(path, baseDir, baseState, leftDir, leftState, rightDir, rightState)
		if err != nil {
			return nil, nil, nil, err
		}
		if !ok {
			kind, reason, detail := describeManifestIssue(path, baseDir, baseState, leftDir, leftState, rightDir, rightState)
			issues = append(issues, OutputMergeIssue{
				Kind:   kind,
				Path:   path,
				Reason: reason,
				Detail: detail,
				Base:   summarizeManifestState(baseState),
				Left:   summarizeManifestState(leftState),
				Right:  summarizeManifestState(rightState),
			})
			continue
		}
		if !mergedState.present {
			continue
		}
		mergedEntries[path] = mergedState.entry
		content[path] = data
	}
	return mergedEntries, content, issues, nil
}

func mergeManifestPath(path, baseDir string, baseState manifestState, leftDir string, leftState manifestState, rightDir string, rightState manifestState) (manifestState, outputContent, bool, error) {
	switch {
	case equalManifestState(leftState, rightState):
		data, err := materializeState(path, leftDir, leftState)
		return leftState, data, true, err
	case equalManifestState(leftState, baseState):
		data, err := materializeState(path, rightDir, rightState)
		return rightState, data, true, err
	case equalManifestState(rightState, baseState):
		data, err := materializeState(path, leftDir, leftState)
		return leftState, data, true, err
	}

	if !baseState.present || !leftState.present || !rightState.present {
		return manifestState{}, outputContent{}, false, nil
	}
	if leftState.entry.Kind != rightState.entry.Kind || leftState.entry.Kind != baseState.entry.Kind {
		return manifestState{}, outputContent{}, false, nil
	}

	switch leftState.entry.Kind {
	case "archive":
		entry, data, ok, err := mergeArchiveState(path, baseDir, baseState, leftDir, leftState, rightDir, rightState)
		return entry, data, ok, err
	case "file":
		entry, data, ok, err := mergeRegularFileState(path, baseDir, baseState, leftDir, leftState, rightDir, rightState)
		return entry, data, ok, err
	default:
		return manifestState{}, outputContent{}, false, nil
	}
}

func mergeRegularFileState(path, baseDir string, baseState manifestState, leftDir string, leftState manifestState, rightDir string, rightState manifestState) (manifestState, outputContent, bool, error) {
	baseData, err := os.ReadFile(filepath.Join(baseDir, filepath.FromSlash(path)))
	if err != nil {
		return manifestState{}, outputContent{}, false, err
	}
	leftData, err := os.ReadFile(filepath.Join(leftDir, filepath.FromSlash(path)))
	if err != nil {
		return manifestState{}, outputContent{}, false, err
	}
	rightData, err := os.ReadFile(filepath.Join(rightDir, filepath.FromSlash(path)))
	if err != nil {
		return manifestState{}, outputContent{}, false, err
	}
	if !isTextData(baseData) || !isTextData(leftData) || !isTextData(rightData) {
		return manifestState{}, outputContent{}, false, nil
	}

	merged, ok, err := mergeTextData(baseData, leftData, rightData)
	if err != nil || !ok {
		return manifestState{}, outputContent{}, ok, err
	}
	executable, ok := mergeBool(baseState.entry.Executable, leftState.entry.Executable, rightState.entry.Executable)
	if !ok {
		return manifestState{}, outputContent{}, false, nil
	}
	entry := OutputEntry{
		Kind:       "file",
		Digest:     digestBytesShort(merged),
		Executable: executable,
	}
	return manifestState{present: true, entry: entry}, outputContent{
		bytes:      merged,
		executable: executable,
	}, true, nil
}

func mergeArchiveState(path, baseDir string, baseState manifestState, leftDir string, leftState manifestState, rightDir string, rightState manifestState) (manifestState, outputContent, bool, error) {
	baseMembers, err := readArchiveMembers(filepath.Join(baseDir, filepath.FromSlash(path)))
	if err != nil {
		return manifestState{}, outputContent{}, false, err
	}
	leftMembers, err := readArchiveMembers(filepath.Join(leftDir, filepath.FromSlash(path)))
	if err != nil {
		return manifestState{}, outputContent{}, false, err
	}
	rightMembers, err := readArchiveMembers(filepath.Join(rightDir, filepath.FromSlash(path)))
	if err != nil {
		return manifestState{}, outputContent{}, false, err
	}

	mergedMembers, ok := mergeArchiveMembers(baseMembers, leftMembers, rightMembers)
	if !ok {
		return manifestState{}, outputContent{}, false, nil
	}
	data, err := encodeArchiveMembers(mergedMembers)
	if err != nil {
		return manifestState{}, outputContent{}, false, err
	}
	executable, ok := mergeBool(baseState.entry.Executable, leftState.entry.Executable, rightState.entry.Executable)
	if !ok {
		return manifestState{}, outputContent{}, false, nil
	}
	entry := OutputEntry{
		Kind:       "archive",
		Digest:     digestBytesShort(archiveDigestBytes(mergedMembers)),
		Executable: executable,
	}
	return manifestState{present: true, entry: entry}, outputContent{
		bytes:      data,
		executable: executable,
		ranlib:     true,
	}, true, nil
}

func mergeArchiveMembers(baseMembers, leftMembers, rightMembers []archiveMember) ([]archiveMember, bool) {
	baseByName := archiveMemberMap(baseMembers)
	leftByName := archiveMemberMap(leftMembers)
	rightByName := archiveMemberMap(rightMembers)

	orderedNames := archiveMemberOrder(baseMembers, leftMembers, rightMembers)
	merged := make([]archiveMember, 0, len(orderedNames))
	for _, name := range orderedNames {
		baseState := archiveMemberStateForName(baseByName, name)
		leftState := archiveMemberStateForName(leftByName, name)
		rightState := archiveMemberStateForName(rightByName, name)
		member, ok := mergeArchiveMember(baseState, leftState, rightState)
		if !ok {
			return nil, false
		}
		if member == nil {
			continue
		}
		merged = append(merged, *member)
	}
	return merged, true
}

type archiveMember struct {
	Name string
	Body []byte
}

type archiveMemberState struct {
	present bool
	member  archiveMember
}

func mergeArchiveMember(baseState, leftState, rightState archiveMemberState) (*archiveMember, bool) {
	switch {
	case equalArchiveMemberState(leftState, rightState):
		if !leftState.present {
			return nil, true
		}
		member := leftState.member
		return &member, true
	case equalArchiveMemberState(leftState, baseState):
		if !rightState.present {
			return nil, true
		}
		member := rightState.member
		return &member, true
	case equalArchiveMemberState(rightState, baseState):
		if !leftState.present {
			return nil, true
		}
		member := leftState.member
		return &member, true
	default:
		return nil, false
	}
}

func readArchiveMembers(path string) ([]archiveMember, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseArchiveMemberBodies(data)
}

func parseArchiveMemberBodies(data []byte) ([]archiveMember, error) {
	const globalHeader = "!<arch>\n"
	const fileHeaderLen = 60
	if len(data) < len(globalHeader) || string(data[:len(globalHeader)]) != globalHeader {
		return nil, fmt.Errorf("invalid archive header")
	}

	offset := len(globalHeader)
	var stringTable []byte
	members := make([]archiveMember, 0)
	for offset < len(data) {
		if len(data)-offset < fileHeaderLen {
			return nil, fmt.Errorf("truncated archive header")
		}
		header := data[offset : offset+fileHeaderLen]
		offset += fileHeaderLen

		if string(header[58:60]) != "`\n" {
			return nil, fmt.Errorf("invalid archive file trailer")
		}

		nameField := strings.TrimSpace(string(header[:16]))
		sizeField := strings.TrimSpace(string(header[48:58]))
		size, err := strconv.Atoi(sizeField)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("invalid archive member size %q", sizeField)
		}
		if len(data)-offset < size {
			return nil, fmt.Errorf("truncated archive member data")
		}

		payload := data[offset : offset+size]
		offset += size
		if offset%2 != 0 {
			offset++
		}

		if isArchiveSpecialName(nameField) {
			if nameField == "//" {
				stringTable = append(stringTable[:0], payload...)
			}
			continue
		}

		name, body, err := resolveArchiveMember(nameField, payload, stringTable)
		if err != nil {
			return nil, err
		}
		members = append(members, archiveMember{Name: name, Body: bytes.Clone(body)})
	}
	return members, nil
}

func encodeArchiveMembers(members []archiveMember) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("!<arch>\n")
	for _, member := range members {
		headerName := member.Name
		payload := member.Body
		if len(headerName) > 15 || strings.Contains(headerName, " ") || strings.Contains(headerName, "/") {
			headerName = fmt.Sprintf("#1/%d", len(member.Name))
			payload = append([]byte(member.Name), payload...)
		} else if !strings.HasSuffix(headerName, "/") {
			headerName += "/"
		}
		header := fmt.Sprintf("%-16s%-12d%-6d%-6d%-8o%-10d`\n", headerName, 0, 0, 0, 0o644, len(payload))
		if len(header) != 60 {
			return nil, fmt.Errorf("unexpected archive header length %d", len(header))
		}
		buf.WriteString(header)
		buf.Write(payload)
		if len(payload)%2 != 0 {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), nil
}

func archiveDigestBytes(members []archiveMember) []byte {
	if len(members) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, member := range members {
		buf.WriteString(member.Name)
		buf.WriteByte(0)
		buf.WriteString(digestBytesShort(member.Body))
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func archiveMemberMap(members []archiveMember) map[string]archiveMember {
	out := make(map[string]archiveMember, len(members))
	for _, member := range members {
		out[member.Name] = member
	}
	return out
}

func archiveMemberOrder(baseMembers, leftMembers, rightMembers []archiveMember) []string {
	seen := make(map[string]struct{}, len(baseMembers)+len(leftMembers)+len(rightMembers))
	out := make([]string, 0, len(seen))
	appendNames := func(members []archiveMember) {
		for _, member := range members {
			if _, ok := seen[member.Name]; ok {
				continue
			}
			seen[member.Name] = struct{}{}
			out = append(out, member.Name)
		}
	}
	appendNames(baseMembers)
	appendNames(leftMembers)
	appendNames(rightMembers)
	return out
}

func materializeState(path, root string, state manifestState) (outputContent, error) {
	if !state.present {
		return outputContent{}, nil
	}
	if state.entry.Kind == "symlink" {
		return outputContent{
			symlink: true,
			target:  state.entry.Target,
		}, nil
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return outputContent{}, err
	}
	return outputContent{
		bytes:      data,
		executable: state.entry.Executable,
		ranlib:     state.entry.Kind == "archive",
	}, nil
}

func mergeTextData(base, left, right []byte) ([]byte, bool, error) {
	if bytes.Equal(left, right) {
		return bytes.Clone(left), true, nil
	}
	if bytes.Equal(left, base) {
		return bytes.Clone(right), true, nil
	}
	if bytes.Equal(right, base) {
		return bytes.Clone(left), true, nil
	}

	if _, err := exec.LookPath("git"); err != nil {
		return nil, false, nil
	}

	tmpDir, err := os.MkdirTemp("", "llar-diff3-*")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(tmpDir)

	basePath := filepath.Join(tmpDir, "base")
	leftPath := filepath.Join(tmpDir, "left")
	rightPath := filepath.Join(tmpDir, "right")
	if err := os.WriteFile(basePath, base, 0o644); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(leftPath, left, 0o644); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(rightPath, right, 0o644); err != nil {
		return nil, false, err
	}

	cmd := exec.Command("git", "merge-file", "-p", leftPath, basePath, rightPath)
	out, err := cmd.Output()
	if err == nil {
		return out, true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return nil, false, nil
	}
	return nil, false, err
}

func runRanlib(path string) error {
	if _, err := exec.LookPath("ranlib"); err != nil {
		return nil
	}
	cmd := exec.Command("ranlib", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ranlib %s: %w (%s)", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func isTextData(data []byte) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	return utf8.Valid(data)
}

func equalManifestState(left, right manifestState) bool {
	if left.present != right.present {
		return false
	}
	if !left.present {
		return true
	}
	return left.entry == right.entry
}

func equalArchiveMemberState(left, right archiveMemberState) bool {
	if left.present != right.present {
		return false
	}
	if !left.present {
		return true
	}
	return left.member.Name == right.member.Name && bytes.Equal(left.member.Body, right.member.Body)
}

func manifestStateForPath(entries map[string]OutputEntry, path string) manifestState {
	entry, ok := entries[path]
	return manifestState{present: ok, entry: entry}
}

func archiveMemberStateForName(entries map[string]archiveMember, name string) archiveMemberState {
	member, ok := entries[name]
	return archiveMemberState{present: ok, member: member}
}

func mergeScalar(base, left, right string) (string, bool) {
	switch {
	case left == right:
		return left, true
	case left == base:
		return right, true
	case right == base:
		return left, true
	default:
		return "", false
	}
}

func mergeMetadata(base, left, right string) (string, bool) {
	if merged, ok := mergeScalar(base, left, right); ok {
		return merged, true
	}

	baseTokens, ok := tokenizeMergeableMetadata(base)
	if !ok {
		return "", false
	}
	leftTokens, ok := tokenizeMergeableMetadata(left)
	if !ok {
		return "", false
	}
	rightTokens, ok := tokenizeMergeableMetadata(right)
	if !ok {
		return "", false
	}
	if !hasTokenPrefix(leftTokens, baseTokens) || !hasTokenPrefix(rightTokens, baseTokens) {
		return "", false
	}

	merged := append([]string{}, baseTokens...)
	seen := make(map[string]struct{}, len(baseTokens))
	for _, token := range baseTokens {
		seen[token] = struct{}{}
	}
	for _, token := range leftTokens[len(baseTokens):] {
		if _, ok := seen[token]; ok {
			continue
		}
		merged = append(merged, token)
		seen[token] = struct{}{}
	}
	for _, token := range rightTokens[len(baseTokens):] {
		if _, ok := seen[token]; ok {
			continue
		}
		merged = append(merged, token)
		seen[token] = struct{}{}
	}
	return strings.Join(merged, " "), true
}

func tokenizeMergeableMetadata(metadata string) ([]string, bool) {
	trimmed := strings.TrimSpace(metadata)
	if trimmed == "" {
		return nil, true
	}
	if strings.ContainsAny(trimmed, "\"'\\") {
		return nil, false
	}
	tokens := strings.Fields(trimmed)
	if len(tokens) == 0 {
		return nil, true
	}
	expectValue := false
	for _, token := range tokens {
		if expectValue {
			expectValue = false
			continue
		}
		if !strings.HasPrefix(token, "-") {
			return nil, false
		}
		switch token {
		case "-framework", "-include", "-isystem", "-Xlinker", "-u":
			expectValue = true
		}
	}
	if expectValue {
		return nil, false
	}
	return tokens, true
}

func hasTokenPrefix(tokens, prefix []string) bool {
	if len(prefix) > len(tokens) {
		return false
	}
	for i := range prefix {
		if tokens[i] != prefix[i] {
			return false
		}
	}
	return true
}

func mergeBool(base, left, right bool) (bool, bool) {
	switch {
	case left == right:
		return left, true
	case left == base:
		return right, true
	case right == base:
		return left, true
	default:
		return false, false
	}
}

func describeManifestIssue(path, baseDir string, baseState manifestState, leftDir string, leftState manifestState, rightDir string, rightState manifestState) (OutputMergeIssueKind, string, string) {
	switch {
	case !baseState.present:
		return OutputMergeIssueKindPathAddedDifferently,
			"path added differently; automatic merge unavailable",
			"path was added on both sides with different outputs, so this pair needs a real combined build"
	case !leftState.present || !rightState.present:
		return OutputMergeIssueKindPathDeleteChange,
			"path deleted on one side and changed on the other; automatic merge unavailable",
			"path is missing on one or more sides, so this pair needs a real combined build"
	case leftState.entry.Kind != rightState.entry.Kind || leftState.entry.Kind != baseState.entry.Kind:
		return OutputMergeIssueKindPathKindMismatch,
			"path kind changed incompatibly; automatic merge unavailable",
			"path kind differs across sides, so this pair needs a real combined build"
	}
	switch baseState.entry.Kind {
	case "archive":
		baseMembers, err := readArchiveMembers(filepath.Join(baseDir, filepath.FromSlash(path)))
		if err != nil {
			return OutputMergeIssueKindArchiveUnmergeable, "archive changed on both sides; automatic merge unavailable", "failed to read base archive members"
		}
		leftMembers, err := readArchiveMembers(filepath.Join(leftDir, filepath.FromSlash(path)))
		if err != nil {
			return OutputMergeIssueKindArchiveUnmergeable, "archive changed on both sides; automatic merge unavailable", "failed to read left archive members"
		}
		rightMembers, err := readArchiveMembers(filepath.Join(rightDir, filepath.FromSlash(path)))
		if err != nil {
			return OutputMergeIssueKindArchiveUnmergeable, "archive changed on both sides; automatic merge unavailable", "failed to read right archive members"
		}
		summary := summarizeArchiveConflictMembers(baseMembers, leftMembers, rightMembers)
		if summary == "" {
			return OutputMergeIssueKindArchiveUnmergeable,
				"archive changed on both sides; automatic merge unavailable",
				"both sides changed this archive relative to base, so automatic archive merge cannot materialize a combined output"
		}
		return OutputMergeIssueKindArchiveUnmergeable,
			"archive changed on both sides; automatic merge unavailable",
			"both sides changed this archive relative to base, so automatic archive merge cannot materialize a combined output\n" + summary
	case "file":
		return summarizeRegularFileIssue(path, baseDir, leftDir, rightDir)
	default:
		return OutputMergeIssueKindUnsupportedKind,
			"unsupported output kind; automatic merge unavailable",
			"unsupported output kind " + baseState.entry.Kind
	}
}

func summarizeArchiveConflictMembers(baseMembers, leftMembers, rightMembers []archiveMember) string {
	baseByName := archiveMemberMap(baseMembers)
	leftByName := archiveMemberMap(leftMembers)
	rightByName := archiveMemberMap(rightMembers)
	orderedNames := archiveMemberOrder(baseMembers, leftMembers, rightMembers)
	conflicts := make([]string, 0)
	for _, name := range orderedNames {
		baseState := archiveMemberStateForName(baseByName, name)
		leftState := archiveMemberStateForName(leftByName, name)
		rightState := archiveMemberStateForName(rightByName, name)
		if _, ok := mergeArchiveMember(baseState, leftState, rightState); ok {
			continue
		}
		conflicts = append(conflicts, name)
	}
	if len(conflicts) == 0 {
		return ""
	}
	const limit = 6
	if len(conflicts) > limit {
		lines := []string{fmt.Sprintf("conflicting members (%d):", len(conflicts))}
		lines = append(lines, conflicts[:limit]...)
		lines = append(lines, fmt.Sprintf("(+%d more)", len(conflicts)-limit))
		return strings.Join(lines, "\n")
	}
	lines := []string{fmt.Sprintf("conflicting members (%d):", len(conflicts))}
	lines = append(lines, conflicts...)
	return strings.Join(lines, "\n")
}

func summarizeRegularFileIssue(path, baseDir, leftDir, rightDir string) (OutputMergeIssueKind, string, string) {
	baseData, err := os.ReadFile(filepath.Join(baseDir, filepath.FromSlash(path)))
	if err != nil {
		return OutputMergeIssueKindFileBinaryUnmergeable, "file changed on both sides; automatic merge unavailable", "failed to read base file"
	}
	leftData, err := os.ReadFile(filepath.Join(leftDir, filepath.FromSlash(path)))
	if err != nil {
		return OutputMergeIssueKindFileBinaryUnmergeable, "file changed on both sides; automatic merge unavailable", "failed to read left file"
	}
	rightData, err := os.ReadFile(filepath.Join(rightDir, filepath.FromSlash(path)))
	if err != nil {
		return OutputMergeIssueKindFileBinaryUnmergeable, "file changed on both sides; automatic merge unavailable", "failed to read right file"
	}
	if !isTextData(baseData) || !isTextData(leftData) || !isTextData(rightData) {
		return OutputMergeIssueKindFileBinaryUnmergeable,
			"non-text file changed on both sides; automatic merge unavailable",
			"both sides changed this non-text file relative to base, so a real combined build is required"
	}
	_, ok, err := mergeTextData(baseData, leftData, rightData)
	if err != nil {
		return OutputMergeIssueKindFileTextUnmergeable,
			"text file changed on both sides; automatic merge unavailable",
			"both sides changed this text file, and automatic three-way merge failed with an error"
	}
	if !ok {
		return OutputMergeIssueKindFileTextUnmergeable,
			"text file changed on both sides; automatic merge unavailable",
			"both sides changed this text file, and automatic three-way merge reported overlapping edits"
	}
	return OutputMergeIssueKindFileTextUnmergeable,
		"text file changed on both sides; automatic merge unavailable",
		"both sides changed this text file relative to base, so a real combined build is required"
}

func describeMetadataConflictDetail(base, left, right string) string {
	baseTokens, baseOK := tokenizeMergeableMetadata(base)
	leftTokens, leftOK := tokenizeMergeableMetadata(left)
	rightTokens, rightOK := tokenizeMergeableMetadata(right)
	if !baseOK || !leftOK || !rightOK {
		return "both sides changed metadata, and automatic flag merge does not support this syntax; a real combined build is required"
	}
	if !hasTokenPrefix(leftTokens, baseTokens) || !hasTokenPrefix(rightTokens, baseTokens) {
		return "both sides changed metadata, and the shared base flags are not a common prefix; a real combined build is required"
	}
	return "both sides changed metadata, and automatic flag merge could not reconcile them"
}

func summarizeMetadata(metadata string) string {
	trimmed := strings.TrimSpace(metadata)
	if trimmed == "" {
		return "<empty>"
	}
	if len(trimmed) > 160 {
		return trimmed[:157] + "..."
	}
	return trimmed
}

func summarizeManifestState(state manifestState) string {
	if !state.present {
		return "<absent>"
	}
	entry := state.entry
	parts := []string{"kind=" + entry.Kind}
	if entry.Digest != "" {
		parts = append(parts, "digest="+entry.Digest)
	}
	if entry.Target != "" {
		parts = append(parts, "target="+entry.Target)
	}
	if entry.Executable {
		parts = append(parts, "executable=true")
	}
	return strings.Join(parts, ", ")
}

func mapsKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
