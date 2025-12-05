package loader

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	ixgoLoader "github.com/goplus/ixgo/load"
)

// resolver handles Go module dependency resolution by wrapping ixgo's ListDriver
// and providing fallback to go mod commands.
type resolver struct {
	listDrvier *ixgoLoader.ListDriver
}

// newResolver creates a new resolver instance.
func newResolver() *resolver {
	return &resolver{listDrvier: new(ixgoLoader.ListDriver)}
}

// Lookup resolves a Go module path to its directory location.
// It first tries the ListDriver cache, then falls back to go mod commands.
// If go.mod doesn't exist in root, it initializes a new module.
func (g *resolver) Lookup(root string, path string) (dir string, found bool) {
	dir, found = g.listDrvier.Lookup(root, path)
	if found {
		return
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); os.IsNotExist(err) {
		execCommand(root, "go", "mod", "init", filepath.Base(root))
	}
	execCommand(root, "go", "get", path)

	ret := execCommand(root, "go", "mod", "download", "-json", path)

	var modDownload struct {
		Dir string
	}
	json.Unmarshal(ret, &modDownload)

	if modDownload.Dir != "" {
		found = true
		dir = modDownload.Dir
	}

	return
}

// execCommand executes a command in the specified directory and returns its output.
// It panics if the command fails, making error handling explicit at the call site.
func execCommand(dir, mainCmd string, subcmd ...string) []byte {
	cmd := exec.Command(mainCmd, subcmd...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	ret, err := cmd.Output()
	if err != nil {
		panic(string(ret))
	}
	return ret
}
