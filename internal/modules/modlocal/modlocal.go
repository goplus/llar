// Package modlocal resolves local filesystem patterns into module paths
// and their corresponding directories. It scans for versions.json files
// to discover modules on disk.
package modlocal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goplus/llar/mod/versions"
)

// Module represents a module discovered from the local filesystem.
type Module struct {
	Path    string // module path (e.g. "madler/zlib")
	Dir     string // absolute directory containing the formula
	Version string // optional pinned version from pattern
}

// Resolve resolves a local file pattern to a list of modules.
//   - pattern="" (from "."): walk up from cwd to find versions.json
//   - pattern="owner/repo": read cwd/owner/repo/versions.json
//   - pattern="owner/...": scan cwd/owner/*/versions.json
//   - pattern="...": scan cwd/*/*/versions.json (two-level)
func Resolve(cwd, pattern string) ([]Module, error) {
	switch {
	case pattern == "":
		return resolveCurrentDir(cwd)
	case pattern == "..." || strings.HasSuffix(pattern, "/..."):
		return resolveWildcard(cwd, pattern)
	default:
		return resolveSingleLocal(cwd, pattern)
	}
}

// resolveCurrentDir finds the module in the current directory by walking up
// to find a versions.json file, then reading its path field.
func resolveCurrentDir(cwd string) ([]Module, error) {
	dir := cwd
	for {
		vFile := filepath.Join(dir, "versions.json")
		if _, err := os.Stat(vFile); err == nil {
			v, err := versions.Parse(vFile, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to parse %s: %w", vFile, err)
			}
			if v.Path == "" {
				return nil, fmt.Errorf("versions.json at %s has no path field", dir)
			}
			return []Module{{Path: v.Path, Dir: dir}}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("no versions.json found in %s or any parent directory", cwd)
		}
		dir = parent
	}
}

// resolveSingleLocal resolves a single local module at cwd/pattern.
func resolveSingleLocal(cwd, pattern string) ([]Module, error) {
	dir := filepath.Join(cwd, pattern)
	vFile := filepath.Join(dir, "versions.json")
	v, err := versions.Parse(vFile, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", vFile, err)
	}
	if v.Path == "" {
		return nil, fmt.Errorf("versions.json at %s has no path field", dir)
	}
	return []Module{{Path: v.Path, Dir: dir}}, nil
}

// resolveWildcard scans for modules matching a ... pattern.
// "..." scans cwd/*/*/versions.json, "owner/..." scans cwd/owner/*/versions.json.
func resolveWildcard(cwd, pattern string) ([]Module, error) {
	var glob string
	if pattern == "..." {
		glob = filepath.Join(cwd, "*", "*", "versions.json")
	} else {
		owner := strings.TrimSuffix(pattern, "/...")
		glob = filepath.Join(cwd, owner, "*", "versions.json")
	}

	matches, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no modules found matching pattern %q", pattern)
	}

	var result []Module
	for _, vFile := range matches {
		dir := filepath.Dir(vFile)

		// Skip directories starting with . or _ (same convention as Go)
		base := filepath.Base(dir)
		if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
			continue
		}
		if pattern == "..." {
			ownerBase := filepath.Base(filepath.Dir(dir))
			if strings.HasPrefix(ownerBase, ".") || strings.HasPrefix(ownerBase, "_") {
				continue
			}
		}

		v, err := versions.Parse(vFile, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", vFile, err)
		}
		if v.Path == "" {
			continue
		}
		result = append(result, Module{Path: v.Path, Dir: dir})
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no valid modules found matching pattern %q", pattern)
	}
	return result, nil
}
