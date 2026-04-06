package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	classfile "github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/formula/repo"
	"github.com/goplus/llar/internal/modules"
	"github.com/goplus/llar/internal/trace"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/mod/module"
)

type Builder struct {
	store        repo.Store
	matrix       string
	runTest      bool
	trace        bool
	workspaceDir string
	newRepo      func(repoPath string) (vcs.Repo, error) // defaults to vcs.NewRepo
}

type Result struct {
	Metadata         string
	OutputDir        string
	Trace            []trace.Record
	TraceEvents      []trace.Event
	TraceScope       trace.Scope
	TraceDiagnostics trace.ParseDiagnostics
	InputDigests     map[string]string
	ReplayReady      bool
}

type Options struct {
	Store        repo.Store
	MatrixStr    string
	RunTest      bool
	Trace        bool
	WorkspaceDir string
}

var captureOnBuildTrace = trace.CaptureLockedThread

func (b *Builder) traceRoots(targets []*modules.Module, mod *modules.Module, sourceDir, installDir string) ([]string, error) {
	roots := []string{sourceDir, installDir}
	for _, dep := range b.resolveModTransitiveDeps(targets, mod) {
		dir, err := b.installDir(dep.Path, dep.Version)
		if err != nil {
			return nil, err
		}
		roots = append(roots, dir)
	}
	return roots, nil
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
		runTest:      opts.RunTest,
		trace:        opts.Trace,
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
	traceTarget := module.Version{}
	if b.trace && len(targets) > 0 {
		traceTarget = module.Version{Path: targets[0].Path, Version: targets[0].Version}
	}

	build := func(mod *modules.Module) (Result, error) {
		traceEnabled := b.trace && mod.Path == traceTarget.Path && mod.Version == traceTarget.Version

		unlock, err := b.store.LockModule(mod.Path)
		if err != nil {
			return Result{}, err
		}
		defer unlock()

		// When onTest is requested, bypass the build cache so test execution
		// cannot be skipped by a cached build hit.
		var cache *buildCache
		if !b.runTest && !traceEnabled {
			cache, err = b.loadCache(mod.Path)
			if err == nil {
				if entry, ok := cache.get(mod.Version, b.matrix); ok {
					dir, _ := b.installDir(mod.Path, mod.Version)
					return Result{Metadata: entry.Metadata, OutputDir: dir}, nil
				}
			}
		}

		// TODO(MeteorsLiu): Source cache dir
		tmpSourceDir, cleanupSourceDir, err := b.sourceDir(mod.Path, mod.Version, traceEnabled)
		if err != nil {
			return Result{}, err
		}
		defer cleanupSourceDir()

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
		cwd, err := os.Getwd()
		if err != nil {
			return Result{}, err
		}
		defer func() {
			_ = os.Chdir(cwd)
		}()
		if err := os.Chdir(tmpSourceDir); err != nil {
			return Result{}, err
		}

		var out classfile.BuildResult
		var records []trace.Record
		var events []trace.Event
		var traceDiagnostics trace.ParseDiagnostics
		traceScope := trace.Scope{
			SourceRoot:  tmpSourceDir,
			BuildRoot:   filepath.Join(tmpSourceDir, "_build"),
			InstallRoot: installDir,
		}
		runOnBuild := func() error {
			mod.OnBuild(buildContext, project, &out)
			return nil
		}
		if traceEnabled {
			traceRoots, err := b.traceRoots(targets, mod, tmpSourceDir, installDir)
			if err != nil {
				return Result{}, err
			}
			traceResult, err := captureOnBuildTrace(ctx, trace.CaptureOptions{
				RootCwd:   tmpSourceDir,
				KeepRoots: traceRoots,
			}, runOnBuild)
			if err != nil {
				return Result{}, err
			}
			records = traceResult.Records
			events = traceResult.Events
			traceDiagnostics = traceResult.Diagnostics
			traceScope.BuildRoot = inferTraceBuildRoot(records, traceScope)
			traceScope.KeepRoots = slices.Clone(traceRoots)
		} else {
			if err := runOnBuild(); err != nil {
				return Result{}, err
			}
		}

		if len(out.Errs()) > 0 {
			return Result{}, errors.Join(out.Errs()...)
		}
		if b.runTest && mod.OnTest != nil {
			var testOut classfile.BuildResult
			mod.OnTest(buildContext, project, &testOut)
			if len(testOut.Errs()) > 0 {
				return Result{}, fmt.Errorf("onTest failed for %s@%s: %w", mod.Path, mod.Version, errors.Join(testOut.Errs()...))
			}
		}

		// Save to cache
		if !b.runTest {
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
		}

		return Result{
			Metadata:         out.Metadata(),
			OutputDir:        installDir,
			Trace:            records,
			TraceEvents:      events,
			TraceScope:       traceScope,
			TraceDiagnostics: traceDiagnostics,
			InputDigests:     collectTraceInputDigests(records, traceScope),
			ReplayReady:      traceEnabled,
		}, nil
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

func inferTraceBuildRoot(records []trace.Record, scope trace.Scope) string {
	sourceRoot := filepath.Clean(scope.SourceRoot)
	if sourceRoot == "" {
		return filepath.Clean(scope.BuildRoot)
	}
	installRoot := filepath.Clean(scope.InstallRoot)
	candidates := make([]string, 0, len(records))
	for _, rec := range records {
		for _, path := range rec.Changes {
			dir, ok := traceBuildCandidateDir(path, sourceRoot, installRoot)
			if !ok {
				continue
			}
			candidates = append(candidates, dir)
		}
	}
	if len(candidates) == 0 {
		return filepath.Clean(scope.BuildRoot)
	}
	root := filepath.Clean(candidates[0])
	for _, dir := range candidates[1:] {
		root = commonPathPrefix(root, filepath.Clean(dir))
		if root == sourceRoot {
			break
		}
	}
	if root == "" || root == "." || root == sourceRoot {
		return filepath.Clean(scope.BuildRoot)
	}
	return root
}

func traceBuildCandidateDir(path, sourceRoot, installRoot string) (string, bool) {
	path = filepath.Clean(path)
	if path == "" {
		return "", false
	}
	if path == sourceRoot || path == installRoot {
		return "", false
	}
	if !isWithinRoot(path, sourceRoot) {
		return "", false
	}
	if installRoot != "" && isWithinRoot(path, installRoot) {
		return "", false
	}
	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir():
		return path, true
	case err == nil:
		return filepath.Dir(path), true
	case errors.Is(err, os.ErrNotExist):
		if strings.HasSuffix(path, string(filepath.Separator)) {
			return strings.TrimSuffix(path, string(filepath.Separator)), true
		}
		return filepath.Dir(path), true
	default:
		return "", false
	}
}

func commonPathPrefix(left, right string) string {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if left == right {
		return left
	}
	leftParts := splitPathParts(left)
	rightParts := splitPathParts(right)
	n := minInt(len(leftParts), len(rightParts))
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		if leftParts[i] != rightParts[i] {
			break
		}
		parts = append(parts, leftParts[i])
	}
	if len(parts) == 0 {
		return ""
	}
	if filepath.IsAbs(left) {
		return string(filepath.Separator) + filepath.Join(parts...)
	}
	return filepath.Join(parts...)
}

func splitPathParts(path string) []string {
	path = filepath.Clean(path)
	if path == "." || path == string(filepath.Separator) {
		return nil
	}
	if filepath.IsAbs(path) {
		path = strings.TrimPrefix(path, string(filepath.Separator))
	}
	return strings.Split(path, string(filepath.Separator))
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func isWithinRoot(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

func (b *Builder) sourceDir(modPath, version string, preserve bool) (string, func(), error) {
	if !preserve {
		dir, err := os.MkdirTemp("", fmt.Sprintf("source-%s-%s*", strings.ReplaceAll(modPath, "/", "-"), version))
		if err != nil {
			return "", nil, err
		}
		return dir, func() {
			_ = os.RemoveAll(dir)
		}, nil
	}

	escaped, err := module.EscapePath(modPath)
	if err != nil {
		return "", nil, err
	}
	dir := filepath.Join(b.workspaceDir, ".trace-src", fmt.Sprintf("%s@%s-%s", escaped, version, b.matrix))
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return "", nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	return dir, func() {}, nil
}

func collectTraceInputDigests(records []trace.Record, scope trace.Scope) map[string]string {
	buildRoot := filepath.Clean(scope.BuildRoot)
	if buildRoot == "" || len(records) == 0 {
		return nil
	}

	paths := make(map[string]struct{})
	for _, rec := range records {
		for _, path := range rec.Inputs {
			if !pathWithinRoot(path, buildRoot) {
				continue
			}
			paths[path] = struct{}{}
		}
		for _, path := range rec.Changes {
			if !pathWithinRoot(path, buildRoot) {
				continue
			}
			paths[path] = struct{}{}
		}
	}

	digests := make(map[string]string, len(paths))
	for path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		sum, err := fileDigest(path)
		if err != nil {
			continue
		}
		digests[path] = sum
	}
	if len(digests) == 0 {
		return nil
	}
	return digests
}

func pathWithinRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8]), nil
}
