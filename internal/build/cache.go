package build

import (
	"encoding/json"
	"os"
	"time"

	"github.com/goplus/llar/formula"
)

const cacheFile = ".cache.json"

// buildCache contains metadata about a successful build.
type buildCache struct {
	BuildResult formula.BuildResult `json:"build_result"`
	BuildTime   time.Time           `json:"build_time"`
}

func loadBuildCache(path string) (*buildCache, error) {
	data, err := os.ReadFile(path)
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
