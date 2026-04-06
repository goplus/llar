package evaluator

import (
	"context"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/trace"
)

func TestWatchIndependentOptions(t *testing.T) {
	matrix := testMatrix()
	traces := map[string][]trace.Record{
		"amd64-linux|doc-off-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-on-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
			record([]string{"sphinx-build", "docs", "out/share/doc"}, "/tmp/work", []string{"/tmp/work/docs/index.md"}, []string{"/tmp/work/out/share/doc/index.html"}),
		},
		"amd64-linux|doc-off-tls-on": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
			record([]string{"python", "gen_tls.py"}, "/tmp/work", []string{"/tmp/work/gen_tls.py"}, []string{"/tmp/work/out/bin/tls-helper"}),
		},
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return ProbeResult{Records: traces[combo]}, nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}
	if !trusted {
		t.Fatalf("Watch() trusted = false, want true")
	}

	want := []string{
		"amd64-linux|doc-off-tls-off",
		"amd64-linux|doc-off-tls-on",
		"amd64-linux|doc-on-tls-off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchStopsReducingZeroDiffOption(t *testing.T) {
	matrix := testMatrix()
	traces := map[string][]trace.Record{
		"amd64-linux|doc-off-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-on-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-off-tls-on": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
			record([]string{"python", "gen_tls.py"}, "/tmp/work", []string{"/tmp/work/gen_tls.py"}, []string{"/tmp/work/out/bin/tls-helper"}),
		},
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return ProbeResult{Records: traces[combo]}, nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}
	if !trusted {
		t.Fatalf("Watch() trusted = false, want true")
	}

	want := []string{
		"amd64-linux|doc-off-tls-off",
		"amd64-linux|doc-off-tls-on",
		"amd64-linux|doc-on-tls-off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchZeroDiffDoesNotBridgeIndependentOptions(t *testing.T) {
	matrix := formula.Matrix{
		Require: map[string][]string{
			"arch": {"amd64"},
			"os":   {"linux"},
		},
		Options: map[string][]string{
			"doc":  {"doc-off", "doc-on"},
			"simd": {"simd-off", "simd-on"},
			"tls":  {"tls-off", "tls-on"},
		},
		DefaultOptions: map[string][]string{
			"doc":  {"doc-off"},
			"simd": {"simd-off"},
			"tls":  {"tls-off"},
		},
	}
	traces := map[string][]trace.Record{
		"amd64-linux|doc-off-simd-off-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-on-simd-off-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-off-simd-on-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
			record([]string{"python", "gen_simd.py"}, "/tmp/work", []string{"/tmp/work/gen_simd.py"}, []string{"/tmp/work/out/bin/simd-helper"}),
		},
		"amd64-linux|doc-off-simd-off-tls-on": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
			record([]string{"python", "gen_tls.py"}, "/tmp/work", []string{"/tmp/work/gen_tls.py"}, []string{"/tmp/work/out/bin/tls-helper"}),
		},
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return ProbeResult{Records: traces[combo]}, nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}
	if !trusted {
		t.Fatalf("Watch() trusted = false, want true")
	}

	want := []string{
		"amd64-linux|doc-off-simd-off-tls-off",
		"amd64-linux|doc-off-simd-off-tls-on",
		"amd64-linux|doc-off-simd-on-tls-off",
		"amd64-linux|doc-on-simd-off-tls-off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchTreatsDownstreamLinkDependencyAsCollision(t *testing.T) {
	matrix := formula.Matrix{
		Require: map[string][]string{
			"arch": {"amd64"},
			"os":   {"linux"},
		},
		Options: map[string][]string{
			"api": {"api-off", "api-on"},
			"cli": {"cli-off", "cli-on"},
		},
		DefaultOptions: map[string][]string{
			"api": {"api-off"},
			"cli": {"cli-off"},
		},
	}
	traces := map[string][]trace.Record{
		"amd64-linux|api-off-cli-off": {
			record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "out/lib/libtracecore.a", "build/core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libtracecore.a"}),
		},
		"amd64-linux|api-on-cli-off": {
			record([]string{"cc", "-DAPI", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "out/lib/libtracecore.a", "build/core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libtracecore.a"}),
		},
		"amd64-linux|api-off-cli-on": {
			record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "out/lib/libtracecore.a", "build/core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libtracecore.a"}),
			record([]string{"cc", "-c", "cli.c", "-o", "build/cli.o"}, "/tmp/work", []string{"/tmp/work/cli.c"}, []string{"/tmp/work/build/cli.o"}),
			record([]string{"cc", "build/cli.o", "out/lib/libtracecore.a", "-o", "out/bin/tracecli"}, "/tmp/work", []string{"/tmp/work/build/cli.o", "/tmp/work/out/lib/libtracecore.a"}, []string{"/tmp/work/out/bin/tracecli"}),
		},
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return ProbeResult{Records: traces[combo]}, nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}
	if !trusted {
		t.Fatalf("Watch() trusted = false, want true")
	}

	want := []string{
		"amd64-linux|api-off-cli-off",
		"amd64-linux|api-off-cli-on",
		"amd64-linux|api-on-cli-off",
		"amd64-linux|api-on-cli-on",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchMixedZeroDiffKeyStillParticipatesInCollisions(t *testing.T) {
	matrix := formula.Matrix{
		Options: map[string][]string{
			"level": {"level-off", "level-medium", "level-strong"},
			"net":   {"net-off", "net-on"},
			"simd":  {"simd-off", "simd-on"},
		},
		DefaultOptions: map[string][]string{
			"level": {"level-off"},
			"net":   {"net-off"},
			"simd":  {"simd-off"},
		},
	}
	traces := map[string][]trace.Record{
		"level-off-net-off-simd-off": {
			record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"cc", "-c", "extra.c", "-o", "build/extra.o"}, "/tmp/work", []string{"/tmp/work/extra.c"}, []string{"/tmp/work/build/extra.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o", "build/extra.o"}, "/tmp/work", []string{"/tmp/work/build/core.o", "/tmp/work/build/extra.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"level-medium-net-off-simd-off": {
			record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"cc", "-c", "extra.c", "-o", "build/extra.o"}, "/tmp/work", []string{"/tmp/work/extra.c"}, []string{"/tmp/work/build/extra.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o", "build/extra.o"}, "/tmp/work", []string{"/tmp/work/build/core.o", "/tmp/work/build/extra.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"level-strong-net-off-simd-off": {
			record([]string{"cc", "-DLEVEL_STRONG", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"cc", "-DLEVEL_STRONG", "-c", "extra.c", "-o", "build/extra.o"}, "/tmp/work", []string{"/tmp/work/extra.c"}, []string{"/tmp/work/build/extra.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o", "build/extra.o"}, "/tmp/work", []string{"/tmp/work/build/core.o", "/tmp/work/build/extra.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"level-off-net-on-simd-off": {
			record([]string{"cc", "-DNET", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"cc", "-c", "extra.c", "-o", "build/extra.o"}, "/tmp/work", []string{"/tmp/work/extra.c"}, []string{"/tmp/work/build/extra.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o", "build/extra.o"}, "/tmp/work", []string{"/tmp/work/build/core.o", "/tmp/work/build/extra.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"level-off-net-off-simd-on": {
			record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"cc", "-DSIMD", "-c", "extra.c", "-o", "build/extra.o"}, "/tmp/work", []string{"/tmp/work/extra.c"}, []string{"/tmp/work/build/extra.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o", "build/extra.o"}, "/tmp/work", []string{"/tmp/work/build/core.o", "/tmp/work/build/extra.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return ProbeResult{Records: traces[combo]}, nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}
	if !trusted {
		t.Fatalf("Watch() trusted = false, want true")
	}

	var want []string
	for _, level := range matrix.Options["level"] {
		for _, net := range matrix.Options["net"] {
			for _, simd := range matrix.Options["simd"] {
				want = append(want, strings.Join([]string{level, net, simd}, "-"))
			}
		}
	}
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchTrustsMainlineGenericActionWhenPathsAreKnown(t *testing.T) {
	matrix := formula.Matrix{
		Options: map[string][]string{
			"feat": {"feat-off", "feat-on"},
		},
		DefaultOptions: map[string][]string{
			"feat": {"feat-off"},
		},
	}
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	traces := map[string]ProbeResult{
		"feat-off": {
			Records: []trace.Record{
				record([]string{"genblob", "build"}, "/tmp/work", []string{"/tmp/work/schema.dsl"}, []string{"/tmp/work/_build/schema.bin"}),
				record([]string{"cp", "/tmp/work/_build/schema.bin", "/tmp/work/install/share/schema.bin"}, "/tmp/work", []string{"/tmp/work/_build/schema.bin"}, []string{"/tmp/work/install/share/schema.bin"}),
			},
			Scope: scope,
		},
		"feat-on": {
			Records: []trace.Record{
				record([]string{"genblob", "build", "--feature"}, "/tmp/work", []string{"/tmp/work/schema.dsl"}, []string{"/tmp/work/_build/schema.bin"}),
				record([]string{"cp", "/tmp/work/_build/schema.bin", "/tmp/work/install/share/schema.bin"}, "/tmp/work", []string{"/tmp/work/_build/schema.bin"}, []string{"/tmp/work/install/share/schema.bin"}),
			},
			Scope: scope,
		},
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return traces[combo], nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"feat-off", "feat-on"}) {
		t.Fatalf("Watch() = %v, want %v", got, []string{"feat-off", "feat-on"})
	}
	if !trusted {
		t.Fatalf("Watch() trusted = false, want true")
	}
}

func TestWatchPrefersEventBackedProbeGraph(t *testing.T) {
	matrix := formula.Matrix{
		Options: map[string][]string{
			"feat": {"feat-off", "feat-on"},
		},
		DefaultOptions: map[string][]string{
			"feat": {"feat-off"},
		},
	}

	probes := map[string]ProbeResult{
		"feat-off": {
			Records: []trace.Record{
				record([]string{"python", "tool.py"}, "/tmp/work", []string{"/tmp/work/shared.in"}, []string{"/tmp/work/out/shared.txt"}),
			},
			Events: []trace.Event{
				{Seq: 1, PID: 100, Cwd: "/tmp/work", Kind: trace.EventExec, Argv: []string{"python", "tool.py"}},
				{Seq: 2, PID: 100, Cwd: "/tmp/work", Kind: trace.EventRead, Path: "/tmp/work/shared.in"},
				{Seq: 3, PID: 100, Cwd: "/tmp/work", Kind: trace.EventWrite, Path: "/tmp/work/out/shared.txt"},
			},
		},
		"feat-on": {
			Records: []trace.Record{
				record([]string{"python", "tool.py"}, "/tmp/work", []string{"/tmp/work/shared.in"}, []string{"/tmp/work/out/shared.txt"}),
			},
			Events: []trace.Event{
				{Seq: 1, PID: 100, Cwd: "/tmp/work", Kind: trace.EventExec, Argv: []string{"python", "tool.py", "--feature"}},
				{Seq: 2, PID: 100, Cwd: "/tmp/work", Kind: trace.EventRead, Path: "/tmp/work/shared.in"},
				{Seq: 3, PID: 100, Cwd: "/tmp/work", Kind: trace.EventWrite, Path: "/tmp/work/out/shared.txt"},
			},
		},
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return probes[combo], nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}
	if !trusted {
		t.Fatalf("Watch() trusted = false, want true")
	}
	want := []string{"feat-off", "feat-on"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchEventBackedArchiveTempNoiseDoesNotCreateCollision(t *testing.T) {
	matrix := formula.Matrix{
		Options: map[string][]string{
			"api":  {"api-off", "api-on"},
			"ship": {"ship-off", "ship-on"},
		},
		DefaultOptions: map[string][]string{
			"api":  {"api-off"},
			"ship": {"ship-off"},
		},
	}
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/build",
		InstallRoot: "/tmp/work/install",
	}

	makeEvents := func(api, ship bool, arTmp, ranlibTmp string) []trace.Event {
		ccArgv := []string{"cc", "-c", "core.c", "-o", "CMakeFiles/tracecore.dir/core.c.o"}
		if api {
			ccArgv = []string{"cc", "-DTRACE_FEATURE_API", "-c", "core.c", "-o", "CMakeFiles/tracecore.dir/core.c.o"}
		}
		events := []trace.Event{
			{Seq: 1, PID: 100, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: ccArgv},
			{Seq: 2, PID: 100, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/core.c"},
			{Seq: 3, PID: 100, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"},
			{Seq: 4, PID: 101, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/bin/ar", "qc", "libtracecore.a", "CMakeFiles/tracecore.dir/core.c.o"}},
			{Seq: 5, PID: 101, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 6, PID: 101, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"},
			{Seq: 7, PID: 101, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 8, PID: 101, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/" + arTmp},
			{Seq: 9, PID: 102, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/bin/ranlib", "libtracecore.a"}},
			{Seq: 10, PID: 102, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 11, PID: 102, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/" + ranlibTmp},
			{Seq: 12, PID: 102, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 13, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"cmake", "--install", "/tmp/work/build", "--prefix", "/tmp/work/install"}},
			{Seq: 14, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/cmake_install.cmake"},
			{Seq: 15, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 16, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/trace.h"},
			{Seq: 17, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/install/lib/libtracecore.a"},
			{Seq: 18, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/install/include/trace.h"},
		}
		if ship {
			events = append(events, trace.Event{
				Seq:  19,
				PID:  103,
				Cwd:  "/tmp/work/build",
				Kind: trace.EventWrite,
				Path: "/tmp/work/install/include/trace_alias.h",
			})
		}
		return events
	}

	probes := map[string]ProbeResult{
		"api-off-ship-off": {
			Events: makeEvents(false, false, "stBaseAr", "stBaseRanlib"),
			Scope:  scope,
			InputDigests: map[string]string{
				"/tmp/work/build/libtracecore.a":                    "archive-base",
				"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o": "core-base",
			},
		},
		"api-on-ship-off": {
			Events: makeEvents(true, false, "stApiAr", "stApiRanlib"),
			Scope:  scope,
			InputDigests: map[string]string{
				"/tmp/work/build/libtracecore.a":                    "archive-api",
				"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o": "core-api",
			},
		},
		"api-off-ship-on": {
			Events: makeEvents(false, true, "stShipAr", "stShipRanlib"),
			Scope:  scope,
			InputDigests: map[string]string{
				"/tmp/work/build/libtracecore.a":                    "archive-base",
				"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o": "core-base",
			},
		},
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return probes[combo], nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}
	if !trusted {
		t.Fatalf("Watch() trusted = false, want true")
	}

	want := []string{
		"api-off-ship-off",
		"api-off-ship-on",
		"api-on-ship-off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchEventBackedCompilerChildEventsCreateCollision(t *testing.T) {
	matrix := formula.Matrix{
		Options: map[string][]string{
			"api": {"api-off", "api-on"},
			"cli": {"cli-off", "cli-on"},
		},
		DefaultOptions: map[string][]string{
			"api": {"api-off"},
			"cli": {"cli-off"},
		},
	}
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/build",
		InstallRoot: "/tmp/work/install",
	}

	makeCompileEvents := func(api bool) []trace.Event {
		driver := []string{"/usr/bin/cc", "-O3", "-DNDEBUG", "-o", "CMakeFiles/tracecore.dir/core.c.o", "-c", "/tmp/work/core.c"}
		if api {
			driver = []string{"/usr/bin/cc", "-DTRACE_FEATURE_API", "-O3", "-DNDEBUG", "-o", "CMakeFiles/tracecore.dir/core.c.o", "-c", "/tmp/work/core.c"}
		}
		return []trace.Event{
			{Seq: 1, PID: 100, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: driver},
			{Seq: 2, PID: 100, Cwd: "/tmp/work/build", Kind: trace.EventClone, ChildPID: 101},
			{Seq: 3, PID: 101, ParentPID: 100, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/lib/gcc/cc1", "/tmp/work/core.c"}},
			{Seq: 4, PID: 101, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/core.c"},
			{Seq: 5, PID: 101, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/trace_options.h"},
			{Seq: 6, PID: 100, Cwd: "/tmp/work/build", Kind: trace.EventClone, ChildPID: 102},
			{Seq: 7, PID: 102, ParentPID: 100, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"as", "-o", "CMakeFiles/tracecore.dir/core.c.o"}},
			{Seq: 8, PID: 102, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"},
			{Seq: 9, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/bin/ar", "qc", "libtracecore.a", "CMakeFiles/tracecore.dir/core.c.o"}},
			{Seq: 10, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 11, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"},
			{Seq: 12, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 13, PID: 104, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/bin/ranlib", "libtracecore.a"}},
			{Seq: 14, PID: 104, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 15, PID: 104, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 16, PID: 105, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"cmake", "--install", "/tmp/work/build", "--prefix", "/tmp/work/install"}},
			{Seq: 17, PID: 105, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 18, PID: 105, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/install/lib/libtracecore.a"},
		}
	}

	makeCliEvents := func() []trace.Event {
		return []trace.Event{
			{Seq: 1, PID: 200, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/bin/cc", "cli.c", "-o", "tracecli", "libtracecore.a"}},
			{Seq: 2, PID: 201, ParentPID: 200, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/bin/ld", "-o", "tracecli"}},
			{Seq: 3, PID: 201, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 4, PID: 201, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/tracecli"},
			{Seq: 5, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"cmake", "--install", "/tmp/work/build", "--prefix", "/tmp/work/install"}},
			{Seq: 6, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 7, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/tracecli"},
			{Seq: 8, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/install/lib/libtracecore.a"},
			{Seq: 9, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/install/bin/tracecli"},
		}
	}

	probes := map[string]ProbeResult{
		"api-off-cli-off": {
			Events: makeCompileEvents(false),
			Scope:  scope,
			InputDigests: map[string]string{
				"/tmp/work/build/trace_options.h":                   "trace-off",
				"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o": "core-off",
				"/tmp/work/build/libtracecore.a":                    "archive-off",
			},
		},
		"api-on-cli-off": {
			Events: makeCompileEvents(true),
			Scope:  scope,
			InputDigests: map[string]string{
				"/tmp/work/build/trace_options.h":                   "trace-api",
				"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o": "core-api",
				"/tmp/work/build/libtracecore.a":                    "archive-api",
			},
		},
		"api-off-cli-on": {
			Events: append(makeCompileEvents(false), makeCliEvents()...),
			Scope:  scope,
			InputDigests: map[string]string{
				"/tmp/work/build/trace_options.h":                   "trace-off",
				"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o": "core-off",
				"/tmp/work/build/libtracecore.a":                    "archive-off",
				"/tmp/work/build/tracecli":                          "cli-bin",
			},
		},
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return probes[combo], nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}
	if !trusted {
		t.Fatalf("Watch() trusted = false, want true")
	}
	want := []string{
		"api-off-cli-off",
		"api-off-cli-on",
		"api-on-cli-off",
		"api-on-cli-on",
	}
	if !reflect.DeepEqual(got, want) {
		baseGraph := buildGraphForProbe(probes["api-off-cli-off"])
		apiGraph := buildGraphForProbe(probes["api-on-cli-off"])
		cliGraph := buildGraphForProbe(probes["api-off-cli-on"])
		t.Logf("base graph:\\n%s", formatGraphSummary(baseGraph, DebugSummaryOptions{InterestingTokens: []string{"/trace_options.h", "/core.c.o", "/libtracecore.a"}, InterestingLimit: 10, RoleSampleLimit: 10}))
		t.Logf("api graph:\\n%s", formatGraphSummary(apiGraph, DebugSummaryOptions{InterestingTokens: []string{"/trace_options.h", "/core.c.o", "/libtracecore.a"}, InterestingLimit: 10, RoleSampleLimit: 10}))
		t.Logf("cli graph:\\n%s", formatGraphSummary(cliGraph, DebugSummaryOptions{InterestingTokens: []string{"/trace_options.h", "/core.c.o", "/libtracecore.a", "/tracecli"}, InterestingLimit: 10, RoleSampleLimit: 10}))
		t.Logf("api diff:\\n%s", DebugDiffSummary(probes["api-off-cli-off"], probes["api-on-cli-off"], DebugDiffSummaryOptions{BaseLabel: "base", ProbeLabel: "api"}))
		t.Logf("collision:\\n%s", DebugCollisionSummary(probes["api-off-cli-off"], probes["api-on-cli-off"], probes["api-off-cli-on"], DebugCollisionSummaryOptions{BaseLabel: "base", LeftLabel: "api", RightLabel: "cli"}))
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchEventBackedBuildRootDigestOnlyCreatesCollision(t *testing.T) {
	matrix := formula.Matrix{
		Options: map[string][]string{
			"api": {"api-off", "api-on"},
			"cli": {"cli-off", "cli-on"},
		},
		DefaultOptions: map[string][]string{
			"api": {"api-off"},
			"cli": {"cli-off"},
		},
	}
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/build",
		InstallRoot: "/tmp/work/install",
	}

	makeCompileEvents := func() []trace.Event {
		return []trace.Event{
			{Seq: 1, PID: 100, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/bin/cc", "-O3", "-DNDEBUG", "-o", "CMakeFiles/tracecore.dir/core.c.o", "-c", "/tmp/work/core.c"}},
			{Seq: 2, PID: 100, Cwd: "/tmp/work/build", Kind: trace.EventClone, ChildPID: 101},
			{Seq: 3, PID: 101, ParentPID: 100, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/lib/gcc/cc1", "/tmp/work/core.c"}},
			{Seq: 4, PID: 101, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/core.c"},
			{Seq: 5, PID: 101, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/trace_options.h"},
			{Seq: 6, PID: 101, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o.d"},
			{Seq: 7, PID: 100, Cwd: "/tmp/work/build", Kind: trace.EventClone, ChildPID: 102},
			{Seq: 8, PID: 102, ParentPID: 100, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"as", "-o", "CMakeFiles/tracecore.dir/core.c.o"}},
			{Seq: 9, PID: 102, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"},
			{Seq: 10, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/bin/ar", "qc", "libtracecore.a", "CMakeFiles/tracecore.dir/core.c.o"}},
			{Seq: 11, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 12, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"},
			{Seq: 13, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 14, PID: 104, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"cmake", "--install", "/tmp/work/build", "--prefix", "/tmp/work/install"}},
			{Seq: 15, PID: 104, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 16, PID: 104, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/install/lib/libtracecore.a"},
		}
	}

	makeCliEvents := func() []trace.Event {
		return []trace.Event{
			{Seq: 1, PID: 200, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/bin/cc", "cli.c", "-o", "tracecli", "libtracecore.a"}},
			{Seq: 2, PID: 201, ParentPID: 200, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/bin/ld", "-o", "tracecli"}},
			{Seq: 3, PID: 201, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 4, PID: 201, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/tracecli"},
			{Seq: 5, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"cmake", "--install", "/tmp/work/build", "--prefix", "/tmp/work/install"}},
			{Seq: 6, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 7, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/tracecli"},
			{Seq: 8, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/install/lib/libtracecore.a"},
			{Seq: 9, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/install/bin/tracecli"},
		}
	}

	probes := map[string]ProbeResult{
		"api-off-cli-off": {
			Events: makeCompileEvents(),
			Scope:  scope,
			InputDigests: map[string]string{
				"/tmp/work/build/trace_options.h":                   "trace-off",
				"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o": "core-off",
				"/tmp/work/build/libtracecore.a":                    "archive-off",
			},
		},
		"api-on-cli-off": {
			Events: makeCompileEvents(),
			Scope:  scope,
			InputDigests: map[string]string{
				"/tmp/work/build/trace_options.h":                   "trace-api",
				"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o": "core-api",
				"/tmp/work/build/libtracecore.a":                    "archive-api",
			},
		},
		"api-off-cli-on": {
			Events: append(makeCompileEvents(), makeCliEvents()...),
			Scope:  scope,
			InputDigests: map[string]string{
				"/tmp/work/build/trace_options.h":                   "trace-off",
				"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o": "core-off",
				"/tmp/work/build/libtracecore.a":                    "archive-off",
				"/tmp/work/build/tracecli":                          "cli-bin",
			},
		},
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return probes[combo], nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}
	if !trusted {
		t.Fatalf("Watch() trusted = false, want true")
	}
	want := []string{
		"api-off-cli-off",
		"api-off-cli-on",
		"api-on-cli-off",
		"api-on-cli-on",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchScopedTraceoptionsShipAliasStaysOrthogonal(t *testing.T) {
	matrix := formula.Matrix{
		Options: map[string][]string{
			"api":  {"api-off", "api-on"},
			"ship": {"ship-off", "ship-on"},
		},
		DefaultOptions: map[string][]string{
			"api":  {"api-off"},
			"ship": {"ship-off"},
		},
	}

	makeProbe := func(tag string, api, ship bool) ProbeResult {
		sourceRoot := "/tmp/work-" + tag
		buildRoot := sourceRoot + "/_build"
		installRoot := "/tmp/install-" + tag
		scope := trace.Scope{
			SourceRoot:  sourceRoot,
			BuildRoot:   buildRoot,
			InstallRoot: installRoot,
		}
		configureArgs := []string{
			"/usr/bin/cmake",
			"-S", sourceRoot,
			"-B", buildRoot,
			"-DTRACE_FEATURE_API=" + map[bool]string{false: "OFF", true: "ON"}[api],
			"-DTRACE_INSTALL_ALIAS=" + map[bool]string{false: "OFF", true: "ON"}[ship],
		}
		records := []trace.Record{
			record(configureArgs, sourceRoot,
				[]string{sourceRoot + "/CMakeLists.txt", sourceRoot + "/trace_options.h.in"},
				[]string{buildRoot + "/trace_options.h", buildRoot + "/cmake_install.cmake", buildRoot + "/install_manifest.txt"}),
			record([]string{"/usr/bin/cc", "-c", sourceRoot + "/core.c", "-o", buildRoot + "/CMakeFiles/tracecore.dir/core.c.o"}, buildRoot,
				[]string{sourceRoot + "/core.c", buildRoot + "/trace_options.h"},
				[]string{buildRoot + "/CMakeFiles/tracecore.dir/core.c.o"}),
			record([]string{"/usr/bin/ar", "qc", buildRoot + "/libtracecore.a", buildRoot + "/CMakeFiles/tracecore.dir/core.c.o"}, buildRoot,
				[]string{buildRoot + "/libtracecore.a", buildRoot + "/CMakeFiles/tracecore.dir/core.c.o"},
				[]string{buildRoot + "/libtracecore.a"}),
			record([]string{"/usr/bin/cmake", "--install", buildRoot, "--prefix", installRoot}, buildRoot,
				[]string{buildRoot + "/cmake_install.cmake", buildRoot + "/libtracecore.a", sourceRoot + "/trace.h", buildRoot + "/trace_options.h"},
				[]string{installRoot + "/lib/libtracecore.a", installRoot + "/include/trace.h", installRoot + "/include/trace_options.h"}),
		}
		if ship {
			records[3].Changes = append(records[3].Changes, installRoot+"/include/trace_alias.h")
		}

		traceOptionsDigest := "trace-off"
		coreDigest := "core-off"
		archiveDigest := "archive-off"
		installTraceOptionsDigest := "install-trace-off"
		installArchiveDigest := "install-archive-off"
		if api {
			traceOptionsDigest = "trace-api"
			coreDigest = "core-api"
			archiveDigest = "archive-api"
			installTraceOptionsDigest = "install-trace-api"
			installArchiveDigest = "install-archive-api"
		}

		manifest := OutputManifest{
			Entries: map[string]OutputEntry{
				"include/trace.h":         {Kind: "file", Digest: "trace-h"},
				"include/trace_options.h": {Kind: "file", Digest: installTraceOptionsDigest},
				"lib/libtracecore.a":      {Kind: "archive", Digest: installArchiveDigest},
			},
		}
		if ship {
			manifest.Entries["include/trace_alias.h"] = OutputEntry{Kind: "file", Digest: "trace-h"}
		}

		return ProbeResult{
			Records: records,
			Scope:   scope,
			InputDigests: map[string]string{
				buildRoot + "/trace_options.h":                   traceOptionsDigest,
				buildRoot + "/CMakeFiles/tracecore.dir/core.c.o": coreDigest,
				buildRoot + "/libtracecore.a":                    archiveDigest,
			},
			OutputManifest: manifest,
		}
	}

	probes := map[string]ProbeResult{
		"api-off-ship-off": makeProbe("base", false, false),
		"api-on-ship-off":  makeProbe("api", true, false),
		"api-off-ship-on":  makeProbe("ship", false, true),
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return probes[combo], nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}
	if !trusted {
		t.Fatalf("Watch() trusted = false, want true")
	}
	want := []string{
		"api-off-ship-off",
		"api-off-ship-on",
		"api-on-ship-off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchEventBackedCompilerChildRecordsFallbackCreatesCollision(t *testing.T) {
	matrix := formula.Matrix{
		Options: map[string][]string{
			"api": {"api-off", "api-on"},
			"cli": {"cli-off", "cli-on"},
		},
		DefaultOptions: map[string][]string{
			"api": {"api-off"},
			"cli": {"cli-off"},
		},
	}
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/build",
		InstallRoot: "/tmp/work/install",
	}

	makeCompileRecords := func(api bool) []trace.Record {
		driver := []string{"/usr/bin/cc", "-O3", "-DNDEBUG", "-o", "CMakeFiles/tracecore.dir/core.c.o", "-c", "/tmp/work/core.c"}
		if api {
			driver = []string{"/usr/bin/cc", "-DTRACE_FEATURE_API", "-O3", "-DNDEBUG", "-o", "CMakeFiles/tracecore.dir/core.c.o", "-c", "/tmp/work/core.c"}
		}
		return []trace.Record{
			{PID: 100, ParentPID: 50, Argv: driver, Cwd: "/tmp/work/build"},
			{PID: 101, ParentPID: 100, Argv: []string{"/usr/lib/gcc/cc1", "/tmp/work/core.c"}, Cwd: "/tmp/work/build", Inputs: []string{"/tmp/work/core.c", "/tmp/work/build/trace_options.h"}, Changes: []string{"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o.d"}},
			{PID: 102, ParentPID: 100, Argv: []string{"as", "-o", "CMakeFiles/tracecore.dir/core.c.o"}, Cwd: "/tmp/work/build", Changes: []string{"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"}},
			{PID: 103, ParentPID: 50, Argv: []string{"/usr/bin/ar", "qc", "libtracecore.a", "CMakeFiles/tracecore.dir/core.c.o"}, Cwd: "/tmp/work/build", Inputs: []string{"/tmp/work/build/libtracecore.a", "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"}, Changes: []string{"/tmp/work/build/libtracecore.a", "/tmp/work/build/stArchiveTmp"}},
			{PID: 104, ParentPID: 50, Argv: []string{"/usr/bin/ranlib", "libtracecore.a"}, Cwd: "/tmp/work/build", Inputs: []string{"/tmp/work/build/libtracecore.a"}, Changes: []string{"/tmp/work/build/libtracecore.a", "/tmp/work/build/stRanlibTmp"}},
			{PID: 105, ParentPID: 50, Argv: []string{"cmake", "--install", "/tmp/work/build", "--prefix", "/tmp/work/install"}, Cwd: "/tmp/work/build", Inputs: []string{"/tmp/work/build/libtracecore.a"}, Changes: []string{"/tmp/work/install/lib/libtracecore.a"}},
		}
	}

	makeCompileEventsWithoutParents := func(api bool) []trace.Event {
		driver := []string{"/usr/bin/cc", "-O3", "-DNDEBUG", "-o", "CMakeFiles/tracecore.dir/core.c.o", "-c", "/tmp/work/core.c"}
		if api {
			driver = []string{"/usr/bin/cc", "-DTRACE_FEATURE_API", "-O3", "-DNDEBUG", "-o", "CMakeFiles/tracecore.dir/core.c.o", "-c", "/tmp/work/core.c"}
		}
		return []trace.Event{
			{Seq: 1, PID: 100, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: driver},
			{Seq: 2, PID: 101, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/lib/gcc/cc1", "/tmp/work/core.c"}},
			{Seq: 3, PID: 101, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/core.c"},
			{Seq: 4, PID: 101, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/trace_options.h"},
			{Seq: 5, PID: 101, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o.d"},
			{Seq: 6, PID: 102, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"as", "-o", "CMakeFiles/tracecore.dir/core.c.o"}},
			{Seq: 7, PID: 102, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"},
			{Seq: 8, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/bin/ar", "qc", "libtracecore.a", "CMakeFiles/tracecore.dir/core.c.o"}},
			{Seq: 9, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 10, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"},
			{Seq: 11, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 12, PID: 103, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/stArchiveTmp"},
			{Seq: 13, PID: 104, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/bin/ranlib", "libtracecore.a"}},
			{Seq: 14, PID: 104, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 15, PID: 104, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 16, PID: 104, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/stRanlibTmp"},
			{Seq: 17, PID: 105, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"cmake", "--install", "/tmp/work/build", "--prefix", "/tmp/work/install"}},
			{Seq: 18, PID: 105, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 19, PID: 105, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/install/lib/libtracecore.a"},
		}
	}

	makeCliRecords := func() []trace.Record {
		return []trace.Record{
			{PID: 200, ParentPID: 60, Argv: []string{"/usr/bin/cc", "cli.c", "-o", "tracecli", "libtracecore.a"}, Cwd: "/tmp/work/build"},
			{PID: 201, ParentPID: 200, Argv: []string{"/usr/bin/ld", "-o", "tracecli"}, Cwd: "/tmp/work/build", Inputs: []string{"/tmp/work/build/libtracecore.a"}, Changes: []string{"/tmp/work/build/tracecli"}},
			{PID: 202, ParentPID: 60, Argv: []string{"cmake", "--install", "/tmp/work/build", "--prefix", "/tmp/work/install"}, Cwd: "/tmp/work/build", Inputs: []string{"/tmp/work/build/libtracecore.a", "/tmp/work/build/tracecli"}, Changes: []string{"/tmp/work/install/lib/libtracecore.a", "/tmp/work/install/bin/tracecli"}},
		}
	}

	makeCliEventsWithoutParents := func() []trace.Event {
		return []trace.Event{
			{Seq: 1, PID: 200, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/bin/cc", "cli.c", "-o", "tracecli", "libtracecore.a"}},
			{Seq: 2, PID: 201, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"/usr/bin/ld", "-o", "tracecli"}},
			{Seq: 3, PID: 201, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 4, PID: 201, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/tracecli"},
			{Seq: 5, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"cmake", "--install", "/tmp/work/build", "--prefix", "/tmp/work/install"}},
			{Seq: 6, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/libtracecore.a"},
			{Seq: 7, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/tracecli"},
			{Seq: 8, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/install/lib/libtracecore.a"},
			{Seq: 9, PID: 202, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/install/bin/tracecli"},
		}
	}

	probes := map[string]ProbeResult{
		"api-off-cli-off": {
			Events:  makeCompileEventsWithoutParents(false),
			Records: makeCompileRecords(false),
			Scope:   scope,
			InputDigests: map[string]string{
				"/tmp/work/build/trace_options.h":                   "trace-off",
				"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o": "core-off",
				"/tmp/work/build/libtracecore.a":                    "archive-off",
			},
		},
		"api-on-cli-off": {
			Events:  makeCompileEventsWithoutParents(true),
			Records: makeCompileRecords(true),
			Scope:   scope,
			InputDigests: map[string]string{
				"/tmp/work/build/trace_options.h":                   "trace-api",
				"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o": "core-api",
				"/tmp/work/build/libtracecore.a":                    "archive-api",
			},
		},
		"api-off-cli-on": {
			Events:  append(makeCompileEventsWithoutParents(false), makeCliEventsWithoutParents()...),
			Records: append(makeCompileRecords(false), makeCliRecords()...),
			Scope:   scope,
			InputDigests: map[string]string{
				"/tmp/work/build/trace_options.h":                   "trace-off",
				"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o": "core-off",
				"/tmp/work/build/libtracecore.a":                    "archive-off",
				"/tmp/work/build/tracecli":                          "cli-bin",
			},
		},
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return probes[combo], nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}
	if !trusted {
		t.Fatalf("Watch() trusted = false, want true")
	}
	want := []string{
		"api-off-cli-off",
		"api-off-cli-on",
		"api-on-cli-off",
		"api-on-cli-on",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchReturnsUntrustedForTraceDiagnostics(t *testing.T) {
	matrix := formula.Matrix{
		Options:        map[string][]string{"feat": {"feat-off", "feat-on"}},
		DefaultOptions: map[string][]string{"feat": {"feat-off"}},
	}
	probe := ProbeResult{
		Records:          []trace.Record{record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"})},
		TraceDiagnostics: trace.ParseDiagnostics{MissingPIDLines: 1},
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return probe, nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"feat-off", "feat-on"}) {
		t.Fatalf("Watch() = %v, want %v", got, []string{"feat-off", "feat-on"})
	}
	if trusted {
		t.Fatalf("Watch() trusted = true, want false")
	}
}

func TestWatchCollidingOptions(t *testing.T) {
	matrix := testMatrix()
	traces := map[string][]trace.Record{
		"amd64-linux|doc-off-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-on-tls-off": {
			record([]string{"cc", "-DDOC", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-off-tls-on": {
			record([]string{"cc", "-DTLS", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
	}

	got, _, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return ProbeResult{Records: traces[combo]}, nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}

	want := []string{
		"amd64-linux|doc-off-tls-off",
		"amd64-linux|doc-off-tls-on",
		"amd64-linux|doc-on-tls-off",
		"amd64-linux|doc-on-tls-on",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchSuppressesPureWriteWriteCollisionWhenOutputManifestMatches(t *testing.T) {
	matrix := testMatrix()
	manifest := outputManifest("share/generated.txt", OutputEntry{Kind: "file", Digest: "same"})
	traces := map[string]ProbeResult{
		"amd64-linux|doc-off-tls-off": {OutputManifest: manifest},
		"amd64-linux|doc-on-tls-off": {
			Records:        []trace.Record{record([]string{"python", "gen_doc_asset.py"}, "/tmp/work", []string{"/tmp/work/doc.in"}, []string{"/tmp/work/out/share/generated.txt"})},
			OutputManifest: manifest,
		},
		"amd64-linux|doc-off-tls-on": {
			Records:        []trace.Record{record([]string{"perl", "gen_tls_asset.pl"}, "/tmp/work", []string{"/tmp/work/tls.in"}, []string{"/tmp/work/out/share/generated.txt"})},
			OutputManifest: manifest,
		},
	}

	got, _, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return traces[combo], nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}

	want := []string{
		"amd64-linux|doc-off-tls-off",
		"amd64-linux|doc-off-tls-on",
		"amd64-linux|doc-on-tls-off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchSkipsMergeCleanOrthogonalSingletonPair(t *testing.T) {
	matrix := testMatrix()

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	writeMergeFile(t, filepath.Join(leftDir, "share", "doc", "index.html"), []byte("<html>doc</html>\n"), 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "bin", "tls-helper"), []byte("#!/bin/sh\necho tls\n"), 0o755)

	baseManifest, err := BuildOutputManifest(baseDir, "")
	if err != nil {
		t.Fatal(err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "")
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "")
	if err != nil {
		t.Fatal(err)
	}

	traces := map[string]ProbeResult{
		"amd64-linux|doc-off-tls-off": {OutputDir: baseDir, OutputManifest: baseManifest},
		"amd64-linux|doc-on-tls-off": {
			Records:        []trace.Record{record([]string{"sphinx-build", "docs", "out/share/doc"}, "/tmp/work", []string{"/tmp/work/docs/index.md"}, []string{"/tmp/work/out/share/doc/index.html"})},
			OutputDir:      leftDir,
			OutputManifest: leftManifest,
		},
		"amd64-linux|doc-off-tls-on": {
			Records:        []trace.Record{record([]string{"python", "gen_tls.py"}, "/tmp/work", []string{"/tmp/work/gen_tls.py"}, []string{"/tmp/work/out/bin/tls-helper"})},
			OutputDir:      rightDir,
			OutputManifest: rightManifest,
		},
	}

	got, _, err := WatchWithOptions(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return traces[combo], nil
	}, WatchOptions{
		ValidateMergedPair: func(_ context.Context, _ string, _ OutputMergeResult) (bool, error) {
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("WatchWithOptions() unexpected error: %v", err)
	}

	want := []string{
		"amd64-linux|doc-off-tls-off",
		"amd64-linux|doc-off-tls-on",
		"amd64-linux|doc-on-tls-off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WatchWithOptions() = %v, want %v", got, want)
	}
}

func TestWatchReincludesMergeCleanPairWhenValidatorRejects(t *testing.T) {
	matrix := testMatrix()

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	writeMergeFile(t, filepath.Join(leftDir, "share", "doc", "index.html"), []byte("<html>doc</html>\n"), 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "bin", "tls-helper"), []byte("#!/bin/sh\necho tls\n"), 0o755)

	baseManifest, err := BuildOutputManifest(baseDir, "")
	if err != nil {
		t.Fatal(err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "")
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "")
	if err != nil {
		t.Fatal(err)
	}

	traces := map[string]ProbeResult{
		"amd64-linux|doc-off-tls-off": {OutputDir: baseDir, OutputManifest: baseManifest},
		"amd64-linux|doc-on-tls-off": {
			Records:        []trace.Record{record([]string{"sphinx-build", "docs", "out/share/doc"}, "/tmp/work", []string{"/tmp/work/docs/index.md"}, []string{"/tmp/work/out/share/doc/index.html"})},
			OutputDir:      leftDir,
			OutputManifest: leftManifest,
		},
		"amd64-linux|doc-off-tls-on": {
			Records:        []trace.Record{record([]string{"python", "gen_tls.py"}, "/tmp/work", []string{"/tmp/work/gen_tls.py"}, []string{"/tmp/work/out/bin/tls-helper"})},
			OutputDir:      rightDir,
			OutputManifest: rightManifest,
		},
	}

	var validatedCombo string
	got, _, err := WatchWithOptions(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return traces[combo], nil
	}, WatchOptions{
		ValidateMergedPair: func(_ context.Context, combo string, merged OutputMergeResult) (bool, error) {
			validatedCombo = combo
			if merged.Root == "" {
				t.Fatal("expected merged root for clean merge")
			}
			return false, nil
		},
	})
	if err != nil {
		t.Fatalf("WatchWithOptions() unexpected error: %v", err)
	}

	wantCombo := "amd64-linux|doc-on-tls-on"
	if validatedCombo != wantCombo {
		t.Fatalf("validated combo = %q, want %q", validatedCombo, wantCombo)
	}

	want := []string{
		"amd64-linux|doc-off-tls-off",
		"amd64-linux|doc-off-tls-on",
		"amd64-linux|doc-on-tls-off",
		"amd64-linux|doc-on-tls-on",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WatchWithOptions() = %v, want %v", got, want)
	}
}

func TestWatchWithValidatorKeepsGraphCollidingSingletonPair(t *testing.T) {
	matrix := testMatrix()

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	writeMergeFile(t, filepath.Join(leftDir, "share", "doc", "index.html"), []byte("<html>doc</html>\n"), 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "bin", "tls-helper"), []byte("#!/bin/sh\necho tls\n"), 0o755)

	baseManifest, err := BuildOutputManifest(baseDir, "")
	if err != nil {
		t.Fatal(err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "")
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "")
	if err != nil {
		t.Fatal(err)
	}

	traces := map[string]ProbeResult{
		"amd64-linux|doc-off-tls-off": {
			Records:        []trace.Record{record([]string{"builder", "foundation"}, "/tmp/work", []string{"/tmp/work/foundation.src"}, []string{"/tmp/work/_build/lib/libfoo.a"})},
			OutputDir:      baseDir,
			OutputManifest: baseManifest,
		},
		"amd64-linux|doc-on-tls-off": {
			Records:        []trace.Record{record([]string{"builder", "foundation", "--doc"}, "/tmp/work", []string{"/tmp/work/foundation.src"}, []string{"/tmp/work/_build/lib/libfoo.a"})},
			OutputDir:      leftDir,
			OutputManifest: leftManifest,
		},
		"amd64-linux|doc-off-tls-on": {
			Records:        []trace.Record{record([]string{"builder", "foundation", "--tls"}, "/tmp/work", []string{"/tmp/work/foundation.src"}, []string{"/tmp/work/_build/lib/libfoo.a"})},
			OutputDir:      rightDir,
			OutputManifest: rightManifest,
		},
	}

	validated := false
	got, _, err := WatchWithOptions(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return traces[combo], nil
	}, WatchOptions{
		ValidateMergedPair: func(_ context.Context, combo string, merged OutputMergeResult) (bool, error) {
			validated = true
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("WatchWithOptions() unexpected error: %v", err)
	}

	want := []string{
		"amd64-linux|doc-off-tls-off",
		"amd64-linux|doc-off-tls-on",
		"amd64-linux|doc-on-tls-off",
		"amd64-linux|doc-on-tls-on",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WatchWithOptions() = %v, want %v", got, want)
	}
	if validated {
		t.Fatalf("validator unexpectedly ran for graph-colliding pair")
	}
}

func TestWatchWithValidatorObservesReplayUnavailableForGraphCollidingPair(t *testing.T) {
	matrix := testMatrix()
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	baseManifest, err := BuildOutputManifest(baseDir, "")
	if err != nil {
		t.Fatal(err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "")
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "")
	if err != nil {
		t.Fatal(err)
	}

	probes := map[string]ProbeResult{
		"amd64-linux|doc-off-tls-off": {
			Records: []trace.Record{{
				PID:       1,
				ParentPID: 0,
				Argv:      []string{"builder", "foundation"},
				Cwd:       "/tmp/work",
				Env:       []string{"PATH=/usr/bin"},
				Inputs:    []string{"/tmp/work/foundation.src"},
				Changes:   []string{"/tmp/work/_build/lib/libfoo.a"},
			}},
			Scope:          scope,
			OutputDir:      baseDir,
			OutputManifest: baseManifest,
			ReplayReady:    true,
		},
		"amd64-linux|doc-on-tls-off": {
			Records: []trace.Record{{
				PID:       2,
				ParentPID: 0,
				Argv:      []string{"builder", "foundation", "--doc"},
				Cwd:       "/tmp/work",
				Env:       []string{"PATH=/usr/bin"},
				Inputs:    []string{"/tmp/work/foundation.src"},
				Changes:   []string{"/tmp/work/_build/lib/libfoo.a"},
			}},
			Scope:          scope,
			OutputDir:      leftDir,
			OutputManifest: leftManifest,
			ReplayReady:    true,
		},
		"amd64-linux|doc-off-tls-on": {
			Records: []trace.Record{{
				PID:       3,
				ParentPID: 0,
				Argv:      []string{"builder", "foundation", "--tls"},
				Cwd:       "/tmp/work",
				Env:       []string{"PATH=/usr/bin"},
				Inputs:    []string{"/tmp/work/foundation.src"},
				Changes:   []string{"/tmp/work/_build/lib/libfoo.a"},
			}},
			Scope:          scope,
			OutputDir:      rightDir,
			OutputManifest: rightManifest,
			ReplayReady:    true,
		},
	}

	var observed []SynthesizedPairObservation
	got, _, err := WatchWithOptions(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return probes[combo], nil
	}, WatchOptions{
		ObserveSynthesizedPair: func(observation SynthesizedPairObservation) {
			observed = append(observed, observation)
		},
	})
	if err != nil {
		t.Fatalf("WatchWithOptions() unexpected error: %v", err)
	}

	want := []string{
		"amd64-linux|doc-off-tls-off",
		"amd64-linux|doc-off-tls-on",
		"amd64-linux|doc-on-tls-off",
		"amd64-linux|doc-on-tls-on",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WatchWithOptions() = %v, want %v", got, want)
	}
	if len(observed) != 1 {
		t.Fatalf("observed pairs = %d, want 1", len(observed))
	}
	observation := observed[0]
	if observation.Combo != "amd64-linux|doc-on-tls-on" {
		t.Fatalf("observed combo = %q, want %q", observation.Combo, "amd64-linux|doc-on-tls-on")
	}
	if observation.SynthesisResult.Mode != OutputSynthesisModeRootReplay {
		t.Fatalf("mode = %q, want %q", observation.SynthesisResult.Mode, OutputSynthesisModeRootReplay)
	}
	if observation.SynthesisResult.Replay == nil {
		t.Fatal("replay summary = nil, want replay summary")
	}
	if !strings.Contains(observation.SynthesisResult.Replay.Unavailable, "eligible replay root identities differ across probes") {
		t.Fatalf("replay unavailable = %q, want root identity mismatch", observation.SynthesisResult.Replay.Unavailable)
	}
	if len(observation.SynthesisResult.Issues) == 0 {
		t.Fatal("issues = 0, want root replay unavailable issue")
	}
	if observation.SynthesisResult.Issues[0].Kind != OutputMergeIssueKindRootReplayUnavailable {
		t.Fatalf("issue kind = %q, want %q", observation.SynthesisResult.Issues[0].Kind, OutputMergeIssueKindRootReplayUnavailable)
	}
}

func TestWatchWithValidatorCanSkipMergeSurfaceOnlyPair(t *testing.T) {
	matrix := testMatrix()
	scope := trace.Scope{
		BuildRoot:   "/tmp/work/build",
		InstallRoot: "/tmp/work/install",
	}

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	writeMergeFile(t, filepath.Join(leftDir, "share", "generated.txt"), []byte("stable\n"), 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "share", "generated.txt"), []byte("stable\n"), 0o644)

	baseManifest, err := BuildOutputManifest(baseDir, "")
	if err != nil {
		t.Fatal(err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "")
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "")
	if err != nil {
		t.Fatal(err)
	}

	traces := map[string]ProbeResult{
		"amd64-linux|doc-off-tls-off": {
			Scope:          scope,
			OutputDir:      baseDir,
			OutputManifest: baseManifest,
		},
		"amd64-linux|doc-on-tls-off": {
			Records: []trace.Record{
				record([]string{"python", "gen-doc-part"}, "/tmp/work", []string{"/tmp/work/docs/input.md"}, []string{"/tmp/work/build/doc.part"}),
				record([]string{"python", "emit-generated"}, "/tmp/work", []string{"/tmp/work/build/doc.part"}, []string{"/tmp/work/install/share/generated.txt"}),
			},
			Scope:          scope,
			OutputDir:      leftDir,
			OutputManifest: leftManifest,
		},
		"amd64-linux|doc-off-tls-on": {
			Records: []trace.Record{
				record([]string{"python", "gen-tls-part"}, "/tmp/work", []string{"/tmp/work/tls/input.cfg"}, []string{"/tmp/work/build/tls.part"}),
				record([]string{"python", "emit-generated"}, "/tmp/work", []string{"/tmp/work/build/tls.part"}, []string{"/tmp/work/install/share/generated.txt"}),
			},
			Scope:          scope,
			OutputDir:      rightDir,
			OutputManifest: rightManifest,
		},
	}

	var validatedCombo string
	got, _, err := WatchWithOptions(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return traces[combo], nil
	}, WatchOptions{
		ValidateMergedPair: func(_ context.Context, combo string, merged OutputMergeResult) (bool, error) {
			validatedCombo = combo
			if combo != "amd64-linux|doc-on-tls-on" {
				t.Fatalf("validated combo = %q, want %q", combo, "amd64-linux|doc-on-tls-on")
			}
			if !merged.Clean() {
				t.Fatalf("expected clean merged output, got issues: %v", merged.Issues)
			}
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("WatchWithOptions() unexpected error: %v", err)
	}

	if validatedCombo != "amd64-linux|doc-on-tls-on" {
		t.Fatalf("validated combo = %q, want %q", validatedCombo, "amd64-linux|doc-on-tls-on")
	}

	want := []string{
		"amd64-linux|doc-off-tls-off",
		"amd64-linux|doc-off-tls-on",
		"amd64-linux|doc-on-tls-off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WatchWithOptions() = %v, want %v", got, want)
	}
}

func TestWatchWithoutValidatorDoesNotExpandIndependentPair(t *testing.T) {
	matrix := testMatrix()

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	writeMergeFile(t, filepath.Join(leftDir, "share", "doc", "index.html"), []byte("doc"), 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "bin", "tls-helper"), []byte("tls"), 0o755)

	baseManifest, err := BuildOutputManifest(baseDir, "")
	if err != nil {
		t.Fatal(err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "")
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "")
	if err != nil {
		t.Fatal(err)
	}

	traces := map[string]ProbeResult{
		"amd64-linux|doc-off-tls-off": {OutputDir: baseDir, OutputManifest: baseManifest},
		"amd64-linux|doc-on-tls-off": {
			Records:        []trace.Record{record([]string{"sphinx-build", "docs", "out/share/doc"}, "/tmp/work", []string{"/tmp/work/docs/index.md"}, []string{"/tmp/work/out/share/doc/index.html"})},
			OutputDir:      leftDir,
			OutputManifest: leftManifest,
		},
		"amd64-linux|doc-off-tls-on": {
			Records:        []trace.Record{record([]string{"python", "gen_tls.py"}, "/tmp/work", []string{"/tmp/work/gen_tls.py"}, []string{"/tmp/work/out/bin/tls-helper"})},
			OutputDir:      rightDir,
			OutputManifest: rightManifest,
		},
	}

	got, _, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return traces[combo], nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}

	want := []string{
		"amd64-linux|doc-off-tls-off",
		"amd64-linux|doc-off-tls-on",
		"amd64-linux|doc-on-tls-off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchIncludesMergeConflictingOrthogonalSingletonPair(t *testing.T) {
	matrix := testMatrix()

	baseDir := t.TempDir()
	leftDir := t.TempDir()
	rightDir := t.TempDir()
	writeMergeFile(t, filepath.Join(leftDir, "share", "generated.bin"), []byte{0x01, 0x02}, 0o644)
	writeMergeFile(t, filepath.Join(rightDir, "share", "generated.bin"), []byte{0x01, 0x03}, 0o644)

	baseManifest, err := BuildOutputManifest(baseDir, "")
	if err != nil {
		t.Fatal(err)
	}
	leftManifest, err := BuildOutputManifest(leftDir, "")
	if err != nil {
		t.Fatal(err)
	}
	rightManifest, err := BuildOutputManifest(rightDir, "")
	if err != nil {
		t.Fatal(err)
	}

	traces := map[string]ProbeResult{
		"amd64-linux|doc-off-tls-off": {OutputDir: baseDir, OutputManifest: baseManifest},
		"amd64-linux|doc-on-tls-off": {
			Records:        []trace.Record{record([]string{"sphinx-build", "docs", "out/share/doc"}, "/tmp/work", []string{"/tmp/work/docs/index.md"}, []string{"/tmp/work/out/share/doc/index.html"})},
			OutputDir:      leftDir,
			OutputManifest: leftManifest,
		},
		"amd64-linux|doc-off-tls-on": {
			Records:        []trace.Record{record([]string{"python", "gen_tls.py"}, "/tmp/work", []string{"/tmp/work/gen_tls.py"}, []string{"/tmp/work/out/bin/tls-helper"})},
			OutputDir:      rightDir,
			OutputManifest: rightManifest,
		},
	}

	got, _, err := WatchWithOptions(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return traces[combo], nil
	}, WatchOptions{
		ValidateMergedPair: func(_ context.Context, _ string, _ OutputMergeResult) (bool, error) {
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("WatchWithOptions() unexpected error: %v", err)
	}

	want := []string{
		"amd64-linux|doc-off-tls-off",
		"amd64-linux|doc-off-tls-on",
		"amd64-linux|doc-on-tls-off",
		"amd64-linux|doc-on-tls-on",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WatchWithOptions() = %v, want %v", got, want)
	}
}

func TestWatchDoesNotExpandToolingOnlyRecordOptions(t *testing.T) {
	matrix := testMatrix()
	traces := map[string][]trace.Record{
		"amd64-linux|doc-off-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-on-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
			record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CheckIncludeFile.c"}),
			record([]string{"cc", "-c", "CheckIncludeFile.c", "-o", "CheckIncludeFile.c.o"}, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc",
				[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CheckIncludeFile.c"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CheckIncludeFile.c.o"}),
			record([]string{"cc", "CheckIncludeFile.c.o", "-o", "cmTC_doc"}, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc",
				[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CheckIncludeFile.c.o"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/cmTC_doc"}),
		},
		"amd64-linux|doc-off-tls-on": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
			record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-tls/OFF64_T.c"}),
			record([]string{"cc", "-c", "OFF64_T.c", "-o", "OFF64_T.c.o"}, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-tls",
				[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-tls/OFF64_T.c"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-tls/OFF64_T.c.o"}),
			record([]string{"cc", "OFF64_T.c.o", "-o", "cmTC_tls"}, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-tls",
				[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-tls/OFF64_T.c.o"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-tls/cmTC_tls"}),
		},
	}

	got, _, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return ProbeResult{Records: traces[combo]}, nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}

	want := []string{
		"amd64-linux|doc-off-tls-off",
		"amd64-linux|doc-off-tls-on",
		"amd64-linux|doc-on-tls-off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchIgnoresDeliveryOnlyReadsUnderInstallRoot(t *testing.T) {
	matrix := testMatrix()
	traces := map[string][]trace.Record{
		"amd64-linux|doc-off-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-on-tls-off": {
			record([]string{"cc", "-DDOC", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-off-tls-on": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
			record([]string{"custom-installer", "install"}, "/tmp/work",
				[]string{"/tmp/work/out/lib/libfoo.a", "/tmp/work/build/core.o"},
				[]string{"/tmp/work/install/include/boost/foo.hpp", "/tmp/work/install/lib/libfoo.a"}),
		},
	}

	got, _, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return ProbeResult{Records: traces[combo], Scope: trace.Scope{InstallRoot: "/tmp/work/install"}}, nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}

	want := []string{
		"amd64-linux|doc-off-tls-off",
		"amd64-linux|doc-off-tls-on",
		"amd64-linux|doc-on-tls-off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestProfilesCollideOnSharedSeedWrites(t *testing.T) {
	path := normalizePath("/tmp/work/_build/checks/libarch.a")
	left := initOptionProfile()
	right := initOptionProfile()
	left.seedWrites[path] = struct{}{}
	right.seedWrites[path] = struct{}{}
	left.seedStates[pathStateKey{path: path}] = struct{}{}
	right.seedStates[pathStateKey{path: path}] = struct{}{}

	if !profilesCollide([]optionVariant{{profile: left}}, []optionVariant{{profile: right}}, false) {
		t.Fatalf("profilesCollide(strict) = false, want true")
	}
	if !profilesCollide([]optionVariant{{profile: left}}, []optionVariant{{profile: right}}, true) {
		t.Fatalf("profilesCollide(merge-aware) = false, want true")
	}
}

func TestOptionVariantsAllowMergeSurfaceOnlySharedSlice(t *testing.T) {
	left := optionVariant{
		profile: optionProfile{
			slicePaths: map[string]struct{}{"app.exe": {}},
		},
		mergeSurfacePaths: map[string]struct{}{"app.exe": {}},
	}
	right := optionVariant{
		profile: optionProfile{
			slicePaths: map[string]struct{}{"app.exe": {}},
		},
		mergeSurfacePaths: map[string]struct{}{"app.exe": {}},
	}

	if !optionVariantsCollide(left, right, false) {
		t.Fatalf("optionVariantsCollide(strict) = false, want true")
	}
	if optionVariantsCollide(left, right, true) {
		t.Fatalf("optionVariantsCollide(merge-aware) = true, want false")
	}
}

func TestProfilesDoNotCollideOnSharedTombstoneSeedStates(t *testing.T) {
	path := normalizePath("/tmp/work/build/generated.h")
	left := initOptionProfile()
	right := initOptionProfile()
	left.seedWrites[path] = struct{}{}
	right.seedWrites[path] = struct{}{}
	left.seedStates[pathStateKey{path: path, tombstone: true}] = struct{}{}
	right.seedStates[pathStateKey{path: path, tombstone: true}] = struct{}{}
	left.flowStates[pathStateKey{path: path, tombstone: true}] = struct{}{}
	right.flowStates[pathStateKey{path: path, tombstone: true}] = struct{}{}
	left.slicePaths[path] = struct{}{}
	right.slicePaths[path] = struct{}{}

	if profilesCollide([]optionVariant{{profile: left}}, []optionVariant{{profile: right}}, false) {
		t.Fatalf("profilesCollide(strict) = true, want false for compatible shared tombstones")
	}
	if profilesCollide([]optionVariant{{profile: left}}, []optionVariant{{profile: right}}, true) {
		t.Fatalf("profilesCollide(merge-aware) = true, want false for compatible shared tombstones")
	}
}

func TestProfilesDoNotCollideWhenNeedAndFlowShareTombstoneState(t *testing.T) {
	path := normalizePath("/tmp/work/build/generated.h")
	left := initOptionProfile()
	right := initOptionProfile()
	left.flowStates[pathStateKey{path: path, tombstone: true}] = struct{}{}
	left.slicePaths[path] = struct{}{}
	right.needStates[pathStateKey{path: path, tombstone: true}] = struct{}{}
	right.needPaths[path] = struct{}{}

	if profilesCollide([]optionVariant{{profile: left}}, []optionVariant{{profile: right}}, false) {
		t.Fatalf("profilesCollide(strict) = true, want false for matching tombstone flow/need state")
	}
}

func TestAssessOptionVariantCollisionReportsRAWAndWAWHazards(t *testing.T) {
	sharedSeed := normalizePath("/tmp/work/build/generated.h")
	sharedFlow := normalizePath("/tmp/work/build/libcore.a")
	left := optionVariant{
		profile: optionProfile{
			seedWrites: map[string]struct{}{sharedSeed: {}},
			slicePaths: map[string]struct{}{sharedFlow: {}},
			seedStates: map[pathStateKey]struct{}{{path: sharedSeed}: {}},
			flowStates: map[pathStateKey]struct{}{{path: sharedFlow}: {}},
		},
	}
	right := optionVariant{
		profile: optionProfile{
			seedWrites: map[string]struct{}{sharedSeed: {}},
			needPaths:  map[string]struct{}{sharedFlow: {}},
			seedStates: map[pathStateKey]struct{}{{path: sharedSeed}: {}},
			needStates: map[pathStateKey]struct{}{{path: sharedFlow}: {}},
		},
	}

	assessment := assessOptionVariantCollision(left, right, false)
	if !assessment.collide() {
		t.Fatalf("assessment.collide() = false, want true")
	}
	want := map[collisionHazardKind]struct{}{
		collisionHazardSeedWAW:              {},
		collisionHazardLeftFlowRightNeedRAW: {},
	}
	for _, hazard := range assessment.hazards {
		delete(want, hazard)
	}
	if len(want) != 0 {
		t.Fatalf("assessment.hazards missing %v, got %v", maps.Keys(want), assessment.hazards)
	}
}

func TestAnalyzeImpactTreatsDigestOnlyRerunAsAffectedSlice(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/build",
	}
	base := ProbeResult{
		Records: []trace.Record{
			record([]string{"cc", "-c", "server.c", "-o", "build/server.o"}, "/tmp/work",
				[]string{"/tmp/work/server.c", "/tmp/work/build/api.h"},
				[]string{"/tmp/work/build/server.o"}),
			record([]string{"cc", "-c", "utils.c", "-o", "build/utils.o"}, "/tmp/work",
				[]string{"/tmp/work/utils.c"},
				[]string{"/tmp/work/build/utils.o"}),
			record([]string{"cc", "build/server.o", "build/utils.o", "-o", "out/app.exe"}, "/tmp/work",
				[]string{"/tmp/work/build/server.o", "/tmp/work/build/utils.o"},
				[]string{"/tmp/work/out/app.exe"}),
		},
		Scope: scope,
		InputDigests: map[string]string{
			"/tmp/work/build/api.h":    "api-base",
			"/tmp/work/build/server.o": "server-base",
			"/tmp/work/build/utils.o":  "utils-base",
		},
	}
	probe := ProbeResult{
		Records: []trace.Record{
			record([]string{"protoc", "api.proto", "--c_out=build"}, "/tmp/work",
				[]string{"/tmp/work/api.proto"},
				[]string{"/tmp/work/build/api.c", "/tmp/work/build/api.h"}),
			record([]string{"cc", "-c", "server.c", "-o", "build/server.o"}, "/tmp/work",
				[]string{"/tmp/work/server.c", "/tmp/work/build/api.h"},
				[]string{"/tmp/work/build/server.o"}),
			record([]string{"cc", "-c", "utils.c", "-o", "build/utils.o"}, "/tmp/work",
				[]string{"/tmp/work/utils.c"},
				[]string{"/tmp/work/build/utils.o"}),
			record([]string{"cc", "-c", "build/api.c", "-o", "build/api.o"}, "/tmp/work",
				[]string{"/tmp/work/build/api.c"},
				[]string{"/tmp/work/build/api.o"}),
			record([]string{"cc", "build/server.o", "build/utils.o", "build/api.o", "-o", "out/app.exe"}, "/tmp/work",
				[]string{"/tmp/work/build/server.o", "/tmp/work/build/utils.o", "/tmp/work/build/api.o"},
				[]string{"/tmp/work/out/app.exe"}),
		},
		Scope: scope,
		InputDigests: map[string]string{
			"/tmp/work/build/api.h":    "api-new",
			"/tmp/work/build/api.c":    "api-c-new",
			"/tmp/work/build/server.o": "server-new",
			"/tmp/work/build/utils.o":  "utils-base",
			"/tmp/work/build/api.o":    "api-o-new",
		},
	}

	baseGraph := buildGraphForProbe(base)
	probeGraph := buildGraphForProbe(probe)
	evidence := buildImpactEvidence(base, probe)
	impact := analyzeImpactWithEvidence(baseGraph, probeGraph, evidence)
	if impact.profile.ambiguous {
		t.Fatalf("impact.profile.ambiguous = true, want false")
	}
	if len(impact.rootProbe) != 1 {
		t.Fatalf("rootProbe = %v, want exactly protoc root", impact.rootProbe)
	}
	if len(impact.affectedPairs) == 0 {
		t.Fatalf("affectedPairs = 0, want digest-only rerun pairing")
	}
	if _, ok := impact.profile.seedWrites[normalizeScopeToken("/tmp/work/build/server.o", scope)]; ok {
		t.Fatalf("server.o unexpectedly treated as seed write")
	}
	for _, path := range []string{
		"/tmp/work/build/api.h",
		"/tmp/work/build/api.c",
		"/tmp/work/build/api.o",
		"/tmp/work/build/server.o",
		"/tmp/work/out/app.exe",
	} {
		path = normalizeScopeToken(path, scope)
		if _, ok := impact.profile.slicePaths[path]; !ok {
			t.Fatalf("slicePaths missing %q", path)
		}
	}
	if _, ok := impact.profile.needPaths[normalizeScopeToken("/tmp/work/api.proto", scope)]; !ok {
		t.Fatalf("needPaths missing api.proto")
	}
	if _, ok := impact.profile.needStates[pathStateKey{
		path:      normalizeScopeToken("/tmp/work/api.proto", scope),
		tombstone: false,
	}]; !ok {
		t.Fatalf("needStates missing api.proto baseline state")
	}
	if _, ok := impact.profile.needPaths[normalizeScopeToken("/tmp/work/build/utils.o", scope)]; ok {
		t.Fatalf("needPaths unexpectedly includes downstream link input utils.o")
	}
}

func TestAnalyzeImpactLateRewriteDoesNotReachEarlierReader(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/build",
	}
	base := ProbeResult{
		Records: []trace.Record{
			record([]string{"gen-config", "config.in", "-o", "build/config.h"}, "/tmp/work",
				[]string{"/tmp/work/config.in"},
				[]string{"/tmp/work/build/config.h"}),
			record([]string{"cc", "-c", "main.c", "-o", "build/main.o"}, "/tmp/work",
				[]string{"/tmp/work/main.c", "/tmp/work/build/config.h"},
				[]string{"/tmp/work/build/main.o"}),
		},
		Scope: scope,
	}
	probe := ProbeResult{
		Records: []trace.Record{
			record([]string{"gen-config", "config.in", "-o", "build/config.h"}, "/tmp/work",
				[]string{"/tmp/work/config.in"},
				[]string{"/tmp/work/build/config.h"}),
			record([]string{"cc", "-c", "main.c", "-o", "build/main.o"}, "/tmp/work",
				[]string{"/tmp/work/main.c", "/tmp/work/build/config.h"},
				[]string{"/tmp/work/build/main.o"}),
			record([]string{"gen-probe-config", "probe.in", "-o", "build/config.h"}, "/tmp/work",
				[]string{"/tmp/work/probe.in"},
				[]string{"/tmp/work/build/config.h"}),
			record([]string{"cc", "-c", "test.c", "-o", "build/test.o"}, "/tmp/work",
				[]string{"/tmp/work/test.c", "/tmp/work/build/config.h"},
				[]string{"/tmp/work/build/test.o"}),
		},
		Scope: scope,
	}

	impact := analyzeImpact(buildGraphForProbe(base), buildGraphForProbe(probe))
	if impact.profile.ambiguous {
		t.Fatalf("impact.profile.ambiguous = true, want false")
	}
	probeGraph := buildGraphForProbe(probe)
	configPath := normalizeScopeToken("/tmp/work/build/config.h", scope)
	if _, ok := impact.profile.seedWrites[configPath]; !ok {
		t.Fatalf("seedWrites missing %q", configPath)
	}
	testObj := normalizeScopeToken("/tmp/work/build/test.o", scope)
	if _, ok := impact.profile.slicePaths[testObj]; !ok {
		t.Fatalf("slicePaths missing late reader output %q", testObj)
	}
	mainObj := normalizeScopeToken("/tmp/work/build/main.o", scope)
	if _, ok := impact.profile.slicePaths[mainObj]; ok {
		t.Fatalf("slicePaths unexpectedly includes earlier reader output %q", mainObj)
	}
	if len(impact.frontierProbe) != 1 {
		t.Fatalf("frontierProbe = %v, want exactly late test compile", impact.frontierProbe)
	}
	if got := probeGraph.actions[impact.frontierProbe[0]].writes; !slices.ContainsFunc(got, func(path string) bool {
		return strings.HasSuffix(path, "/test.o")
	}) {
		t.Fatalf("frontier action writes = %v, want test.o writer", got)
	}
}

func TestAnalyzeImpactDeletedSeedPropagatesThroughBaselineRead(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/build",
	}
	base := ProbeResult{
		Records: []trace.Record{
			record([]string{"gen-api", "api.idl", "-o", "build/api.h"}, "/tmp/work",
				[]string{"/tmp/work/api.idl"},
				[]string{"/tmp/work/build/api.h"}),
			record([]string{"cc", "-c", "server.c", "-o", "build/server.o"}, "/tmp/work",
				[]string{"/tmp/work/server.c", "/tmp/work/build/api.h"},
				[]string{"/tmp/work/build/server.o"}),
		},
		Scope: scope,
	}
	probe := ProbeResult{
		Records: []trace.Record{
			record([]string{"cc", "-c", "server.c", "-o", "build/server.o"}, "/tmp/work",
				[]string{"/tmp/work/server.c", "/tmp/work/build/api.h"},
				[]string{"/tmp/work/build/server.o"}),
		},
		Scope: scope,
	}

	impact := analyzeImpact(buildGraphForProbe(base), buildGraphForProbe(probe))
	if impact.profile.ambiguous {
		t.Fatalf("impact.profile.ambiguous = true, want false")
	}
	apiHeader := normalizeScopeToken("/tmp/work/build/api.h", scope)
	if _, ok := impact.profile.seedWrites[apiHeader]; !ok {
		t.Fatalf("seedWrites missing deleted path %q", apiHeader)
	}
	if _, ok := impact.profile.flowStates[pathStateKey{path: apiHeader, tombstone: true}]; !ok {
		t.Fatalf("flowStates missing tombstone state for deleted path %q", apiHeader)
	}
	serverObj := normalizeScopeToken("/tmp/work/build/server.o", scope)
	if _, ok := impact.profile.slicePaths[serverObj]; !ok {
		t.Fatalf("slicePaths missing reader output of deleted seed %q", serverObj)
	}
}

func TestAnalyzeImpactMarksAmbiguousEventReadForIncomparableWriters(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/build",
	}
	base := ProbeResult{
		Events: []trace.Event{
			{Seq: 1, PID: 10, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"cc", "-c", "/tmp/work/main.c", "-o", "/tmp/work/build/main.o"}},
			{Seq: 2, PID: 10, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/main.c"},
			{Seq: 3, PID: 10, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/config.h"},
			{Seq: 4, PID: 10, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/main.o"},
		},
		Scope: scope,
	}
	probe := ProbeResult{
		Events: []trace.Event{
			{Seq: 1, PID: 100, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"gen-a", "/tmp/work/build/config.h"}},
			{Seq: 2, PID: 100, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/config.h"},
			{Seq: 3, PID: 200, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"gen-b", "/tmp/work/build/config.h"}},
			{Seq: 4, PID: 200, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/config.h"},
			{Seq: 5, PID: 300, Cwd: "/tmp/work/build", Kind: trace.EventExec, Argv: []string{"cc", "-c", "/tmp/work/main.c", "-o", "/tmp/work/build/main.o"}},
			{Seq: 6, PID: 300, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/main.c"},
			{Seq: 7, PID: 300, Cwd: "/tmp/work/build", Kind: trace.EventRead, Path: "/tmp/work/build/config.h"},
			{Seq: 8, PID: 300, Cwd: "/tmp/work/build", Kind: trace.EventWrite, Path: "/tmp/work/build/main.o"},
		},
		Scope: scope,
	}

	impact := analyzeImpact(buildGraphForProbe(base), buildGraphForProbe(probe))
	if !impact.profile.ambiguous {
		t.Fatalf("impact.profile.ambiguous = false, want true for incomparable event-backed writers")
	}
}

func TestAnalyzeImpactFrontierKeepsOnlyFirstMixedConsumers(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/build",
	}
	base := ProbeResult{
		Records: []trace.Record{
			record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "build/libcombo.a", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o"},
				[]string{"/tmp/work/build/libcombo.a"}),
		},
		Scope: scope,
	}
	probe := ProbeResult{
		Records: []trace.Record{
			record([]string{"cc", "-DAPI", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "build/libcombo.a", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o"},
				[]string{"/tmp/work/build/libcombo.a"}),
			record([]string{"cc", "build/libcombo.a", "build/main.o", "-o", "build/app"}, "/tmp/work",
				[]string{"/tmp/work/build/libcombo.a", "/tmp/work/build/main.o"},
				[]string{"/tmp/work/build/app"}),
			record([]string{"pack", "build/app", "build/package.cfg", "-o", "build/app.bundle"}, "/tmp/work",
				[]string{"/tmp/work/build/app", "/tmp/work/build/package.cfg"},
				[]string{"/tmp/work/build/app.bundle"}),
		},
		Scope: scope,
	}

	probeGraph := buildGraphForProbe(probe)
	impact := analyzeImpact(buildGraphForProbe(base), probeGraph)
	if len(impact.flowProbe) != 4 {
		t.Fatalf("flowProbe = %v, want all four probe actions in flow", impact.flowProbe)
	}
	if len(impact.frontierProbe) != 1 {
		t.Fatalf("frontierProbe = %v, want only first mixed consumer", impact.frontierProbe)
	}
	if got := probeGraph.actions[impact.frontierProbe[0]].writes; !slices.ContainsFunc(got, func(path string) bool {
		return strings.HasSuffix(path, "/app")
	}) {
		t.Fatalf("frontier action writes = %v, want app writer", got)
	}
}

func TestAnalyzeImpactFrontierIgnoresAmbientPrerequisites(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/build",
	}
	base := ProbeResult{
		Records: []trace.Record{
			record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/core.c", "/usr/bin/cc"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "build/libcombo.a", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o", "/usr/bin/ar"},
				[]string{"/tmp/work/build/libcombo.a"}),
		},
		Scope: scope,
	}
	probe := ProbeResult{
		Records: []trace.Record{
			record([]string{"cc", "-DAPI", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/core.c", "/usr/bin/cc"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "build/libcombo.a", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o", "/usr/bin/ar"},
				[]string{"/tmp/work/build/libcombo.a"}),
		},
		Scope: scope,
	}

	impact := analyzeImpact(buildGraphForProbe(base), buildGraphForProbe(probe))
	if len(impact.frontierProbe) != 0 {
		t.Fatalf("frontierProbe = %v, want no frontier when only ambient prerequisites remain", impact.frontierProbe)
	}
	if _, ok := impact.profile.needPaths["/usr/bin/cc"]; ok {
		t.Fatalf("needPaths unexpectedly includes ambient compiler")
	}
	if _, ok := impact.profile.needPaths["/usr/bin/ar"]; ok {
		t.Fatalf("needPaths unexpectedly includes ambient archiver")
	}
	if _, ok := impact.profile.needStates[pathStateKey{path: "/usr/bin/cc"}]; ok {
		t.Fatalf("needStates unexpectedly includes ambient compiler state")
	}
	if _, ok := impact.profile.needStates[pathStateKey{path: "/usr/bin/ar"}]; ok {
		t.Fatalf("needStates unexpectedly includes ambient archiver state")
	}
}

func TestAnalyzeImpactAllowsToolingConfigureRootToSeedFlow(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/build",
	}
	base := ProbeResult{
		Records: []trace.Record{
			record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/build", "-DTRACE_FEATURE_API=OFF"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt", "/tmp/work/trace_options.h.in"},
				[]string{"/tmp/work/build/trace_options.h"}),
			record([]string{"/usr/bin/cc", "-c", "/tmp/work/core.c", "-o", "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/core.c", "/tmp/work/build/trace_options.h"},
				[]string{"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"}),
			record([]string{"/usr/bin/ar", "qc", "/tmp/work/build/libtracecore.a", "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/build/libtracecore.a", "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"},
				[]string{"/tmp/work/build/libtracecore.a"}),
		},
		Scope: scope,
		InputDigests: map[string]string{
			"/tmp/work/build/trace_options.h":                   "trace-off",
			"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o": "core-off",
			"/tmp/work/build/libtracecore.a":                    "archive-off",
		},
	}
	probe := ProbeResult{
		Records: []trace.Record{
			record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/build", "-DTRACE_FEATURE_API=ON"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt", "/tmp/work/trace_options.h.in"},
				[]string{"/tmp/work/build/trace_options.h"}),
			record([]string{"/usr/bin/cc", "-c", "/tmp/work/core.c", "-o", "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/core.c", "/tmp/work/build/trace_options.h"},
				[]string{"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"}),
			record([]string{"/usr/bin/ar", "qc", "/tmp/work/build/libtracecore.a", "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/build/libtracecore.a", "/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o"},
				[]string{"/tmp/work/build/libtracecore.a"}),
		},
		Scope: scope,
		InputDigests: map[string]string{
			"/tmp/work/build/trace_options.h":                   "trace-api",
			"/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o": "core-api",
			"/tmp/work/build/libtracecore.a":                    "archive-api",
		},
	}

	impact := analyzeImpactWithEvidence(buildGraphForProbe(base), buildGraphForProbe(probe), buildImpactEvidence(base, probe))
	if impact.profile.ambiguous {
		t.Fatalf("impact.profile.ambiguous = true, want false")
	}
	coreObj := normalizeScopeToken("/tmp/work/build/CMakeFiles/tracecore.dir/core.c.o", scope)
	if _, ok := impact.profile.slicePaths[coreObj]; !ok {
		t.Fatalf("slicePaths missing compile output %q from configure-root seed", coreObj)
	}
	lib := normalizeScopeToken("/tmp/work/build/libtracecore.a", scope)
	if _, ok := impact.profile.slicePaths[lib]; !ok {
		t.Fatalf("slicePaths missing archive output %q from configure-root seed", lib)
	}
	if len(impact.flowProbe) == 0 {
		t.Fatalf("flowProbe = 0, want configure-root propagation into mainline actions")
	}
}

func TestAnalyzeImpactUsesBuildRootOutputDigestsToStopNoopPropagation(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/build",
	}
	base := ProbeResult{
		Records: []trace.Record{
			record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/build", "-DEXPAT_GE=OFF"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt", "/tmp/work/expat_config.h.cmake"},
				[]string{"/tmp/work/build/expat_config.h"}),
			record([]string{"cc", "-c", "lib/xmlparse.c", "-o", "build/xmlparse.o"}, "/tmp/work",
				[]string{"/tmp/work/lib/xmlparse.c", "/tmp/work/build/expat_config.h"},
				[]string{"/tmp/work/build/xmlparse.o"}),
			record([]string{"ar", "rcs", "build/libexpat.a", "build/xmlparse.o"}, "/tmp/work",
				[]string{"/tmp/work/build/xmlparse.o"},
				[]string{"/tmp/work/build/libexpat.a"}),
		},
		Scope: scope,
		InputDigests: map[string]string{
			"/tmp/work/build/expat_config.h": "cfg-base",
			"/tmp/work/build/xmlparse.o":     "xmlparse-same",
			"/tmp/work/build/libexpat.a":     "libexpat-same",
		},
	}
	probe := ProbeResult{
		Records: []trace.Record{
			record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/build", "-DEXPAT_GE=ON"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt", "/tmp/work/expat_config.h.cmake"},
				[]string{"/tmp/work/build/expat_config.h"}),
			record([]string{"cc", "-c", "lib/xmlparse.c", "-o", "build/xmlparse.o"}, "/tmp/work",
				[]string{"/tmp/work/lib/xmlparse.c", "/tmp/work/build/expat_config.h"},
				[]string{"/tmp/work/build/xmlparse.o"}),
			record([]string{"ar", "rcs", "build/libexpat.a", "build/xmlparse.o"}, "/tmp/work",
				[]string{"/tmp/work/build/xmlparse.o"},
				[]string{"/tmp/work/build/libexpat.a"}),
		},
		Scope: scope,
		InputDigests: map[string]string{
			"/tmp/work/build/expat_config.h": "cfg-ge",
			"/tmp/work/build/xmlparse.o":     "xmlparse-same",
			"/tmp/work/build/libexpat.a":     "libexpat-same",
		},
	}

	baseGraph := buildGraphForProbe(base)
	probeGraph := buildGraphForProbe(probe)
	evidence := buildImpactEvidence(base, probe)
	impact := analyzeImpactWithEvidence(baseGraph, probeGraph, evidence)
	if impact.profile.ambiguous {
		t.Fatalf("impact.profile.ambiguous = true, want false")
	}
	configPath := normalizeScopeToken("/tmp/work/build/expat_config.h", scope)
	if _, ok := impact.profile.seedWrites[configPath]; !ok {
		t.Fatalf("seedWrites missing %q", configPath)
	}
	if _, ok := impact.profile.slicePaths[normalizeScopeToken("/tmp/work/build/xmlparse.o", scope)]; ok {
		t.Fatalf(
			"slicePaths unexpectedly includes xmlparse.o: roots=%v seed=%v need=%v slice=%v evidence=%v pathChanged(xmlparse)=%v pathChanged(libexpat)=%v",
			impact.rootProbe,
			slices.Sorted(maps.Keys(impact.profile.seedWrites)),
			slices.Sorted(maps.Keys(impact.profile.needPaths)),
			slices.Sorted(maps.Keys(impact.profile.slicePaths)),
			evidence.changed,
			pathChanged(evidence, probeGraph, "/tmp/work/build/xmlparse.o"),
			pathChanged(evidence, probeGraph, "/tmp/work/build/libexpat.a"),
		)
	}
	if _, ok := impact.profile.slicePaths[normalizeScopeToken("/tmp/work/build/libexpat.a", scope)]; ok {
		t.Fatalf("slicePaths unexpectedly includes libexpat.a")
	}
}

func TestAnalyzeImpactIgnoresCMakeTryCompileNoisePaths(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/build",
	}
	base := ProbeResult{
		Records: []trace.Record{
			record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/build"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt", "/tmp/work/expat/lib/expat_config.h.cmake"},
				[]string{"/tmp/work/build/expat_config.h"}),
			record([]string{"cc", "-c", "/tmp/work/expat/lib/xmlparse.c", "-o", "/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/expat/lib/xmlparse.c", "/tmp/work/build/expat_config.h"},
				[]string{"/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}),
			record([]string{"ar", "rcs", "/tmp/work/build/libexpat.a", "/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"},
				[]string{"/tmp/work/build/libexpat.a"}),
		},
		Scope: scope,
	}
	probe := ProbeResult{
		Records: []trace.Record{
			record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/build", "-DEXPAT_GE=ON"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt", "/tmp/work/expat/lib/expat_config.h.cmake"},
				[]string{
					"/tmp/work/build/expat_config.h",
					"/tmp/work/build/CMakeFiles/CMakeScratch/TryCompile-doc/CheckIncludeFile.c.o",
					"/tmp/work/build/CMakeFiles/CMakeScratch/TryCompile-doc/cmTC_doc",
				}),
			record([]string{"cc", "-c", "/tmp/work/expat/lib/xmlparse.c", "-o", "/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/expat/lib/xmlparse.c", "/tmp/work/build/expat_config.h"},
				[]string{"/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}),
			record([]string{"ar", "rcs", "/tmp/work/build/libexpat.a", "/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"},
				[]string{"/tmp/work/build/libexpat.a"}),
		},
		Scope: scope,
	}

	impact := analyzeImpact(buildGraphForProbe(base), buildGraphForProbe(probe))
	for _, noisePath := range []string{
		"/tmp/work/build/CMakeFiles/CMakeScratch/TryCompile-doc/CheckIncludeFile.c.o",
		"/tmp/work/build/CMakeFiles/CMakeScratch/TryCompile-doc/cmTC_doc",
	} {
		key := normalizeScopeToken(noisePath, scope)
		if _, ok := impact.profile.seedWrites[key]; ok {
			t.Fatalf("seedWrites unexpectedly includes try-compile noise %q", key)
		}
		if _, ok := impact.profile.slicePaths[key]; ok {
			t.Fatalf("slicePaths unexpectedly includes try-compile noise %q", key)
		}
		if _, ok := impact.profile.needPaths[key]; ok {
			t.Fatalf("needPaths unexpectedly includes try-compile noise %q", key)
		}
	}
	if _, ok := impact.profile.seedWrites[normalizeScopeToken("/tmp/work/build/expat_config.h", scope)]; !ok {
		t.Fatalf("seedWrites missing expat_config.h")
	}
}

func TestAnalyzeImpactIgnoresProbeOnlyNoisePathsWithoutFixedNames(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/build",
	}
	base := ProbeResult{
		Records: []trace.Record{
			recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/build"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt", "/tmp/work/expat/lib/expat_config.h.cmake"},
				[]string{"/tmp/work/build/expat_config.h"}),
			recordWithProc(200, 2, []string{"cc", "-c", "/tmp/work/expat/lib/xmlparse.c", "-o", "/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/expat/lib/xmlparse.c", "/tmp/work/build/expat_config.h"},
				[]string{"/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}),
			recordWithProc(201, 2, []string{"ar", "rcs", "/tmp/work/build/libexpat.a", "/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"},
				[]string{"/tmp/work/build/libexpat.a"}),
		},
		Scope: scope,
	}
	probe := ProbeResult{
		Records: []trace.Record{
			recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/build", "-DEXPAT_GE=ON"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt", "/tmp/work/expat/lib/expat_config.h.cmake"},
				[]string{
					"/tmp/work/build/expat_config.h",
					"/tmp/work/build/probe-checks/CheckFeature.c",
				}),
			recordWithProc(101, 100, []string{"cc", "-c", "CheckFeature.c", "-o", "CheckFeature.c.o"}, "/tmp/work/build/probe-checks",
				[]string{"/tmp/work/build/probe-checks/CheckFeature.c"},
				[]string{"/tmp/work/build/probe-checks/CheckFeature.c.o"}),
			recordWithProc(102, 100, []string{"cc", "CheckFeature.c.o", "-o", "probe-check"}, "/tmp/work/build/probe-checks",
				[]string{"/tmp/work/build/probe-checks/CheckFeature.c.o"},
				[]string{"/tmp/work/build/probe-checks/probe-check"}),
			recordWithProc(200, 2, []string{"cc", "-c", "/tmp/work/expat/lib/xmlparse.c", "-o", "/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/expat/lib/xmlparse.c", "/tmp/work/build/expat_config.h"},
				[]string{"/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}),
			recordWithProc(201, 2, []string{"ar", "rcs", "/tmp/work/build/libexpat.a", "/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"},
				[]string{"/tmp/work/build/libexpat.a"}),
		},
		Scope: scope,
	}

	impact := analyzeImpact(buildGraphForProbe(base), buildGraphForProbe(probe))
	for _, noisePath := range []string{
		"/tmp/work/build/probe-checks/CheckFeature.c",
		"/tmp/work/build/probe-checks/CheckFeature.c.o",
		"/tmp/work/build/probe-checks/probe-check",
	} {
		key := normalizeScopeToken(noisePath, scope)
		if _, ok := impact.profile.seedWrites[key]; ok {
			t.Fatalf("seedWrites unexpectedly includes probe-only noise %q", key)
		}
		if _, ok := impact.profile.seedStates[pathStateKey{path: key}]; ok {
			t.Fatalf("seedStates unexpectedly includes probe-only noise %q", key)
		}
		if _, ok := impact.profile.slicePaths[key]; ok {
			t.Fatalf("slicePaths unexpectedly includes probe-only noise %q", key)
		}
		if _, ok := impact.profile.flowStates[pathStateKey{path: key}]; ok {
			t.Fatalf("flowStates unexpectedly includes probe-only noise %q", key)
		}
		if _, ok := impact.profile.needPaths[key]; ok {
			t.Fatalf("needPaths unexpectedly includes probe-only noise %q", key)
		}
		if _, ok := impact.profile.needStates[pathStateKey{path: key}]; ok {
			t.Fatalf("needStates unexpectedly includes probe-only noise %q", key)
		}
	}
	if _, ok := impact.profile.seedWrites[normalizeScopeToken("/tmp/work/build/expat_config.h", scope)]; !ok {
		t.Fatalf("seedWrites missing expat_config.h")
	}
}

func TestAnalyzeImpactIgnoresProbeOnlyNoisePathsWrappedByGenericMake(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/tmp/work",
		BuildRoot:  "/tmp/work/build",
	}
	base := ProbeResult{
		Records: []trace.Record{
			recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/build"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt", "/tmp/work/expat/lib/expat_config.h.cmake"},
				[]string{"/tmp/work/build/expat_config.h"}),
			recordWithProc(200, 2, []string{"cc", "-c", "/tmp/work/expat/lib/xmlparse.c", "-o", "/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/expat/lib/xmlparse.c", "/tmp/work/build/expat_config.h"},
				[]string{"/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}),
			recordWithProc(201, 2, []string{"ar", "rcs", "/tmp/work/build/libexpat.a", "/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"},
				[]string{"/tmp/work/build/libexpat.a"}),
		},
		Scope: scope,
	}
	probe := ProbeResult{
		Records: []trace.Record{
			recordWithProc(100, 1, []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/build", "-DEXPAT_GE=ON"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt", "/tmp/work/expat/lib/expat_config.h.cmake"},
				[]string{
					"/tmp/work/build/expat_config.h",
					"/tmp/work/build/probe-checks/CheckFeature.c",
					"/tmp/work/build/probe-checks/Makefile",
				}),
			recordWithProc(101, 100, []string{"gmake", "-f", "Makefile"}, "/tmp/work/build/probe-checks",
				[]string{"/tmp/work/build/probe-checks/Makefile"},
				nil),
			recordWithProc(102, 101, []string{"cc", "-c", "CheckFeature.c", "-o", "CheckFeature.c.o"}, "/tmp/work/build/probe-checks",
				[]string{"/tmp/work/build/probe-checks/CheckFeature.c"},
				[]string{"/tmp/work/build/probe-checks/CheckFeature.c.o"}),
			recordWithProc(103, 101, []string{"cc", "CheckFeature.c.o", "-o", "probe-check"}, "/tmp/work/build/probe-checks",
				[]string{"/tmp/work/build/probe-checks/CheckFeature.c.o"},
				[]string{"/tmp/work/build/probe-checks/probe-check"}),
			recordWithProc(200, 2, []string{"cc", "-c", "/tmp/work/expat/lib/xmlparse.c", "-o", "/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/expat/lib/xmlparse.c", "/tmp/work/build/expat_config.h"},
				[]string{"/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}),
			recordWithProc(201, 2, []string{"ar", "rcs", "/tmp/work/build/libexpat.a", "/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"}, "/tmp/work/build",
				[]string{"/tmp/work/build/CMakeFiles/expat.dir/lib/xmlparse.c.o"},
				[]string{"/tmp/work/build/libexpat.a"}),
		},
		Scope: scope,
	}

	impact := analyzeImpact(buildGraphForProbe(base), buildGraphForProbe(probe))
	for _, noisePath := range []string{
		"/tmp/work/build/probe-checks/CheckFeature.c",
		"/tmp/work/build/probe-checks/Makefile",
		"/tmp/work/build/probe-checks/CheckFeature.c.o",
		"/tmp/work/build/probe-checks/probe-check",
	} {
		key := normalizeScopeToken(noisePath, scope)
		if _, ok := impact.profile.seedWrites[key]; ok {
			t.Fatalf("seedWrites unexpectedly includes wrapped probe noise %q", key)
		}
		if _, ok := impact.profile.slicePaths[key]; ok {
			t.Fatalf("slicePaths unexpectedly includes wrapped probe noise %q", key)
		}
		if _, ok := impact.profile.needPaths[key]; ok {
			t.Fatalf("needPaths unexpectedly includes wrapped probe noise %q", key)
		}
	}
	if _, ok := impact.profile.seedWrites[normalizeScopeToken("/tmp/work/build/expat_config.h", scope)]; !ok {
		t.Fatalf("seedWrites missing expat_config.h")
	}
}

func testMatrix() formula.Matrix {
	return formula.Matrix{
		Require: map[string][]string{
			"arch": {"amd64"},
			"os":   {"linux"},
		},
		Options: map[string][]string{
			"doc": {"doc-off", "doc-on"},
			"tls": {"tls-off", "tls-on"},
		},
		DefaultOptions: map[string][]string{
			"doc": {"doc-off"},
			"tls": {"tls-off"},
		},
	}
}

func record(argv []string, cwd string, inputs, changes []string) trace.Record {
	return trace.Record{
		Argv:    argv,
		Cwd:     cwd,
		Inputs:  inputs,
		Changes: changes,
	}
}

func outputManifest(path string, entry OutputEntry) OutputManifest {
	return OutputManifest{
		Entries: map[string]OutputEntry{
			path: entry,
		},
	}
}
