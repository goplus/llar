package evaluator

import (
	"context"
	"os"
	"path/filepath"
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
