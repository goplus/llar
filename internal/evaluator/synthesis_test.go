package evaluator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goplus/llar/internal/trace"
)

func TestSynthesizeOutputTrees_UsesDirectMergeMode(t *testing.T) {
	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()

	writeMergeFile(t, filepath.Join(baseDir, "share", "config.txt"), []byte("base\n"), 0o644)
	writeMergeFile(t, filepath.Join(leftDir, "share", "config.txt"), []byte("left\n"), 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "share", "config.txt"), []byte("base\n"), 0o644)

	baseManifest, err := BuildOutputManifest(baseDir, "")
	if err != nil {
		t.Fatalf("BuildOutputManifest(base) error: %v", err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "")
	if err != nil {
		t.Fatalf("BuildOutputManifest(left) error: %v", err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "")
	if err != nil {
		t.Fatalf("BuildOutputManifest(right) error: %v", err)
	}

	result, err := synthesizeOutputTrees(
		context.Background(),
		ProbeResult{OutputDir: baseDir, OutputManifest: baseManifest},
		ProbeResult{OutputDir: leftDir, OutputManifest: leftManifest},
		ProbeResult{OutputDir: rightDir, OutputManifest: rightManifest},
	)
	if err != nil {
		t.Fatalf("synthesizeOutputTrees() error: %v", err)
	}
	if result.Mode != OutputSynthesisModeDirectMerge {
		t.Fatalf("mode = %q, want %q", result.Mode, OutputSynthesisModeDirectMerge)
	}
	if !result.Clean() {
		t.Fatalf("synthesis issues = %#v, want clean", result.Issues)
	}
	if _, ok := result.AsMergeResult(); !ok {
		t.Fatal("AsMergeResult() = !ok, want direct merge result")
	}
	_ = os.RemoveAll(result.Root)
}

func TestSynthesizeOutputTrees_FallsBackToRootReplay(t *testing.T) {
	baseSource := t.TempDir()
	leftSource := t.TempDir()
	rightSource := t.TempDir()
	script := "#!/bin/sh\nset -eu\nproto=0\nlog=0\nout=\nfor arg in \"$@\"; do\n  case \"$arg\" in\n    --proto=*) proto=${arg#--proto=} ;;\n    --log=*) log=${arg#--log=} ;;\n    --out=*) out=${arg#--out=} ;;\n  esac\ndone\nmkdir -p \"$(dirname \"$out\")\"\nprintf 'state=proto:%s,log:%s\\n' \"$proto\" \"$log\" > \"$out\"\n"
	for _, root := range []string{baseSource, leftSource, rightSource} {
		scriptPath := filepath.Join(root, "emit-config.sh")
		if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
			t.Fatalf("WriteFile(script): %v", err)
		}
	}

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	writeMergeFile(t, filepath.Join(baseDir, "share", "config.txt"), []byte("state=proto:0,log:0\n"), 0o644)
	writeMergeFile(t, filepath.Join(leftDir, "share", "config.txt"), []byte("state=proto:1,log:0\n"), 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "share", "config.txt"), []byte("state=proto:0,log:1\n"), 0o644)

	baseManifest, err := BuildOutputManifest(baseDir, "")
	if err != nil {
		t.Fatalf("BuildOutputManifest(base) error: %v", err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "")
	if err != nil {
		t.Fatalf("BuildOutputManifest(left) error: %v", err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "")
	if err != nil {
		t.Fatalf("BuildOutputManifest(right) error: %v", err)
	}

	baseScript := filepath.Join(baseSource, "emit-config.sh")
	leftScript := filepath.Join(leftSource, "emit-config.sh")
	rightScript := filepath.Join(rightSource, "emit-config.sh")
	baseProbe := ProbeResult{
		Records: []trace.Record{{
			PID:       100,
			ParentPID: 0,
			Argv: []string{
				baseScript,
				"--proto=0",
				"--log=0",
				"--out=" + filepath.Join(baseDir, "share", "config.txt"),
			},
			Cwd: baseSource,
			Env: []string{"PATH=" + os.Getenv("PATH")},
			Changes: []string{
				filepath.Join(baseDir, "share", "config.txt"),
			},
		}},
		OutputDir:      baseDir,
		OutputManifest: baseManifest,
		Scope: trace.Scope{
			SourceRoot:  baseSource,
			BuildRoot:   filepath.Join(baseSource, "_build"),
			InstallRoot: baseDir,
		},
		ReplayReady: true,
	}
	leftProbe := ProbeResult{
		Records: []trace.Record{{
			PID:       200,
			ParentPID: 0,
			Argv: []string{
				leftScript,
				"--proto=1",
				"--log=0",
				"--out=" + filepath.Join(leftDir, "share", "config.txt"),
			},
			Cwd: leftSource,
			Env: []string{"PATH=" + os.Getenv("PATH")},
			Changes: []string{
				filepath.Join(leftDir, "share", "config.txt"),
			},
		}},
		OutputDir:      leftDir,
		OutputManifest: leftManifest,
		Scope: trace.Scope{
			SourceRoot:  leftSource,
			BuildRoot:   filepath.Join(leftSource, "_build"),
			InstallRoot: leftDir,
		},
		ReplayReady: true,
	}
	rightProbe := ProbeResult{
		Records: []trace.Record{{
			PID:       300,
			ParentPID: 0,
			Argv: []string{
				rightScript,
				"--proto=0",
				"--log=1",
				"--out=" + filepath.Join(rightDir, "share", "config.txt"),
			},
			Cwd: rightSource,
			Env: []string{"PATH=" + os.Getenv("PATH")},
			Changes: []string{
				filepath.Join(rightDir, "share", "config.txt"),
			},
		}},
		OutputDir:      rightDir,
		OutputManifest: rightManifest,
		Scope: trace.Scope{
			SourceRoot:  rightSource,
			BuildRoot:   filepath.Join(rightSource, "_build"),
			InstallRoot: rightDir,
		},
		ReplayReady: true,
	}

	result, err := synthesizeOutputTrees(context.Background(), baseProbe, leftProbe, rightProbe)
	if err != nil {
		t.Fatalf("synthesizeOutputTrees() error: %v", err)
	}
	if result.Mode != OutputSynthesisModeRootReplay {
		t.Fatalf("mode = %q, want %q", result.Mode, OutputSynthesisModeRootReplay)
	}
	if !result.Clean() {
		t.Fatalf("synthesis issues = %#v, want clean replay", result.Issues)
	}
	got, err := os.ReadFile(filepath.Join(result.Root, "share", "config.txt"))
	if err != nil {
		t.Fatalf("ReadFile(replay output): %v", err)
	}
	if string(got) != "state=proto:1,log:1\n" {
		t.Fatalf("replay output = %q, want combined state", string(got))
	}
	_ = os.RemoveAll(result.Root)
}

func TestSynthesizeOutputTrees_RootReplayPreservesBaseOutputsForUnselectedRoots(t *testing.T) {
	baseSource := t.TempDir()
	leftSource := t.TempDir()
	rightSource := t.TempDir()

	staticScript := "#!/bin/sh\nset -eu\nout=\nfor arg in \"$@\"; do\n  case \"$arg\" in\n    --out=*) out=${arg#--out=} ;;\n  esac\ndone\nmkdir -p \"$(dirname \"$out\")\"\nprintf 'static=base\\n' > \"$out\"\n"
	configScript := "#!/bin/sh\nset -eu\nproto=0\nlog=0\nout=\nfor arg in \"$@\"; do\n  case \"$arg\" in\n    --proto=*) proto=${arg#--proto=} ;;\n    --log=*) log=${arg#--log=} ;;\n    --out=*) out=${arg#--out=} ;;\n  esac\ndone\nmkdir -p \"$(dirname \"$out\")\"\nprintf 'config=proto:%s,log:%s\\n' \"$proto\" \"$log\" > \"$out\"\n"
	for _, root := range []string{baseSource, leftSource, rightSource} {
		if err := os.WriteFile(filepath.Join(root, "emit-static.sh"), []byte(staticScript), 0o755); err != nil {
			t.Fatalf("WriteFile(static script): %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "emit-config.sh"), []byte(configScript), 0o755); err != nil {
			t.Fatalf("WriteFile(config script): %v", err)
		}
	}

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	writeMergeFile(t, filepath.Join(baseDir, "share", "static.txt"), []byte("static=base\n"), 0o644)
	writeMergeFile(t, filepath.Join(baseDir, "share", "config.txt"), []byte("config=proto:0,log:0\n"), 0o644)
	writeMergeFile(t, filepath.Join(leftDir, "share", "static.txt"), []byte("static=base\n"), 0o644)
	writeMergeFile(t, filepath.Join(leftDir, "share", "config.txt"), []byte("config=proto:1,log:0\n"), 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "share", "static.txt"), []byte("static=base\n"), 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "share", "config.txt"), []byte("config=proto:0,log:1\n"), 0o644)

	baseManifest, _ := BuildOutputManifest(baseDir, "")
	leftManifest, _ := BuildOutputManifest(leftDir, "")
	rightManifest, _ := BuildOutputManifest(rightDir, "")

	makeProbe := func(sourceRoot, outputDir string, manifest OutputManifest, proto, log string, pidBase int64) ProbeResult {
		return ProbeResult{
			Records: []trace.Record{
				{
					PID:       pidBase,
					ParentPID: 0,
					Argv: []string{
						filepath.Join(sourceRoot, "emit-static.sh"),
						"--out=" + filepath.Join(outputDir, "share", "static.txt"),
					},
					Cwd: sourceRoot,
					Env: []string{"PATH=" + os.Getenv("PATH")},
					Changes: []string{
						filepath.Join(outputDir, "share", "static.txt"),
					},
				},
				{
					PID:       pidBase + 1,
					ParentPID: 0,
					Argv: []string{
						filepath.Join(sourceRoot, "emit-config.sh"),
						"--proto=" + proto,
						"--log=" + log,
						"--out=" + filepath.Join(outputDir, "share", "config.txt"),
					},
					Cwd: sourceRoot,
					Env: []string{"PATH=" + os.Getenv("PATH")},
					Changes: []string{
						filepath.Join(outputDir, "share", "config.txt"),
					},
				},
			},
			OutputDir:      outputDir,
			OutputManifest: manifest,
			Scope: trace.Scope{
				SourceRoot:  sourceRoot,
				BuildRoot:   filepath.Join(sourceRoot, "_build"),
				InstallRoot: outputDir,
			},
			ReplayReady: true,
		}
	}

	result, err := synthesizeOutputTrees(
		context.Background(),
		makeProbe(baseSource, baseDir, baseManifest, "0", "0", 1000),
		makeProbe(leftSource, leftDir, leftManifest, "1", "0", 2000),
		makeProbe(rightSource, rightDir, rightManifest, "0", "1", 3000),
	)
	if err != nil {
		t.Fatalf("synthesizeOutputTrees() error: %v", err)
	}
	if result.Mode != OutputSynthesisModeRootReplay || !result.Clean() {
		t.Fatalf("result = %#v, want clean root replay", result)
	}
	staticData, err := os.ReadFile(filepath.Join(result.Root, "share", "static.txt"))
	if err != nil {
		t.Fatalf("ReadFile(static.txt): %v", err)
	}
	if string(staticData) != "static=base\n" {
		t.Fatalf("static.txt = %q, want preserved base output", string(staticData))
	}
	configData, err := os.ReadFile(filepath.Join(result.Root, "share", "config.txt"))
	if err != nil {
		t.Fatalf("ReadFile(config.txt): %v", err)
	}
	if string(configData) != "config=proto:1,log:1\n" {
		t.Fatalf("config.txt = %q, want merged replay output", string(configData))
	}
	_ = os.RemoveAll(result.Root)
}

func TestSynthesizeOutputTrees_RootReplaySelectsDependentRoots(t *testing.T) {
	baseSource := t.TempDir()
	leftSource := t.TempDir()
	rightSource := t.TempDir()

	genScript := "#!/bin/sh\nset -eu\nproto=0\nlog=0\nout=\nfor arg in \"$@\"; do\n  case \"$arg\" in\n    --proto=*) proto=${arg#--proto=} ;;\n    --log=*) log=${arg#--log=} ;;\n    --out=*) out=${arg#--out=} ;;\n  esac\ndone\nmkdir -p \"$(dirname \"$out\")\"\nprintf 'proto=%s,log=%s\\n' \"$proto\" \"$log\" > \"$out\"\n"
	for _, root := range []string{baseSource, leftSource, rightSource} {
		if err := os.WriteFile(filepath.Join(root, "gen.sh"), []byte(genScript), 0o755); err != nil {
			t.Fatalf("WriteFile(gen script): %v", err)
		}
	}

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	baseBuild := filepath.Join(baseSource, "_build")
	leftBuild := filepath.Join(leftSource, "_build")
	rightBuild := filepath.Join(rightSource, "_build")
	if err := os.MkdirAll(baseBuild, 0o755); err != nil {
		t.Fatalf("MkdirAll(base build): %v", err)
	}
	if err := os.MkdirAll(leftBuild, 0o755); err != nil {
		t.Fatalf("MkdirAll(left build): %v", err)
	}
	if err := os.MkdirAll(rightBuild, 0o755); err != nil {
		t.Fatalf("MkdirAll(right build): %v", err)
	}
	writeMergeFile(t, filepath.Join(baseBuild, "generated.txt"), []byte("proto=0,log=0\n"), 0o644)
	writeMergeFile(t, filepath.Join(leftBuild, "generated.txt"), []byte("proto=1,log=0\n"), 0o644)
	writeMergeFile(t, filepath.Join(rightBuild, "generated.txt"), []byte("proto=0,log=1\n"), 0o644)
	writeMergeFile(t, filepath.Join(baseDir, "share", "config.txt"), []byte("proto=0,log=0\n"), 0o644)
	writeMergeFile(t, filepath.Join(leftDir, "share", "config.txt"), []byte("proto=1,log=0\n"), 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "share", "config.txt"), []byte("proto=0,log=1\n"), 0o644)

	baseManifest, _ := BuildOutputManifest(baseDir, "")
	leftManifest, _ := BuildOutputManifest(leftDir, "")
	rightManifest, _ := BuildOutputManifest(rightDir, "")

	makeProbe := func(sourceRoot, buildRoot, outputDir string, manifest OutputManifest, proto, log string, pidBase int64) ProbeResult {
		generatedPath := filepath.Join(buildRoot, "generated.txt")
		return ProbeResult{
			Records: []trace.Record{
				{
					PID:       pidBase,
					ParentPID: 0,
					Argv: []string{
						filepath.Join(sourceRoot, "gen.sh"),
						"--proto=" + proto,
						"--log=" + log,
						"--out=" + generatedPath,
					},
					Cwd:     sourceRoot,
					Env:     []string{"PATH=" + os.Getenv("PATH")},
					Changes: []string{generatedPath},
				},
				{
					PID:       pidBase + 1,
					ParentPID: 0,
					Argv: []string{
						"/bin/cp",
						generatedPath,
						filepath.Join(outputDir, "share", "config.txt"),
					},
					Cwd:    sourceRoot,
					Env:    []string{"PATH=" + os.Getenv("PATH")},
					Inputs: []string{generatedPath},
					Changes: []string{
						filepath.Join(outputDir, "share", "config.txt"),
					},
				},
			},
			OutputDir:      outputDir,
			OutputManifest: manifest,
			Scope: trace.Scope{
				SourceRoot:  sourceRoot,
				BuildRoot:   buildRoot,
				InstallRoot: outputDir,
			},
			ReplayReady: true,
		}
	}

	result, err := synthesizeOutputTrees(
		context.Background(),
		makeProbe(baseSource, baseBuild, baseDir, baseManifest, "0", "0", 4000),
		makeProbe(leftSource, leftBuild, leftDir, leftManifest, "1", "0", 5000),
		makeProbe(rightSource, rightBuild, rightDir, rightManifest, "0", "1", 6000),
	)
	if err != nil {
		t.Fatalf("synthesizeOutputTrees() error: %v", err)
	}
	if result.Mode != OutputSynthesisModeRootReplay || !result.Clean() {
		t.Fatalf("result = %#v, want clean dependent root replay", result)
	}
	got, err := os.ReadFile(filepath.Join(result.Root, "share", "config.txt"))
	if err != nil {
		t.Fatalf("ReadFile(config.txt): %v", err)
	}
	if string(got) != "proto=1,log=1\n" {
		t.Fatalf("config.txt = %q, want replayed downstream output", string(got))
	}
	_ = os.RemoveAll(result.Root)
}

func TestSynthesizeOutputTrees_RootReplayRejectsWideFrontier(t *testing.T) {
	baseSource := t.TempDir()
	leftSource := t.TempDir()
	rightSource := t.TempDir()

	genScript := "#!/bin/sh\nset -eu\nleft=0\nright=0\nout=\nfor arg in \"$@\"; do\n  case \"$arg\" in\n    --left=*) left=${arg#--left=} ;;\n    --right=*) right=${arg#--right=} ;;\n    --out=*) out=${arg#--out=} ;;\n  esac\ndone\nmkdir -p \"$(dirname \"$out\")\"\nprintf 'left=%s,right=%s\\n' \"$left\" \"$right\" > \"$out\"\n"
	for _, root := range []string{baseSource, leftSource, rightSource} {
		if err := os.WriteFile(filepath.Join(root, "gen.sh"), []byte(genScript), 0o755); err != nil {
			t.Fatalf("WriteFile(gen script): %v", err)
		}
	}

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	baseBuild := filepath.Join(baseSource, "_build")
	leftBuild := filepath.Join(leftSource, "_build")
	rightBuild := filepath.Join(rightSource, "_build")
	for _, dir := range []string{baseBuild, leftBuild, rightBuild} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(build root): %v", err)
		}
	}
	baseFiles := []string{
		filepath.Join(baseBuild, "s1.txt"),
		filepath.Join(baseBuild, "s2.txt"),
		filepath.Join(baseBuild, "s3.txt"),
		filepath.Join(baseBuild, "s4.txt"),
		filepath.Join(baseDir, "share", "final.txt"),
	}
	leftFiles := []string{
		filepath.Join(leftBuild, "s1.txt"),
		filepath.Join(leftBuild, "s2.txt"),
		filepath.Join(leftBuild, "s3.txt"),
		filepath.Join(leftBuild, "s4.txt"),
		filepath.Join(leftDir, "share", "final.txt"),
	}
	rightFiles := []string{
		filepath.Join(rightBuild, "s1.txt"),
		filepath.Join(rightBuild, "s2.txt"),
		filepath.Join(rightBuild, "s3.txt"),
		filepath.Join(rightBuild, "s4.txt"),
		filepath.Join(rightDir, "share", "final.txt"),
	}
	for _, path := range baseFiles {
		writeMergeFile(t, path, []byte("left=0,right=0\n"), 0o644)
	}
	for _, path := range leftFiles {
		writeMergeFile(t, path, []byte("left=1,right=0\n"), 0o644)
	}
	for _, path := range rightFiles {
		writeMergeFile(t, path, []byte("left=0,right=1\n"), 0o644)
	}

	baseManifest, _ := BuildOutputManifest(baseDir, "")
	leftManifest, _ := BuildOutputManifest(leftDir, "")
	rightManifest, _ := BuildOutputManifest(rightDir, "")

	makeProbe := func(sourceRoot, buildRoot, outputDir string, manifest OutputManifest, leftValue, rightValue string, pidBase int64) ProbeResult {
		s1 := filepath.Join(buildRoot, "s1.txt")
		s2 := filepath.Join(buildRoot, "s2.txt")
		s3 := filepath.Join(buildRoot, "s3.txt")
		s4 := filepath.Join(buildRoot, "s4.txt")
		final := filepath.Join(outputDir, "share", "final.txt")
		return ProbeResult{
			Records: []trace.Record{
				{
					PID:       pidBase,
					ParentPID: 0,
					Argv: []string{
						filepath.Join(sourceRoot, "gen.sh"),
						"--left=" + leftValue,
						"--right=" + rightValue,
						"--out=" + s1,
					},
					Cwd:     sourceRoot,
					Env:     []string{"PATH=" + os.Getenv("PATH")},
					Changes: []string{s1},
				},
				{
					PID:       pidBase + 1,
					ParentPID: 0,
					Argv: []string{
						"/bin/cp",
						s1,
						s2,
					},
					Cwd:    sourceRoot,
					Env:    []string{"PATH=" + os.Getenv("PATH")},
					Inputs: []string{s1},
					Changes: []string{
						s2,
					},
				},
				{
					PID:       pidBase + 2,
					ParentPID: 0,
					Argv: []string{
						"/bin/cp",
						s2,
						s3,
					},
					Cwd:    sourceRoot,
					Env:    []string{"PATH=" + os.Getenv("PATH")},
					Inputs: []string{s2},
					Changes: []string{
						s3,
					},
				},
				{
					PID:       pidBase + 3,
					ParentPID: 0,
					Argv: []string{
						"/bin/cp",
						s3,
						s4,
					},
					Cwd:    sourceRoot,
					Env:    []string{"PATH=" + os.Getenv("PATH")},
					Inputs: []string{s3},
					Changes: []string{
						s4,
					},
				},
				{
					PID:       pidBase + 4,
					ParentPID: 0,
					Argv: []string{
						"/bin/cp",
						s4,
						final,
					},
					Cwd:    sourceRoot,
					Env:    []string{"PATH=" + os.Getenv("PATH")},
					Inputs: []string{s4},
					Changes: []string{
						final,
					},
				},
			},
			OutputDir:      outputDir,
			OutputManifest: manifest,
			Scope: trace.Scope{
				SourceRoot:  sourceRoot,
				BuildRoot:   buildRoot,
				InstallRoot: outputDir,
			},
			ReplayReady: true,
		}
	}

	result, err := synthesizeOutputTrees(
		context.Background(),
		makeProbe(baseSource, baseBuild, baseDir, baseManifest, "0", "0", 10000),
		makeProbe(leftSource, leftBuild, leftDir, leftManifest, "1", "0", 11000),
		makeProbe(rightSource, rightBuild, rightDir, rightManifest, "0", "1", 12000),
	)
	if err != nil {
		t.Fatalf("synthesizeOutputTrees() error: %v", err)
	}
	if result.Clean() {
		t.Fatalf("result = %#v, want needs-rebuild because replay frontier is too wide", result)
	}
	if result.Mode != OutputSynthesisModeRootReplay {
		t.Fatalf("mode = %q, want %q", result.Mode, OutputSynthesisModeRootReplay)
	}
	if result.Replay == nil {
		t.Fatal("replay summary = nil, want replay summary")
	}
	if !strings.Contains(result.Replay.Unavailable, "replay frontier too wide") {
		t.Fatalf("unavailable = %q, want replay frontier too wide", result.Replay.Unavailable)
	}
	if len(result.Replay.SelectedRoots) != 5 {
		t.Fatalf("selected roots = %d, want 5", len(result.Replay.SelectedRoots))
	}
	if result.Replay.SelectedWrites != 1 {
		t.Fatalf("selected writes = %d, want 1 materialized write", result.Replay.SelectedWrites)
	}
}

func TestSynthesizeOutputTrees_RootReplayRejectsShellCommand(t *testing.T) {
	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	writeMergeFile(t, filepath.Join(baseDir, "share", "config.txt"), []byte("state=proto:0,log:0\n"), 0o644)
	writeMergeFile(t, filepath.Join(leftDir, "share", "config.txt"), []byte("state=proto:1,log:0\n"), 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "share", "config.txt"), []byte("state=proto:0,log:1\n"), 0o644)

	baseManifest, _ := BuildOutputManifest(baseDir, "")
	leftManifest, _ := BuildOutputManifest(leftDir, "")
	rightManifest, _ := BuildOutputManifest(rightDir, "")
	makeProbe := func(outputDir string, manifest OutputManifest, script string) ProbeResult {
		return ProbeResult{
			Records: []trace.Record{{
				PID:       1,
				ParentPID: 0,
				Argv:      []string{"sh", "-c", script},
				Cwd:       outputDir,
				Env:       []string{"PATH=" + os.Getenv("PATH")},
			}},
			OutputDir:      outputDir,
			OutputManifest: manifest,
			Scope: trace.Scope{
				SourceRoot:  outputDir,
				BuildRoot:   filepath.Join(outputDir, "_build"),
				InstallRoot: outputDir,
			},
			ReplayReady: true,
		}
	}

	result, err := synthesizeOutputTrees(
		context.Background(),
		makeProbe(baseDir, baseManifest, "echo base"),
		makeProbe(leftDir, leftManifest, "echo left"),
		makeProbe(rightDir, rightManifest, "echo right"),
	)
	if err != nil {
		t.Fatalf("synthesizeOutputTrees() error: %v", err)
	}
	if result.Clean() {
		t.Fatalf("result = clean, want needs-rebuild")
	}
	if result.Mode != OutputSynthesisModeRootReplay {
		t.Fatalf("mode = %q, want %q", result.Mode, OutputSynthesisModeRootReplay)
	}
	if len(result.Issues) == 0 {
		t.Fatal("issues = 0, want replay unavailable issue")
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Kind == OutputMergeIssueKindRootReplayUnavailable {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("issues = %#v, want root replay unavailable", result.Issues)
	}
}

func TestSynthesizeOutputTrees_RootReplayCountsMaterializedWritesForFanout(t *testing.T) {
	baseSource := t.TempDir()
	leftSource := t.TempDir()
	rightSource := t.TempDir()
	for _, root := range []string{baseSource, leftSource, rightSource} {
		scriptPath := filepath.Join(root, "emit.sh")
		if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nset -eu\nleft=0\nright=0\nout=\nfor arg in \"$@\"; do\n  case \"$arg\" in\n    --left=*) left=${arg#--left=} ;;\n    --right=*) right=${arg#--right=} ;;\n    --out=*) out=${arg#--out=} ;;\n  esac\ndone\nmkdir -p \"$(dirname \"$out\")\"\nprintf 'left=%s,right=%s\\n' \"$left\" \"$right\" > \"$out\"\n"), 0o755); err != nil {
			t.Fatalf("WriteFile(script): %v", err)
		}
	}
	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	writeMergeFile(t, filepath.Join(baseDir, "share", "out.txt"), []byte("left=0,right=0\n"), 0o644)
	writeMergeFile(t, filepath.Join(leftDir, "share", "out.txt"), []byte("left=1,right=0\n"), 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "share", "out.txt"), []byte("left=0,right=1\n"), 0o644)
	baseManifest, _ := BuildOutputManifest(baseDir, "")
	leftManifest, _ := BuildOutputManifest(leftDir, "")
	rightManifest, _ := BuildOutputManifest(rightDir, "")

	makeProbe := func(sourceRoot, outputDir string, manifest OutputManifest, leftValue, rightValue string, pidBase int64) ProbeResult {
		buildRoot := filepath.Join(sourceRoot, "_build")
		scriptPath := filepath.Join(sourceRoot, "emit.sh")
		var records []trace.Record
		records = append(records, trace.Record{
			PID:       pidBase,
			ParentPID: 0,
			Argv: []string{
				"cmake",
				"-S", filepath.Join(sourceRoot, "src"),
				"-B", buildRoot,
				"-DLEFT=" + leftValue,
				"-DRIGHT=" + rightValue,
			},
			Cwd: sourceRoot,
			Env: []string{"PATH=" + os.Getenv("PATH")},
			Changes: func() []string {
				out := []string{filepath.Join(buildRoot, "config.h")}
				for i := 0; i < 200; i++ {
					out = append(out, filepath.Join(buildRoot, fmt.Sprintf("scratch-%03d.tmp", i)))
				}
				return out
			}(),
		})
		records = append(records, trace.Record{
			PID:       pidBase + 1,
			ParentPID: 0,
			Argv:      []string{"cmake", "--build", buildRoot, "--config", "Release"},
			Cwd:       sourceRoot,
			Env:       []string{"PATH=" + os.Getenv("PATH")},
			Inputs:    []string{filepath.Join(buildRoot, "config.h")},
			Changes: func() []string {
				out := []string{filepath.Join(buildRoot, "obj.o")}
				for i := 0; i < 200; i++ {
					out = append(out, filepath.Join(buildRoot, fmt.Sprintf("obj-%03d.o", i)))
				}
				return out
			}(),
		})
		records = append(records, trace.Record{
			PID:       pidBase + 2,
			ParentPID: 0,
			Argv: []string{
				scriptPath,
				"--left=" + leftValue,
				"--right=" + rightValue,
				"--out=" + filepath.Join(outputDir, "share", "out.txt"),
			},
			Cwd:     sourceRoot,
			Env:     []string{"PATH=" + os.Getenv("PATH")},
			Inputs:  []string{filepath.Join(buildRoot, "obj.o")},
			Changes: []string{filepath.Join(outputDir, "share", "out.txt")},
		})
		return ProbeResult{
			Records:        records,
			OutputDir:      outputDir,
			OutputManifest: manifest,
			Scope: trace.Scope{
				SourceRoot:  sourceRoot,
				BuildRoot:   buildRoot,
				InstallRoot: outputDir,
			},
			ReplayReady: true,
		}
	}

	result, err := synthesizeOutputTrees(
		context.Background(),
		makeProbe(baseSource, baseDir, baseManifest, "0", "0", 100),
		makeProbe(leftSource, leftDir, leftManifest, "1", "0", 200),
		makeProbe(rightSource, rightDir, rightManifest, "0", "1", 300),
	)
	if err != nil {
		t.Fatalf("synthesizeOutputTrees() error: %v", err)
	}
	if result.Mode != OutputSynthesisModeRootReplay {
		t.Fatalf("mode = %q, want %q", result.Mode, OutputSynthesisModeRootReplay)
	}
	if result.Replay == nil {
		t.Fatal("Replay = nil, want replay summary")
	}
	if result.Replay.SelectedWrites != 1 {
		t.Fatalf("selected writes = %d, want 1 materialized write", result.Replay.SelectedWrites)
	}
}

func TestSynthesizeOutputTrees_RootReplayIgnoresIrrelevantNoiseRoots(t *testing.T) {
	baseSource := t.TempDir()
	leftSource := t.TempDir()
	rightSource := t.TempDir()
	script := "#!/bin/sh\nset -eu\nproto=0\nlog=0\nout=\nfor arg in \"$@\"; do\n  case \"$arg\" in\n    --proto=*) proto=${arg#--proto=} ;;\n    --log=*) log=${arg#--log=} ;;\n    --out=*) out=${arg#--out=} ;;\n  esac\ndone\nmkdir -p \"$(dirname \"$out\")\"\nprintf 'state=proto:%s,log:%s\\n' \"$proto\" \"$log\" > \"$out\"\n"
	for _, root := range []string{baseSource, leftSource, rightSource} {
		if err := os.WriteFile(filepath.Join(root, "emit-config.sh"), []byte(script), 0o755); err != nil {
			t.Fatalf("WriteFile(script): %v", err)
		}
	}

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	writeMergeFile(t, filepath.Join(baseDir, "share", "config.txt"), []byte("state=proto:0,log:0\n"), 0o644)
	writeMergeFile(t, filepath.Join(leftDir, "share", "config.txt"), []byte("state=proto:1,log:0\n"), 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "share", "config.txt"), []byte("state=proto:0,log:1\n"), 0o644)

	baseManifest, _ := BuildOutputManifest(baseDir, "")
	leftManifest, _ := BuildOutputManifest(leftDir, "")
	rightManifest, _ := BuildOutputManifest(rightDir, "")

	makeProbe := func(sourceRoot, outputDir string, manifest OutputManifest, proto, log string, pidBase int64) ProbeResult {
		return ProbeResult{
			Records: []trace.Record{
				{
					PID:       pidBase,
					ParentPID: 0,
					Argv:      []string{"uname", "-m"},
					Cwd:       sourceRoot,
					Env:       []string{"PATH=" + os.Getenv("PATH")},
				},
				{
					PID:       pidBase + 1,
					ParentPID: 0,
					Argv: []string{
						filepath.Join(sourceRoot, "emit-config.sh"),
						"--proto=" + proto,
						"--log=" + log,
						"--out=" + filepath.Join(outputDir, "share", "config.txt"),
					},
					Cwd: sourceRoot,
					Env: []string{"PATH=" + os.Getenv("PATH")},
					Changes: []string{
						filepath.Join(outputDir, "share", "config.txt"),
					},
				},
			},
			OutputDir:      outputDir,
			OutputManifest: manifest,
			Scope: trace.Scope{
				SourceRoot:  sourceRoot,
				BuildRoot:   filepath.Join(sourceRoot, "_build"),
				InstallRoot: outputDir,
			},
			ReplayReady: true,
		}
	}

	result, err := synthesizeOutputTrees(
		context.Background(),
		makeProbe(baseSource, baseDir, baseManifest, "0", "0", 7000),
		makeProbe(leftSource, leftDir, leftManifest, "1", "0", 8000),
		makeProbe(rightSource, rightDir, rightManifest, "0", "1", 9000),
	)
	if err != nil {
		t.Fatalf("synthesizeOutputTrees() error: %v", err)
	}
	if result.Mode != OutputSynthesisModeRootReplay || !result.Clean() {
		t.Fatalf("result = %#v, want clean replay", result)
	}
	if result.Replay == nil {
		t.Fatal("Replay = nil, want replay summary")
	}
	if result.Replay.CandidateRoots != 2 {
		t.Fatalf("CandidateRoots = %d, want 2", result.Replay.CandidateRoots)
	}
	if result.Replay.EligibleRoots != 1 {
		t.Fatalf("EligibleRoots = %d, want 1", result.Replay.EligibleRoots)
	}
}

func TestSynthesizeOutputTrees_RootReplayRejectsAmbiguousIdentity(t *testing.T) {
	baseSource := t.TempDir()
	leftSource := t.TempDir()
	rightSource := t.TempDir()
	for _, root := range []string{baseSource, leftSource, rightSource} {
		scriptPath := filepath.Join(root, "emit-config.sh")
		if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("WriteFile(script): %v", err)
		}
	}
	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	writeMergeFile(t, filepath.Join(baseDir, "share", "config.txt"), []byte("state=proto:0,log:0\n"), 0o644)
	writeMergeFile(t, filepath.Join(leftDir, "share", "config.txt"), []byte("state=proto:1,log:0\n"), 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "share", "config.txt"), []byte("state=proto:0,log:1\n"), 0o644)
	baseManifest, _ := BuildOutputManifest(baseDir, "")
	leftManifest, _ := BuildOutputManifest(leftDir, "")
	rightManifest, _ := BuildOutputManifest(rightDir, "")

	makeProbe := func(sourceRoot, outputDir string, manifest OutputManifest, pidBase int64, proto string) ProbeResult {
		scriptPath := filepath.Join(sourceRoot, "emit-config.sh")
		return ProbeResult{
			Records: []trace.Record{
				{
					PID:       pidBase,
					ParentPID: 0,
					Argv:      []string{scriptPath, "--proto=0", "--out=" + filepath.Join(outputDir, "share", "config.txt")},
					Cwd:       sourceRoot,
					Env:       []string{"PATH=" + os.Getenv("PATH")},
					Changes:   []string{filepath.Join(outputDir, "share", "config.txt")},
				},
				{
					PID:       pidBase + 1,
					ParentPID: 0,
					Argv:      []string{scriptPath, "--proto=" + proto, "--out=" + filepath.Join(outputDir, "share", "config.txt")},
					Cwd:       sourceRoot,
					Env:       []string{"PATH=" + os.Getenv("PATH")},
					Changes:   []string{filepath.Join(outputDir, "share", "config.txt")},
				},
			},
			OutputDir:      outputDir,
			OutputManifest: manifest,
			Scope: trace.Scope{
				SourceRoot:  sourceRoot,
				BuildRoot:   filepath.Join(sourceRoot, "_build"),
				InstallRoot: outputDir,
			},
			ReplayReady: true,
		}
	}

	result, err := synthesizeOutputTrees(
		context.Background(),
		makeProbe(baseSource, baseDir, baseManifest, 100, "0"),
		makeProbe(leftSource, leftDir, leftManifest, 200, "1"),
		makeProbe(rightSource, rightDir, rightManifest, 300, "1"),
	)
	if err != nil {
		t.Fatalf("synthesizeOutputTrees() error: %v", err)
	}
	if result.Clean() {
		t.Fatalf("result = %#v, want needs-rebuild", result)
	}
	if result.Replay == nil || result.Replay.Unavailable == "" {
		t.Fatalf("Replay summary = %#v, want unavailable detail", result.Replay)
	}
	if !strings.Contains(result.Replay.Unavailable, "ambiguous") {
		t.Fatalf("Replay.Unavailable = %q, want ambiguous identity detail", result.Replay.Unavailable)
	}
}
