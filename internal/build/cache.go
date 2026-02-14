package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/goplus/llar/mod/module"
)

// Workspace directory layout:
//
//	workspaceDir/
//	  <escaped>/                      # module-level dir (cacheDir)
//	    .cache.json                   # build cache: maps "version-matrix" → buildEntry
//	  <escaped>@<version>-<matrix>/   # build output dir (installDir)
//	    include/
//	    lib/
//	    ...
const cacheFile = ".cache.json"

// buildEntry contains metadata about a single successful build.
type buildEntry struct {
	Metadata  string    `json:"metadata"`
	BuildTime time.Time `json:"build_time"`
}

// buildCache maps "version-matrixString" keys to their build entries.
type buildCache struct {
	Cache map[string]*buildEntry `json:"cache"`
}

func cacheKey(version, matrix string) string {
	return version + "-" + matrix
}

func (c *buildCache) get(version, matrix string) (*buildEntry, bool) {
	entry, ok := c.Cache[cacheKey(version, matrix)]
	return entry, ok
}

func (c *buildCache) set(version, matrix string, entry *buildEntry) {
	if c.Cache == nil {
		c.Cache = make(map[string]*buildEntry)
	}
	c.Cache[cacheKey(version, matrix)] = entry
}

// cacheDir returns the module-level directory for cache storage: workspaceDir/<escapedPath>.
func (b *Builder) cacheDir(modPath string) (string, error) {
	escaped, err := module.EscapePath(modPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(b.workspaceDir, escaped), nil
}

// installDir returns the build output directory: workspaceDir/<escapedPath>@<version>-<matrix>.
func (b *Builder) installDir(modPath, version string) (string, error) {
	escaped, err := module.EscapePath(modPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(b.workspaceDir, fmt.Sprintf("%s@%s-%s", escaped, version, b.matrix)), nil
}

// loadCache reads the cache file for a module from the workspace directory.
func (b *Builder) loadCache(modPath string) (*buildCache, error) {
	dir, err := b.cacheDir(modPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, cacheFile))
	if err != nil {
		return nil, err
	}
	var cache buildCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

// saveCache writes the cache file for a module to the workspace directory.
func (b *Builder) saveCache(modPath string, cache *buildCache) error {
	dir, err := b.cacheDir(modPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, cacheFile), data, 0o644)
}
