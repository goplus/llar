package build

import (
	"encoding/json"
	"io/fs"
	"os"
	"time"

	"github.com/goplus/llar/formula"
)

const cacheFile = ".cache.json"

// buildEntry contains metadata about a single successful build.
type buildEntry struct {
	BuildResult formula.BuildResult `json:"build_result"`
	BuildTime   time.Time           `json:"build_time"`
}

// buildCache maps "version-matrixString" keys to their build entries.
// Example:
//
//	{
//	  "1.0.0-amd64-linux": { "build_result": {...}, "build_time": "..." },
//	  "1.0.0-arm64-linux": { "build_result": {...}, "build_time": "..." }
//	}
type buildCache struct {
	Cache map[string]*buildEntry `json:"cache"`
}

// cacheKey returns the cache key for a given version and matrix combination.
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

func loadCacheFS(fsys fs.FS) (*buildCache, error) {
	data, err := fs.ReadFile(fsys, cacheFile)
	if err != nil {
		return nil, err
	}
	var cache buildCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func saveBuildCache(path string, cache *buildCache) error {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
