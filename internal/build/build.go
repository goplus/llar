package build

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	classfile "github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/formula/repo"
	"github.com/goplus/llar/internal/modules"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/mod/module"
)

type Builder struct {
	store        *repo.Store
	matrix       string
	workspaceDir string
	initOnce     sync.Once
	newRepo      func(repoPath string) (vcs.Repo, error) // defaults to vcs.NewRepo
}

type Result struct {
	Metadata string
}

type Options struct {
	Store        *repo.Store
	MatrixStr    string
	WorkspaceDir string
}

func defaultWorkspaceDir() (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	workspaceDir := filepath.Join(userCacheDir, ".llar", "workspaces")

	if err := os.MkdirAll(workspaceDir, 0700); err != nil {
		return "", err
	}
	return workspaceDir, nil
}

// NewBuilder creates a new Builder.
func NewBuilder(opts Options) (*Builder, error) {
	workspaceDir := opts.WorkspaceDir
	if workspaceDir == "" {
		var err error
		workspaceDir, err = defaultWorkspaceDir()
		if err != nil {
			return nil, err
		}
	}
	return &Builder{
		store:        opts.Store,
		matrix:       opts.MatrixStr,
		workspaceDir: workspaceDir,
		newRepo:      vcs.NewRepo,
	}, nil
}

// constructBuildList reorders the MVS build list into a valid build order
// using DFS post-order traversal: leaves (modules with no deps) come first,
// the main module (root) comes last.
//
// This method lives in the build module (rather than modules) because build
// ordering may change in the future, e.g. to support parallel builds.
//
// Example:
//
//	Graph: A -> B -> C, A -> D -> C
//	Input  (MVS BuildList): [A@1.0.0, B@1.2.0, C@1.2.0, D@1.0.0]
//	Output (build order):   [C@1.2.0, B@1.2.0, D@1.0.0, A@1.0.0]
func (b *Builder) constructBuildList(targets []*modules.Module) []*modules.Module {
	byPath := make(map[string]*modules.Module, len(targets))
	for _, m := range targets {
		byPath[m.Path] = m
	}

	var order []*modules.Module
	visited := make(map[string]bool, len(targets))

	var visit func(m *modules.Module)
	visit = func(m *modules.Module) {
		if visited[m.Path] {
			return
		}
		visited[m.Path] = true
		for _, dep := range m.Deps {
			if d, ok := byPath[dep.Path]; ok {
				visit(d)
			}
		}
		order = append(order, m)
	}

	if len(targets) > 0 {
		visit(targets[0])
	}

	return order
}

// resolveModTransitiveDeps collects all transitive dependencies of mod from
// the MVS build list and returns them in build order (DFS post-order: leaves first).
//
// modules.Module.Deps only stores direct dependencies so that the build module
// can reorder freely. This method reconstructs the full transitive set by
// walking the dependency graph through targets (which use MVS-selected versions).
//
// Case 1 - Simple:
//
//	Graph:  A -> B -> C -> D
//	Input:  targets=[A@1.0.0, B@1.2.0, C@1.2.0, D@1.0.0], mod=C@1.2.0
//	Output: [D@1.0.0]
//
// Case 2 - Diamond (MVS version selection):
//
//	Graph:  A -> B -> C, A -> D -> C   (MVS selects C@2.0.0)
//	Input:  targets=[A@1.0.0, B@1.2.0, C@2.0.0, D@1.0.0], mod=B@1.2.0
//	Output: [C@2.0.0]
//
// Case 3 - Diamond with transitive dep:
//
//	Graph:  A -> B -> C, A -> D -> C -> E   (MVS selects C@2.0.0)
//	Input:  targets=[A@1.0.0, B@1.2.0, C@2.0.0, D@1.0.0, E@1.0.0], mod=B@1.2.0
//	Output: [E@1.0.0, C@2.0.0]
//
// Case 4 - Multiple direct deps (alphabet order):
//
//	Graph:  A -> B -> C, A -> B -> D
//	Input:  targets=[A@1.0.0, B@1.2.0, C@1.1.0, D@1.0.0], mod=B@1.2.0
//	Output: [C@1.1.0, D@1.0.0]
//
// Case 5 - Dep ordering by topology:
//
//	Graph:  A -> B -> C -> D, A -> B -> D
//	Input:  targets=[A@1.0.0, B@1.2.0, C@1.1.0, D@1.2.0], mod=B@1.2.0
//	Output: [D@1.2.0, C@1.1.0]  (D before C because B depends on both D and C directly, while C depends on D transitively)
func (b *Builder) resolveModTransitiveDeps(targets []*modules.Module, mod *modules.Module) []module.Version {
	byPath := make(map[string]*modules.Module, len(targets))
	for _, m := range targets {
		byPath[m.Path] = m
	}

	var order []module.Version
	visited := make(map[string]bool)
	visited[mod.Path] = true

	var visit func(m *modules.Module)
	visit = func(m *modules.Module) {
		if visited[m.Path] {
			return
		}
		visited[m.Path] = true
		for _, dep := range m.Deps {
			if d, ok := byPath[dep.Path]; ok {
				visit(d)
			}
		}
		order = append(order, module.Version{Path: m.Path, Version: m.Version})
	}

	for _, dep := range mod.Deps {
		if d, ok := byPath[dep.Path]; ok {
			visit(d)
		}
	}

	return order
}

func (b *Builder) Build(ctx context.Context, targets []*modules.Module) ([]Result, error) {
	builtResults := make(map[module.Version]classfile.BuildResult)

	build := func(mod *modules.Module) (Result, error) {
		unlock, err := b.store.LockModule(mod.ModPath)
		if err != nil {
			return Result{}, err
		}
		defer unlock()

		// Check cache
		cache, err := b.loadCache(mod.Path)
		if err == nil {
			if entry, ok := cache.get(mod.Version, b.matrix); ok {
				return Result{Metadata: entry.Metadata}, nil
			}
		}

		// TODO(MeteorsLiu): Source cache dir
		tmpSourceDir, err := os.MkdirTemp("", fmt.Sprintf("source-%s-%s*", strings.ReplaceAll(mod.Path, "/", "-"), mod.Version))
		if err != nil {
			return Result{}, err
		}
		defer os.RemoveAll(tmpSourceDir)

		// Before we start to build, clone source to tmpSourceDir
		// And switch current dir to it.
		// TODO(MeteorsLiu): Support different code host
		repo, err := b.newRepo(fmt.Sprintf("github.com/%s", mod.Path))
		if err != nil {
			return Result{}, err
		}
		if err := repo.Sync(ctx, mod.Version, "", tmpSourceDir); err != nil {
			return Result{}, err
		}

		installDir, err := b.installDir(mod.Path, mod.Version)
		if err != nil {
			return Result{}, err
		}
		if err := os.MkdirAll(installDir, 0o755); err != nil {
			return Result{}, err
		}

		getOutputDir := func(_ string, m module.Version) (string, error) {
			return b.installDir(m.Path, m.Version)
		}
		buildContext := classfile.NewContext(tmpSourceDir, installDir, b.matrix, getOutputDir)

		// Inject results of already-built dependencies
		for modVer, result := range builtResults {
			buildContext.AddBuildResult(modVer, result)
		}

		project := &classfile.Project{Deps: b.resolveModTransitiveDeps(targets, mod), SourceFS: mod.FS.(fs.ReadFileFS)}

		// Ready! Go!
		if err := os.Chdir(tmpSourceDir); err != nil {
			return Result{}, err
		}

		var out classfile.BuildResult
		mod.OnBuild(buildContext, project, &out)

		if len(out.Errs()) > 0 {
			return Result{}, errors.Join(out.Errs()...)
		}

		// Save to cache
		if cache == nil {
			cache = &buildCache{}
		}
		cache.set(mod.Version, b.matrix, &buildEntry{
			Metadata:  out.Metadata(),
			BuildTime: time.Now(),
		})
		if err := b.saveCache(mod.Path, cache); err != nil {
			return Result{}, err
		}

		return Result{Metadata: out.Metadata()}, nil
	}

	var results []Result

	// Save current environment and restore it after OnBuild,
	// that's because OnBuild may break environment
	// TODO(MeteorsLiu): Switch to sandbox to run OnBuild
	savedEnv := os.Environ()
	defer func() {
		os.Clearenv()
		for _, env := range savedEnv {
			k, v, _ := strings.Cut(env, "=")
			os.Setenv(k, v)
		}
	}()

	// TODO(MeteorsLiu): Parallel build
	for _, target := range b.constructBuildList(targets) {
		result, err := build(target)
		if err != nil {
			return nil, err
		}

		// Track result for downstream dependencies
		modVer := module.Version{Path: target.Path, Version: target.Version}
		br := classfile.BuildResult{}
		if result.Metadata != "" {
			br.SetMetadata(result.Metadata)
		}
		builtResults[modVer] = br

		results = append(results, result)
	}
	return results, nil
}
