package evaluator

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type OutputManifest struct {
	Metadata string
	Entries  map[string]OutputEntry
}

type OutputEntry struct {
	Kind       string
	Digest     string
	Target     string
	Executable bool
}

type outputManifestDiff struct {
	metadataChanged bool
	metadata        string
	entries         map[string]outputManifestDiffEntry
}

type outputManifestDiffEntry struct {
	present bool
	entry   OutputEntry
}

func diffOutputManifest(base, probe OutputManifest) outputManifestDiff {
	diff := outputManifestDiff{}
	if base.Metadata != probe.Metadata {
		diff.metadataChanged = true
		diff.metadata = probe.Metadata
	}

	allPaths := make(map[string]struct{}, len(base.Entries)+len(probe.Entries))
	for path := range base.Entries {
		allPaths[path] = struct{}{}
	}
	for path := range probe.Entries {
		allPaths[path] = struct{}{}
	}
	if len(allPaths) == 0 {
		return diff
	}

	entries := make(map[string]outputManifestDiffEntry)
	for path := range allPaths {
		baseEntry, baseOK := base.Entries[path]
		probeEntry, probeOK := probe.Entries[path]
		switch {
		case baseOK && probeOK && baseEntry == probeEntry:
			continue
		case probeOK:
			entries[path] = outputManifestDiffEntry{present: true, entry: probeEntry}
		default:
			entries[path] = outputManifestDiffEntry{present: false}
		}
	}
	if len(entries) > 0 {
		diff.entries = entries
	}
	return diff
}

func (diff outputManifestDiff) empty() bool {
	return !diff.metadataChanged && len(diff.entries) == 0
}

func outputManifestDiffsCollide(left, right outputManifestDiff) bool {
	if left.metadataChanged && right.metadataChanged && left.metadata != right.metadata {
		return true
	}
	if len(left.entries) == 0 || len(right.entries) == 0 {
		return false
	}
	for path, leftEntry := range left.entries {
		rightEntry, ok := right.entries[path]
		if !ok {
			continue
		}
		if leftEntry != rightEntry {
			return true
		}
	}
	return false
}

type manifestCalculator interface {
	Calculate(path string, info fs.FileInfo) (OutputEntry, error)
}

func BuildOutputManifest(outputDir, metadata string) (OutputManifest, error) {
	manifest := OutputManifest{Metadata: metadata}
	if outputDir == "" {
		return manifest, nil
	}

	entries := make(map[string]OutputEntry)
	err := filepath.WalkDir(outputDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == outputDir {
			return nil
		}

		rel, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		entry, err := buildManifestEntry(path, info)
		if err != nil {
			return err
		}
		entries[rel] = entry
		return nil
	})
	if err != nil {
		return OutputManifest{}, err
	}
	if len(entries) > 0 {
		manifest.Entries = entries
	}
	return manifest, nil
}

func buildManifestEntry(path string, info fs.FileInfo) (OutputEntry, error) {
	calculator, err := newManifestCalculator(path, info)
	if err != nil {
		return OutputEntry{}, err
	}
	entry, err := calculator.Calculate(path, info)
	if err != nil {
		return OutputEntry{}, err
	}
	entry.Executable = info.Mode()&0o111 != 0
	return entry, nil
}

func newManifestCalculator(path string, info fs.FileInfo) (manifestCalculator, error) {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return symlinkManifestCalculator{}, nil
	case info.Mode()&os.ModeType == 0 && strings.HasSuffix(strings.ToLower(path), ".a"):
		return archiveManifestCalculator{}, nil
	case info.Mode()&os.ModeType == 0:
		return rawFileManifestCalculator{}, nil
	default:
		return nil, fmt.Errorf("no manifest calculator for %q", path)
	}
}

type symlinkManifestCalculator struct{}

func (symlinkManifestCalculator) Calculate(path string, _ fs.FileInfo) (OutputEntry, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return OutputEntry{}, err
	}
	target = filepath.ToSlash(target)
	return OutputEntry{
		Kind:   "symlink",
		Target: target,
		Digest: digestBytesShort([]byte(target)),
	}, nil
}

type archiveManifestCalculator struct{}

func (archiveManifestCalculator) Calculate(path string, _ fs.FileInfo) (OutputEntry, error) {
	digest, err := archiveDigest(path)
	if err != nil {
		return OutputEntry{}, err
	}
	return OutputEntry{
		Kind:   "archive",
		Digest: digest,
	}, nil
}

type rawFileManifestCalculator struct{}

func (rawFileManifestCalculator) Calculate(path string, _ fs.FileInfo) (OutputEntry, error) {
	digest, err := fileDigestShort(path)
	if err != nil {
		return OutputEntry{}, err
	}
	return OutputEntry{
		Kind:   "file",
		Digest: digest,
	}, nil
}

func fileDigestShort(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return digestBytesShort(data), nil
}

func digestBytesShort(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

type archiveMemberDigest struct {
	Name   string
	Digest string
}

func archiveDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	members, err := parseArchiveMembers(data)
	if err != nil {
		return "", err
	}
	if len(members) == 0 {
		return digestBytesShort(nil), nil
	}
	var buf bytes.Buffer
	for _, member := range members {
		buf.WriteString(member.Name)
		buf.WriteByte(0)
		buf.WriteString(member.Digest)
		buf.WriteByte('\n')
	}
	return digestBytesShort(buf.Bytes()), nil
}

func parseArchiveMembers(data []byte) ([]archiveMemberDigest, error) {
	const globalHeader = "!<arch>\n"
	const fileHeaderLen = 60
	if len(data) < len(globalHeader) || string(data[:len(globalHeader)]) != globalHeader {
		return nil, fmt.Errorf("invalid archive header")
	}

	offset := len(globalHeader)
	var stringTable []byte
	members := make([]archiveMemberDigest, 0)
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
		members = append(members, archiveMemberDigest{
			Name:   name,
			Digest: digestBytesShort(body),
		})
	}
	return members, nil
}

func isArchiveSpecialName(name string) bool {
	switch name {
	case "/", "/SYM64/", "__.SYMDEF", "__.SYMDEF SORTED", "__.SYMDEF_64", "__.SYMDEF SORTED_64", "//":
		return true
	default:
		return false
	}
}

func resolveArchiveMember(name string, payload, stringTable []byte) (string, []byte, error) {
	switch {
	case strings.HasPrefix(name, "#1/"):
		n, err := strconv.Atoi(strings.TrimPrefix(name, "#1/"))
		if err != nil || n < 0 || n > len(payload) {
			return "", nil, fmt.Errorf("invalid BSD archive name %q", name)
		}
		return string(payload[:n]), payload[n:], nil
	case strings.HasPrefix(name, "/") && len(name) > 1 && isDecimal(name[1:]):
		if len(stringTable) == 0 {
			return "", nil, fmt.Errorf("archive member %q missing string table", name)
		}
		idx, _ := strconv.Atoi(name[1:])
		resolved, err := archiveStringTableName(stringTable, idx)
		if err != nil {
			return "", nil, err
		}
		return resolved, payload, nil
	default:
		return strings.TrimSuffix(name, "/"), payload, nil
	}
}

func archiveStringTableName(table []byte, idx int) (string, error) {
	if idx < 0 || idx >= len(table) {
		return "", fmt.Errorf("archive string table offset %d out of range", idx)
	}
	rest := table[idx:]
	end := bytes.IndexByte(rest, '\n')
	if end < 0 {
		end = len(rest)
	}
	return strings.TrimSuffix(string(rest[:end]), "/"), nil
}

func isDecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
