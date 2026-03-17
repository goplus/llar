package evaluator

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
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
		"amd64-linux|doc-on-tls-on",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestIsTrustedGraphRejectsBusinessCodegen(t *testing.T) {
	graph := actionGraph{
		actions: []actionNode{
			{kind: kindCodegen, writes: []string{"/build/generated.c"}},
			{kind: kindCompile, reads: []string{"/build/generated.c"}, writes: []string{"/build/generated.o"}},
			{kind: kindArchive, reads: []string{"/build/generated.o"}, writes: []string{"/build/libfoo.a"}},
			{kind: kindInstall, reads: []string{"/build/libfoo.a"}, writes: []string{"/install/lib/libfoo.a"}},
		},
		business: []bool{true, true, true, true},
		paths: map[string]pathFacts{
			"/build/generated.c": {
				path:    "/build/generated.c",
				role:    rolePropagating,
				writers: []int{0},
				readers: []int{1},
			},
			"/build/generated.o": {
				path:    "/build/generated.o",
				role:    rolePropagating,
				writers: []int{1},
				readers: []int{2},
			},
			"/build/libfoo.a": {
				path:    "/build/libfoo.a",
				role:    roleDelivery,
				writers: []int{2},
				readers: []int{3},
			},
			"/install/lib/libfoo.a": {
				path:    "/install/lib/libfoo.a",
				role:    roleDelivery,
				writers: []int{3},
			},
		},
	}

	if isTrustedGraph(graph) {
		t.Fatalf("isTrustedGraph(codegen) = true, want false")
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
			record([]string{"cc", "-c", "core.c"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-on-simd-off-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-off-simd-on-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
			record([]string{"python", "gen_simd.py"}, "/tmp/work",
				[]string{"/tmp/work/gen_simd.py"},
				[]string{"/tmp/work/out/bin/simd-helper"}),
		},
		"amd64-linux|doc-off-simd-off-tls-on": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
			record([]string{"python", "gen_tls.py"}, "/tmp/work",
				[]string{"/tmp/work/gen_tls.py"},
				[]string{"/tmp/work/out/bin/tls-helper"}),
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
		"amd64-linux|doc-on-simd-off-tls-on",
		"amd64-linux|doc-on-simd-on-tls-off",
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
			record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"cc", "-c", "extra.c", "-o", "build/extra.o"}, "/tmp/work",
				[]string{"/tmp/work/extra.c"},
				[]string{"/tmp/work/build/extra.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o", "build/extra.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o", "/tmp/work/build/extra.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"level-medium-net-off-simd-off": {
			record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"cc", "-c", "extra.c", "-o", "build/extra.o"}, "/tmp/work",
				[]string{"/tmp/work/extra.c"},
				[]string{"/tmp/work/build/extra.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o", "build/extra.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o", "/tmp/work/build/extra.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"level-strong-net-off-simd-off": {
			record([]string{"cc", "-DLEVEL_STRONG", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"cc", "-DLEVEL_STRONG", "-c", "extra.c", "-o", "build/extra.o"}, "/tmp/work",
				[]string{"/tmp/work/extra.c"},
				[]string{"/tmp/work/build/extra.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o", "build/extra.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o", "/tmp/work/build/extra.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"level-off-net-on-simd-off": {
			record([]string{"cc", "-DNET", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"cc", "-c", "extra.c", "-o", "build/extra.o"}, "/tmp/work",
				[]string{"/tmp/work/extra.c"},
				[]string{"/tmp/work/build/extra.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o", "build/extra.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o", "/tmp/work/build/extra.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"level-off-net-off-simd-on": {
			record([]string{"cc", "-c", "core.c", "-o", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"cc", "-DSIMD", "-c", "extra.c", "-o", "build/extra.o"}, "/tmp/work",
				[]string{"/tmp/work/extra.c"},
				[]string{"/tmp/work/build/extra.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o", "build/extra.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o", "/tmp/work/build/extra.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
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

func TestWatchReturnsUntrustedForBusinessGenericAction(t *testing.T) {
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
				record([]string{"genblob", "build"}, "/tmp/work",
					[]string{"/tmp/work/schema.dsl"},
					[]string{"/tmp/work/_build/schema.bin"}),
				record([]string{"cc", "-c", "/tmp/work/core.c", "-o", "/tmp/work/_build/core.o"}, "/tmp/work",
					[]string{"/tmp/work/core.c", "/tmp/work/_build/schema.bin"},
					[]string{"/tmp/work/_build/core.o"}),
				record([]string{"ar", "rcs", "/tmp/work/_build/libcore.a", "/tmp/work/_build/core.o"}, "/tmp/work",
					[]string{"/tmp/work/_build/core.o"},
					[]string{"/tmp/work/_build/libcore.a"}),
				record([]string{"cp", "/tmp/work/_build/schema.bin", "/tmp/work/install/share/schema.bin"}, "/tmp/work",
					[]string{"/tmp/work/_build/schema.bin"},
					[]string{"/tmp/work/install/share/schema.bin"}),
				record([]string{"cp", "/tmp/work/_build/libcore.a", "/tmp/work/install/lib/libcore.a"}, "/tmp/work",
					[]string{"/tmp/work/_build/libcore.a"},
					[]string{"/tmp/work/install/lib/libcore.a"}),
			},
			Scope: scope,
		},
		"feat-on": {
			Records: []trace.Record{
				record([]string{"genblob", "build", "--feature"}, "/tmp/work",
					[]string{"/tmp/work/schema.dsl"},
					[]string{"/tmp/work/_build/schema.bin"}),
				record([]string{"cc", "-c", "/tmp/work/core.c", "-o", "/tmp/work/_build/core.o"}, "/tmp/work",
					[]string{"/tmp/work/core.c", "/tmp/work/_build/schema.bin"},
					[]string{"/tmp/work/_build/core.o"}),
				record([]string{"ar", "rcs", "/tmp/work/_build/libcore.a", "/tmp/work/_build/core.o"}, "/tmp/work",
					[]string{"/tmp/work/_build/core.o"},
					[]string{"/tmp/work/_build/libcore.a"}),
				record([]string{"cp", "/tmp/work/_build/schema.bin", "/tmp/work/install/share/schema.bin"}, "/tmp/work",
					[]string{"/tmp/work/_build/schema.bin"},
					[]string{"/tmp/work/install/share/schema.bin"}),
				record([]string{"cp", "/tmp/work/_build/libcore.a", "/tmp/work/install/lib/libcore.a"}, "/tmp/work",
					[]string{"/tmp/work/_build/libcore.a"},
					[]string{"/tmp/work/install/lib/libcore.a"}),
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
	if trusted {
		t.Fatalf("Watch() trusted = true, want false")
	}
}

func TestWatchReturnsUntrustedForTraceDiagnostics(t *testing.T) {
	matrix := formula.Matrix{
		Options: map[string][]string{
			"feat": {"feat-off", "feat-on"},
		},
		DefaultOptions: map[string][]string{
			"feat": {"feat-off"},
		},
	}
	probe := ProbeResult{
		Records: []trace.Record{
			record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
		},
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
		"amd64-linux|doc-off-tls-off": {
			OutputManifest: manifest,
		},
		"amd64-linux|doc-on-tls-off": {
			Records: []trace.Record{
				record([]string{"python", "gen_doc_asset.py"}, "/tmp/work", []string{"/tmp/work/doc.in"}, []string{"/tmp/work/out/share/generated.txt"}),
			},
			OutputManifest: manifest,
		},
		"amd64-linux|doc-off-tls-on": {
			Records: []trace.Record{
				record([]string{"perl", "gen_tls_asset.pl"}, "/tmp/work", []string{"/tmp/work/tls.in"}, []string{"/tmp/work/out/share/generated.txt"}),
			},
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

func TestWatchCollidingProducerConsumerOptions(t *testing.T) {
	matrix := testMatrix()
	manifest := outputManifest("lib/libfoo.a", OutputEntry{Kind: "archive", Digest: "same"})
	traces := map[string]ProbeResult{
		"amd64-linux|doc-off-tls-off": {
			Records: []trace.Record{
				record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
				record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
			},
			OutputManifest: manifest,
		},
		"amd64-linux|doc-on-tls-off": {
			Records: []trace.Record{
				record([]string{"cc", "-DDOC", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
				record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
			},
			OutputManifest: manifest,
		},
		"amd64-linux|doc-off-tls-on": {
			Records: []trace.Record{
				record([]string{"cc", "-c", "core.c"}, "/tmp/work", []string{"/tmp/work/core.c"}, []string{"/tmp/work/build/core.o"}),
				record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work", []string{"/tmp/work/build/core.o"}, []string{"/tmp/work/out/lib/libfoo.a"}),
				record([]string{"cc", "-c", "cli.c"}, "/tmp/work", []string{"/tmp/work/cli.c"}, []string{"/tmp/work/build/cli.o"}),
				record([]string{"cc", "build/cli.o", "out/lib/libfoo.a", "-o", "out/bin/foo"}, "/tmp/work", []string{"/tmp/work/build/cli.o", "/tmp/work/out/lib/libfoo.a"}, []string{"/tmp/work/out/bin/foo"}),
			},
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
		"amd64-linux|doc-on-tls-on",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchIgnoresToolingOnlyOptions(t *testing.T) {
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

func TestWatchIgnoresSharedToolingParamTouches(t *testing.T) {
	matrix := testMatrix()
	traces := map[string][]trace.Record{
		"amd64-linux|doc-off-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-on-tls-off": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
			record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeTmp/TryCompile-12345/CheckIncludeFile.c"}),
			record([]string{"cc", "-c", "CheckIncludeFile.c", "-o", "CheckIncludeFile.c.o"}, "/tmp/work/_build/CMakeFiles/CMakeTmp/TryCompile-12345",
				[]string{"/tmp/work/_build/CMakeFiles/CMakeTmp/TryCompile-12345/CheckIncludeFile.c"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeTmp/TryCompile-12345/CheckIncludeFile.c.o"}),
			record([]string{"ld", "-o", "cmTC_a1b2c3", "CheckIncludeFile.c.o"}, "/tmp/work/_build/CMakeFiles/CMakeTmp",
				[]string{"/tmp/work/_build/CMakeFiles/CMakeTmp/TryCompile-12345/CheckIncludeFile.c.o"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeTmp/cmTC_a1b2c3"}),
		},
		"amd64-linux|doc-off-tls-on": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "libfoo.a", "core.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
			record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeTmp/TryCompile-67890/OFF64_T.c"}),
			record([]string{"cc", "-c", "OFF64_T.c", "-o", "OFF64_T.c.o"}, "/tmp/work/_build/CMakeFiles/CMakeTmp/TryCompile-67890",
				[]string{"/tmp/work/_build/CMakeFiles/CMakeTmp/TryCompile-67890/OFF64_T.c"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeTmp/TryCompile-67890/OFF64_T.c.o"}),
			record([]string{"ld", "-o", "cmTC_d4e5f6", "OFF64_T.c.o"}, "/tmp/work/_build/CMakeFiles/CMakeTmp",
				[]string{"/tmp/work/_build/CMakeFiles/CMakeTmp/TryCompile-67890/OFF64_T.c.o"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeTmp/cmTC_d4e5f6"}),
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
			record([]string{"cc", "-c", "core.c"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-on-tls-off": {
			record([]string{"cc", "-DDOC", "-c", "core.c"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
		},
		"amd64-linux|doc-off-tls-on": {
			record([]string{"cc", "-c", "core.c"}, "/tmp/work",
				[]string{"/tmp/work/core.c"},
				[]string{"/tmp/work/build/core.o"}),
			record([]string{"ar", "rcs", "out/lib/libfoo.a", "build/core.o"}, "/tmp/work",
				[]string{"/tmp/work/build/core.o"},
				[]string{"/tmp/work/out/lib/libfoo.a"}),
			record([]string{"custom-installer", "install"}, "/tmp/work",
				[]string{"/tmp/work/out/lib/libfoo.a", "/tmp/work/build/core.o"},
				[]string{"/tmp/work/install/include/boost/foo.hpp", "/tmp/work/install/lib/libfoo.a"}),
		},
	}

	got, _, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return ProbeResult{
			Records: traces[combo],
			Scope:   trace.Scope{InstallRoot: "/tmp/work/install"},
		}, nil
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

func TestAddProfilePathKeepsUnknownSeparateButColliding(t *testing.T) {
	path := normalizePath("/tmp/work/_build/checks/libarch.a")
	left := optionProfile{
		propagatingReads:  make(map[string]struct{}),
		propagatingWrites: make(map[string]struct{}),
		unknownReads:      make(map[string]struct{}),
		unknownWrites:     make(map[string]struct{}),
		deliveryWrites:    make(map[string]struct{}),
		toolingReads:      make(map[string]struct{}),
		toolingWrites:     make(map[string]struct{}),
		paramTouches:      make(map[string]struct{}),
	}
	right := optionProfile{
		propagatingReads:  make(map[string]struct{}),
		propagatingWrites: make(map[string]struct{}),
		unknownReads:      make(map[string]struct{}),
		unknownWrites:     make(map[string]struct{}),
		deliveryWrites:    make(map[string]struct{}),
		toolingReads:      make(map[string]struct{}),
		toolingWrites:     make(map[string]struct{}),
		paramTouches:      make(map[string]struct{}),
	}

	addProfilePath(&left, roleUnknown, path, true)
	addProfilePath(&right, rolePropagating, path, false)

	if _, ok := left.propagatingWrites[path]; ok {
		t.Fatalf("unknown write leaked into propagatingWrites")
	}
	if _, ok := left.unknownWrites[path]; !ok {
		t.Fatalf("unknown write missing from unknownWrites")
	}
	if !profilesCollide([]optionVariant{{profile: left}}, []optionVariant{{profile: right}}) {
		t.Fatalf("profilesCollide() = false, want true")
	}
}

func TestDiffProfileKeepsLinkReadsForStagedDeliveryOutput(t *testing.T) {
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	baseRecords := []trace.Record{
		record([]string{"ar", "rcs", "libtracecore.a", "core.o"}, "/tmp/work/_build",
			[]string{"/tmp/work/_build/core.o"},
			[]string{"/tmp/work/_build/libtracecore.a"}),
		record([]string{"cmake", "--install", "/tmp/work/_build", "--prefix", "/tmp/work/install"}, "/tmp/work",
			[]string{"/tmp/work/_build/libtracecore.a"},
			[]string{"/tmp/work/install/lib/libtracecore.a"}),
	}
	probeRecords := append(slices.Clone(baseRecords),
		record([]string{"cc", "-c", "/tmp/work/cli.c", "-o", "/tmp/work/_build/cli.o"}, "/tmp/work",
			[]string{"/tmp/work/cli.c"},
			[]string{"/tmp/work/_build/cli.o"}),
		record([]string{"cc", "/tmp/work/_build/cli.o", "/tmp/work/_build/libtracecore.a", "-o", "/tmp/work/_build/tracecli"}, "/tmp/work/_build",
			[]string{"/tmp/work/_build/cli.o", "/tmp/work/_build/libtracecore.a"},
			[]string{"/tmp/work/_build/tracecli"}),
		record([]string{"cmake", "--install", "/tmp/work/_build", "--prefix", "/tmp/work/install"}, "/tmp/work",
			[]string{"/tmp/work/_build/libtracecore.a", "/tmp/work/_build/tracecli"},
			[]string{"/tmp/work/install/lib/libtracecore.a", "/tmp/work/install/bin/tracecli"}),
	)

	baseGraph := buildGraphWithScope(baseRecords, scope)
	probeGraph := buildGraphWithScope(probeRecords, scope)
	profile := diffProfile(baseGraph, probeGraph)

	path := normalizePath("/tmp/work/_build/libtracecore.a")
	if _, ok := profile.propagatingReads[path]; !ok {
		t.Fatalf("propagatingReads missing staged link input %q: got %v", path, maps.Keys(profile.propagatingReads))
	}
}

func TestWatchCollidesOnSameCompileActionParameterDelta(t *testing.T) {
	matrix := formula.Matrix{
		Options: map[string][]string{
			"dbstat":  {"dbstat-off", "dbstat-on"},
			"json1":   {"json1-off", "json1-on"},
			"rtree":   {"rtree-off", "rtree-on"},
			"soundex": {"soundex-off", "soundex-on"},
		},
		DefaultOptions: map[string][]string{
			"dbstat":  {"dbstat-off"},
			"json1":   {"json1-off"},
			"rtree":   {"rtree-off"},
			"soundex": {"soundex-off"},
		},
	}
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	traces := map[string][]trace.Record{
		"dbstat-off-json1-off-rtree-off-soundex-off": {
			record([]string{"sh", "-c", "cc -O2 -DSQLITE_THREADSAFE=1 -c /tmp/work/sqlite3.c -o /tmp/work/_build/sqlite3.o"}, "/tmp/work", nil, nil),
			record([]string{"cc1", "-O2", "-DSQLITE_THREADSAFE=1", "/tmp/work/sqlite3.c"}, "/tmp/work",
				[]string{"/tmp/work/sqlite3.c"},
				nil),
			record([]string{"as", "-o", "/tmp/work/_build/sqlite3.o", "/tmp/cc-base.s"}, "/tmp/work",
				nil,
				[]string{"/tmp/work/_build/sqlite3.o"}),
			record([]string{"ar", "rcs", "/tmp/work/_build/libsqlite3.a", "/tmp/work/_build/sqlite3.o"}, "/tmp/work",
				[]string{"/tmp/work/_build/sqlite3.o"},
				[]string{"/tmp/work/_build/libsqlite3.a", "/tmp/work/_build/stBase123"}),
			record([]string{"cp", "/tmp/work/_build/libsqlite3.a", "/tmp/work/install/lib/libsqlite3.a"}, "/tmp/work",
				[]string{"/tmp/work/_build/libsqlite3.a"},
				[]string{"/tmp/work/install/lib/libsqlite3.a"}),
		},
		"dbstat-on-json1-off-rtree-off-soundex-off": {
			record([]string{"sh", "-c", "cc -O2 -DSQLITE_THREADSAFE=1 -DSQLITE_ENABLE_DBSTAT_VTAB -c /tmp/work/sqlite3.c -o /tmp/work/_build/sqlite3.o"}, "/tmp/work", nil, nil),
			record([]string{"cc1", "-O2", "-DSQLITE_THREADSAFE=1", "-DSQLITE_ENABLE_DBSTAT_VTAB", "/tmp/work/sqlite3.c"}, "/tmp/work",
				[]string{"/tmp/work/sqlite3.c"},
				nil),
			record([]string{"as", "-o", "/tmp/work/_build/sqlite3.o", "/tmp/cc-dbstat.s"}, "/tmp/work",
				nil,
				[]string{"/tmp/work/_build/sqlite3.o"}),
			record([]string{"ar", "rcs", "/tmp/work/_build/libsqlite3.a", "/tmp/work/_build/sqlite3.o"}, "/tmp/work",
				[]string{"/tmp/work/_build/sqlite3.o"},
				[]string{"/tmp/work/_build/libsqlite3.a", "/tmp/work/_build/stDb12345"}),
			record([]string{"cp", "/tmp/work/_build/libsqlite3.a", "/tmp/work/install/lib/libsqlite3.a"}, "/tmp/work",
				[]string{"/tmp/work/_build/libsqlite3.a"},
				[]string{"/tmp/work/install/lib/libsqlite3.a"}),
		},
		"dbstat-off-json1-on-rtree-off-soundex-off": {
			record([]string{"sh", "-c", "cc -O2 -DSQLITE_THREADSAFE=1 -DSQLITE_ENABLE_JSON1 -c /tmp/work/sqlite3.c -o /tmp/work/_build/sqlite3.o"}, "/tmp/work", nil, nil),
			record([]string{"cc1", "-O2", "-DSQLITE_THREADSAFE=1", "-DSQLITE_ENABLE_JSON1", "/tmp/work/sqlite3.c"}, "/tmp/work",
				[]string{"/tmp/work/sqlite3.c"},
				nil),
			record([]string{"as", "-o", "/tmp/work/_build/sqlite3.o", "/tmp/cc-json.s"}, "/tmp/work",
				nil,
				[]string{"/tmp/work/_build/sqlite3.o"}),
			record([]string{"ar", "rcs", "/tmp/work/_build/libsqlite3.a", "/tmp/work/_build/sqlite3.o"}, "/tmp/work",
				[]string{"/tmp/work/_build/sqlite3.o"},
				[]string{"/tmp/work/_build/libsqlite3.a", "/tmp/work/_build/stJson999"}),
			record([]string{"cp", "/tmp/work/_build/libsqlite3.a", "/tmp/work/install/lib/libsqlite3.a"}, "/tmp/work",
				[]string{"/tmp/work/_build/libsqlite3.a"},
				[]string{"/tmp/work/install/lib/libsqlite3.a"}),
		},
		"dbstat-off-json1-off-rtree-on-soundex-off": {
			record([]string{"sh", "-c", "cc -O2 -DSQLITE_THREADSAFE=1 -DSQLITE_ENABLE_RTREE -c /tmp/work/sqlite3.c -o /tmp/work/_build/sqlite3.o"}, "/tmp/work", nil, nil),
			record([]string{"cc1", "-O2", "-DSQLITE_THREADSAFE=1", "-DSQLITE_ENABLE_RTREE", "/tmp/work/sqlite3.c"}, "/tmp/work",
				[]string{"/tmp/work/sqlite3.c"},
				nil),
			record([]string{"as", "-o", "/tmp/work/_build/sqlite3.o", "/tmp/cc-rtree.s"}, "/tmp/work",
				nil,
				[]string{"/tmp/work/_build/sqlite3.o"}),
			record([]string{"ar", "rcs", "/tmp/work/_build/libsqlite3.a", "/tmp/work/_build/sqlite3.o"}, "/tmp/work",
				[]string{"/tmp/work/_build/sqlite3.o"},
				[]string{"/tmp/work/_build/libsqlite3.a", "/tmp/work/_build/stRtree1"}),
			record([]string{"cp", "/tmp/work/_build/libsqlite3.a", "/tmp/work/install/lib/libsqlite3.a"}, "/tmp/work",
				[]string{"/tmp/work/_build/libsqlite3.a"},
				[]string{"/tmp/work/install/lib/libsqlite3.a"}),
		},
		"dbstat-off-json1-off-rtree-off-soundex-on": {
			record([]string{"sh", "-c", "cc -O2 -DSQLITE_THREADSAFE=1 -DSQLITE_SOUNDEX -c /tmp/work/sqlite3.c -o /tmp/work/_build/sqlite3.o"}, "/tmp/work", nil, nil),
			record([]string{"cc1", "-O2", "-DSQLITE_THREADSAFE=1", "-DSQLITE_SOUNDEX", "/tmp/work/sqlite3.c"}, "/tmp/work",
				[]string{"/tmp/work/sqlite3.c"},
				nil),
			record([]string{"as", "-o", "/tmp/work/_build/sqlite3.o", "/tmp/cc-soundex.s"}, "/tmp/work",
				nil,
				[]string{"/tmp/work/_build/sqlite3.o"}),
			record([]string{"ar", "rcs", "/tmp/work/_build/libsqlite3.a", "/tmp/work/_build/sqlite3.o"}, "/tmp/work",
				[]string{"/tmp/work/_build/sqlite3.o"},
				[]string{"/tmp/work/_build/libsqlite3.a", "/tmp/work/_build/stSoundex2"}),
			record([]string{"cp", "/tmp/work/_build/libsqlite3.a", "/tmp/work/install/lib/libsqlite3.a"}, "/tmp/work",
				[]string{"/tmp/work/_build/libsqlite3.a"},
				[]string{"/tmp/work/install/lib/libsqlite3.a"}),
		},
	}

	got, _, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return ProbeResult{Records: traces[combo], Scope: scope}, nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}

	want := matrix.Combinations()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchCollidesOnSameCc1plusActionParameterDelta(t *testing.T) {
	matrix := formula.Matrix{
		Options: map[string][]string{
			"compact": {"compact-off", "compact-on"},
			"wchar":   {"wchar-off", "wchar-on"},
		},
		DefaultOptions: map[string][]string{
			"compact": {"compact-off"},
			"wchar":   {"wchar-off"},
		},
	}
	traces := map[string]ProbeResult{
		"compact-off-wchar-off": {
			Records: []trace.Record{
				record([]string{"/usr/lib/gcc/aarch64-linux-gnu/12/cc1plus", "-O2", "/tmp/work/src/pugixml.cpp"}, "/tmp/work/_build",
					[]string{"/tmp/work/src/pugixml.cpp", "/tmp/work/src/pugixml.hpp"},
					nil),
				record([]string{"as", "-o", "/tmp/work/_build/pugixml.cpp.o", "/tmp/cc-base.s"}, "/tmp/work/_build",
					nil,
					[]string{"/tmp/work/_build/pugixml.cpp.o"}),
				record([]string{"ar", "rcs", "/tmp/work/_build/libpugixml.a", "/tmp/work/_build/pugixml.cpp.o"}, "/tmp/work/_build",
					[]string{"/tmp/work/_build/pugixml.cpp.o"},
					[]string{"/tmp/work/_build/libpugixml.a", "/tmp/work/_build/stBase1"}),
			},
			Scope: trace.Scope{SourceRoot: "/tmp/work/src", BuildRoot: "/tmp/work/_build", InstallRoot: "/tmp/work/install"},
		},
		"compact-on-wchar-off": {
			Records: []trace.Record{
				record([]string{"/usr/lib/gcc/aarch64-linux-gnu/12/cc1plus", "-O2", "-DPUGIXML_COMPACT", "/tmp/work/src/pugixml.cpp"}, "/tmp/work/_build",
					[]string{"/tmp/work/src/pugixml.cpp", "/tmp/work/src/pugixml.hpp"},
					nil),
				record([]string{"as", "-o", "/tmp/work/_build/pugixml.cpp.o", "/tmp/cc-compact.s"}, "/tmp/work/_build",
					nil,
					[]string{"/tmp/work/_build/pugixml.cpp.o"}),
				record([]string{"ar", "rcs", "/tmp/work/_build/libpugixml.a", "/tmp/work/_build/pugixml.cpp.o"}, "/tmp/work/_build",
					[]string{"/tmp/work/_build/pugixml.cpp.o"},
					[]string{"/tmp/work/_build/libpugixml.a", "/tmp/work/_build/stCompact2"}),
			},
			Scope: trace.Scope{SourceRoot: "/tmp/work/src", BuildRoot: "/tmp/work/_build", InstallRoot: "/tmp/work/install"},
		},
		"compact-off-wchar-on": {
			Records: []trace.Record{
				record([]string{"/usr/lib/gcc/aarch64-linux-gnu/12/cc1plus", "-O2", "-DPUGIXML_WCHAR_MODE", "/tmp/work/src/pugixml.cpp"}, "/tmp/work/_build",
					[]string{"/tmp/work/src/pugixml.cpp", "/tmp/work/src/pugixml.hpp"},
					nil),
				record([]string{"as", "-o", "/tmp/work/_build/pugixml.cpp.o", "/tmp/cc-wchar.s"}, "/tmp/work/_build",
					nil,
					[]string{"/tmp/work/_build/pugixml.cpp.o"}),
				record([]string{"ar", "rcs", "/tmp/work/_build/libpugixml.a", "/tmp/work/_build/pugixml.cpp.o"}, "/tmp/work/_build",
					[]string{"/tmp/work/_build/pugixml.cpp.o"},
					[]string{"/tmp/work/_build/libpugixml.a", "/tmp/work/_build/stWchar3"}),
			},
			Scope: trace.Scope{SourceRoot: "/tmp/work/src", BuildRoot: "/tmp/work/_build", InstallRoot: "/tmp/work/install"},
		},
	}

	got, _, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return traces[combo], nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}

	want := []string{
		"compact-off-wchar-off",
		"compact-off-wchar-on",
		"compact-on-wchar-off",
		"compact-on-wchar-on",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchCollidesOnGeneratedHeaderContentDelta(t *testing.T) {
	baseRoot := t.TempDir()
	probeRoot := t.TempDir()
	for _, root := range []string{baseRoot, probeRoot} {
		for _, dir := range []string{
			root,
			filepath.Join(root, "_build"),
			filepath.Join(root, "install", "lib"),
		} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("MkdirAll(%s): %v", dir, err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, "core.c"), []byte("int main(void) { return 0; }\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(core.c): %v", err)
		}
	}
	writeHeader := func(root, body string) {
		t.Helper()
		path := filepath.Join(root, "_build", "config.h")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}
	writeHeader(baseRoot, "#define FEATURE_FLAG 0\n")
	writeHeader(probeRoot, "#define FEATURE_FLAG 1\n")

	matrix := formula.Matrix{
		Options: map[string][]string{
			"feature": {"feature-off", "feature-on"},
			"mode":    {"mode-off", "mode-on"},
		},
		DefaultOptions: map[string][]string{
			"feature": {"feature-off"},
			"mode":    {"mode-off"},
		},
	}
	traces := map[string]ProbeResult{
		"feature-off-mode-off": {
			Records: []trace.Record{
				record([]string{"cc", "-c", filepath.Join(baseRoot, "core.c"), "-o", filepath.Join(baseRoot, "_build", "core.o")}, baseRoot,
					[]string{filepath.Join(baseRoot, "core.c"), filepath.Join(baseRoot, "_build", "config.h")},
					[]string{filepath.Join(baseRoot, "_build", "core.o")}),
				record([]string{"ar", "rcs", filepath.Join(baseRoot, "_build", "libcore.a"), filepath.Join(baseRoot, "_build", "core.o")}, baseRoot,
					[]string{filepath.Join(baseRoot, "_build", "core.o")},
					[]string{filepath.Join(baseRoot, "_build", "libcore.a")}),
				record([]string{"cp", filepath.Join(baseRoot, "_build", "libcore.a"), filepath.Join(baseRoot, "install", "lib", "libcore.a")}, baseRoot,
					[]string{filepath.Join(baseRoot, "_build", "libcore.a")},
					[]string{filepath.Join(baseRoot, "install", "lib", "libcore.a")}),
			},
			Scope: trace.Scope{SourceRoot: baseRoot, BuildRoot: filepath.Join(baseRoot, "_build"), InstallRoot: filepath.Join(baseRoot, "install")},
		},
		"feature-on-mode-off": {
			Records: []trace.Record{
				record([]string{"cc", "-c", filepath.Join(probeRoot, "core.c"), "-o", filepath.Join(probeRoot, "_build", "core.o")}, probeRoot,
					[]string{filepath.Join(probeRoot, "core.c"), filepath.Join(probeRoot, "_build", "config.h")},
					[]string{filepath.Join(probeRoot, "_build", "core.o")}),
				record([]string{"ar", "rcs", filepath.Join(probeRoot, "_build", "libcore.a"), filepath.Join(probeRoot, "_build", "core.o")}, probeRoot,
					[]string{filepath.Join(probeRoot, "_build", "core.o")},
					[]string{filepath.Join(probeRoot, "_build", "libcore.a")}),
				record([]string{"cp", filepath.Join(probeRoot, "_build", "libcore.a"), filepath.Join(probeRoot, "install", "lib", "libcore.a")}, probeRoot,
					[]string{filepath.Join(probeRoot, "_build", "libcore.a")},
					[]string{filepath.Join(probeRoot, "install", "lib", "libcore.a")}),
			},
			Scope: trace.Scope{SourceRoot: probeRoot, BuildRoot: filepath.Join(probeRoot, "_build"), InstallRoot: filepath.Join(probeRoot, "install")},
		},
		"feature-off-mode-on": {
			Records: []trace.Record{
				record([]string{"cc", "-c", filepath.Join(probeRoot, "core.c"), "-o", filepath.Join(probeRoot, "_build", "core.o")}, probeRoot,
					[]string{filepath.Join(probeRoot, "core.c"), filepath.Join(probeRoot, "_build", "config.h")},
					[]string{filepath.Join(probeRoot, "_build", "core.o")}),
				record([]string{"ar", "rcs", filepath.Join(probeRoot, "_build", "libcore.a"), filepath.Join(probeRoot, "_build", "core.o")}, probeRoot,
					[]string{filepath.Join(probeRoot, "_build", "core.o")},
					[]string{filepath.Join(probeRoot, "_build", "libcore.a")}),
				record([]string{"cp", filepath.Join(probeRoot, "_build", "libcore.a"), filepath.Join(probeRoot, "install", "lib", "libcore.a")}, probeRoot,
					[]string{filepath.Join(probeRoot, "_build", "libcore.a")},
					[]string{filepath.Join(probeRoot, "install", "lib", "libcore.a")}),
			},
			Scope: trace.Scope{SourceRoot: probeRoot, BuildRoot: filepath.Join(probeRoot, "_build"), InstallRoot: filepath.Join(probeRoot, "install")},
		},
	}

	got, _, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return traces[combo], nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}

	want := []string{
		"feature-off-mode-off",
		"feature-off-mode-on",
		"feature-on-mode-off",
		"feature-on-mode-on",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchDoesNotPropagateConfigureNoiseIntoBusinessDiff(t *testing.T) {
	baseRoot := t.TempDir()
	apiRoot := t.TempDir()
	shipRoot := t.TempDir()
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

	scopeFor := func(root string) trace.Scope {
		return trace.Scope{
			SourceRoot:  root,
			BuildRoot:   filepath.Join(root, "_build"),
			InstallRoot: filepath.Join(root, "install"),
		}
	}
	for _, root := range []string{baseRoot, apiRoot, shipRoot} {
		if err := os.MkdirAll(filepath.Join(root, "_build"), 0o755); err != nil {
			t.Fatalf("MkdirAll(_build): %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, "install", "lib"), 0o755); err != nil {
			t.Fatalf("MkdirAll(install/lib): %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, "install", "include"), 0o755); err != nil {
			t.Fatalf("MkdirAll(install/include): %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "CMakeLists.txt"), []byte("cmake_minimum_required(VERSION 3.25)\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(CMakeLists.txt): %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "core.c"), []byte("int main(void) { return 0; }\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(core.c): %v", err)
		}
	}

	configureWrites := func(root, tag string) []string {
		buildRoot := filepath.Join(root, "_build")
		return []string{
			filepath.Join(buildRoot, "trace_options.h"),
			filepath.Join(buildRoot, "cmake_install.cmake"),
			filepath.Join(buildRoot, "CMakeFiles", "CMakeScratch", "TryCompile-"+tag, "CheckIncludeFile.c"),
		}
	}

	traces := map[string]ProbeResult{
		"api-off-ship-off": {
			Records: []trace.Record{
				record([]string{"cmake", "-S", baseRoot, "-B", filepath.Join(baseRoot, "_build")}, baseRoot,
					[]string{filepath.Join(baseRoot, "CMakeLists.txt")},
					configureWrites(baseRoot, "base")),
				record([]string{"cc", "-c", filepath.Join(baseRoot, "core.c"), "-o", filepath.Join(baseRoot, "_build", "core.o")}, baseRoot,
					[]string{filepath.Join(baseRoot, "core.c"), filepath.Join(baseRoot, "_build", "trace_options.h")},
					[]string{filepath.Join(baseRoot, "_build", "core.o")}),
				record([]string{"ar", "rcs", filepath.Join(baseRoot, "_build", "libtracecore.a"), filepath.Join(baseRoot, "_build", "core.o")}, baseRoot,
					[]string{filepath.Join(baseRoot, "_build", "core.o")},
					[]string{filepath.Join(baseRoot, "_build", "libtracecore.a")}),
				record([]string{"install", "-m644", filepath.Join(baseRoot, "_build", "libtracecore.a"), filepath.Join(baseRoot, "install", "lib", "libtracecore.a")}, baseRoot,
					[]string{filepath.Join(baseRoot, "_build", "cmake_install.cmake"), filepath.Join(baseRoot, "_build", "libtracecore.a")},
					[]string{filepath.Join(baseRoot, "install", "lib", "libtracecore.a")}),
			},
			Scope: scopeFor(baseRoot),
		},
		"api-on-ship-off": {
			Records: []trace.Record{
				record([]string{"cmake", "-S", apiRoot, "-B", filepath.Join(apiRoot, "_build")}, apiRoot,
					[]string{filepath.Join(apiRoot, "CMakeLists.txt")},
					configureWrites(apiRoot, "api")),
				record([]string{"cc", "-DTRACE_API", "-c", filepath.Join(apiRoot, "core.c"), "-o", filepath.Join(apiRoot, "_build", "core.o")}, apiRoot,
					[]string{filepath.Join(apiRoot, "core.c"), filepath.Join(apiRoot, "_build", "trace_options.h")},
					[]string{filepath.Join(apiRoot, "_build", "core.o")}),
				record([]string{"ar", "rcs", filepath.Join(apiRoot, "_build", "libtracecore.a"), filepath.Join(apiRoot, "_build", "core.o")}, apiRoot,
					[]string{filepath.Join(apiRoot, "_build", "core.o")},
					[]string{filepath.Join(apiRoot, "_build", "libtracecore.a")}),
				record([]string{"install", "-m644", filepath.Join(apiRoot, "_build", "libtracecore.a"), filepath.Join(apiRoot, "install", "lib", "libtracecore.a")}, apiRoot,
					[]string{filepath.Join(apiRoot, "_build", "cmake_install.cmake"), filepath.Join(apiRoot, "_build", "libtracecore.a")},
					[]string{filepath.Join(apiRoot, "install", "lib", "libtracecore.a")}),
			},
			Scope: scopeFor(apiRoot),
		},
		"api-off-ship-on": {
			Records: []trace.Record{
				record([]string{"cmake", "-S", shipRoot, "-B", filepath.Join(shipRoot, "_build")}, shipRoot,
					[]string{filepath.Join(shipRoot, "CMakeLists.txt")},
					configureWrites(shipRoot, "ship")),
				record([]string{"cc", "-c", filepath.Join(shipRoot, "core.c"), "-o", filepath.Join(shipRoot, "_build", "core.o")}, shipRoot,
					[]string{filepath.Join(shipRoot, "core.c"), filepath.Join(shipRoot, "_build", "trace_options.h")},
					[]string{filepath.Join(shipRoot, "_build", "core.o")}),
				record([]string{"ar", "rcs", filepath.Join(shipRoot, "_build", "libtracecore.a"), filepath.Join(shipRoot, "_build", "core.o")}, shipRoot,
					[]string{filepath.Join(shipRoot, "_build", "core.o")},
					[]string{filepath.Join(shipRoot, "_build", "libtracecore.a")}),
				record([]string{"install", "-m644", filepath.Join(shipRoot, "_build", "libtracecore.a"), filepath.Join(shipRoot, "install", "lib", "libtracecore.a")}, shipRoot,
					[]string{filepath.Join(shipRoot, "_build", "cmake_install.cmake"), filepath.Join(shipRoot, "_build", "libtracecore.a")},
					[]string{filepath.Join(shipRoot, "install", "lib", "libtracecore.a"), filepath.Join(shipRoot, "install", "include", "trace_alias.h")}),
			},
			Scope: scopeFor(shipRoot),
		},
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return traces[combo], nil
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

func TestAppendReaderSeedsSkipsUnknownAndToolingPaths(t *testing.T) {
	graph := actionGraph{
		scope: trace.Scope{InstallRoot: "/install"},
		actions: []actionNode{
			{},
			{},
			{},
			{},
			{kind: kindInstall, writes: []string{"/install/lib/libfoo.a"}},
			{kind: kindLink, writes: []string{"/build/bin/foo"}},
			{kind: kindConfigure, writes: []string{"/build/tooling.out"}},
		},
		tooling: []bool{false, false, false, false, false, false, true},
		paths: map[string]pathFacts{
			"/unknown": {
				path:    "/unknown",
				readers: []int{1},
				role:    roleUnknown,
			},
			"/tooling": {
				path:    "/tooling",
				readers: []int{2},
				role:    roleTooling,
			},
			"/propagating": {
				path:    "/propagating",
				readers: []int{3},
				role:    rolePropagating,
			},
			"/delivery": {
				path:    "/delivery",
				readers: []int{4},
				role:    roleDelivery,
			},
			"/delivery-link": {
				path:    "/delivery-link",
				readers: []int{5},
				role:    roleDelivery,
			},
			"/tooling-reader": {
				path:    "/tooling-reader",
				readers: []int{6},
				role:    rolePropagating,
			},
			"/install/lib/libfoo.a": {
				path: "/install/lib/libfoo.a",
				role: roleDelivery,
			},
			"/build/bin/foo": {
				path: "/build/bin/foo",
				role: rolePropagating,
			},
		},
	}

	got := appendReaderSeeds(nil, graph, []string{"/unknown", "/tooling", "/propagating", "/delivery", "/delivery-link", "/tooling-reader"})
	want := []int{3, 5}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("appendReaderSeeds() = %v, want %v", got, want)
	}
}

func TestBuildRefinedActionDiffSeparatesPropagationSeedWrites(t *testing.T) {
	scope := trace.Scope{
		BuildRoot:   "/build",
		InstallRoot: "/install",
	}
	base := actionGraph{
		scope: scope,
		actions: []actionNode{
			{},
		},
	}
	probe := actionGraph{
		scope: scope,
		actions: []actionNode{
			{
				writes: []string{
					"/build/generated.h",
					"/build/install-only.stamp",
					"/build/no-reader.stamp",
					"/build/tooling-only.stamp",
					"/build/non-business.stamp",
				},
			},
			{kind: kindCompile},
			{kind: kindInstall, writes: []string{"/install/lib/libfoo.a"}},
			{kind: kindConfigure, writes: []string{"/build/tooling.out"}},
			{kind: kindGeneric, reads: []string{"/build/non-business.stamp"}},
		},
		tooling:  []bool{false, false, false, true, false},
		business: []bool{false, true, false, false, false},
		paths: map[string]pathFacts{
			"/build/generated.h": {
				path:    "/build/generated.h",
				role:    rolePropagating,
				readers: []int{1},
			},
			"/build/install-only.stamp": {
				path:    "/build/install-only.stamp",
				role:    roleDelivery,
				readers: []int{2},
			},
			"/build/no-reader.stamp": {
				path: "/build/no-reader.stamp",
				role: rolePropagating,
			},
			"/build/tooling-only.stamp": {
				path:    "/build/tooling-only.stamp",
				role:    rolePropagating,
				readers: []int{3},
			},
			"/build/non-business.stamp": {
				path:    "/build/non-business.stamp",
				role:    rolePropagating,
				readers: []int{4},
			},
			"/install/lib/libfoo.a": {
				path: "/install/lib/libfoo.a",
				role: roleDelivery,
			},
		},
	}
	diff := buildRefinedActionDiff(base, probe, 0, 0)
	wantWrites := []string{
		"/build/generated.h",
		"/build/install-only.stamp",
		"/build/no-reader.stamp",
		"/build/tooling-only.stamp",
		"/build/non-business.stamp",
	}
	if !reflect.DeepEqual(diff.probeWrites, wantWrites) {
		t.Fatalf("probeWrites = %v, want %v", diff.probeWrites, wantWrites)
	}
	wantSeedWrites := []string{"/build/generated.h"}
	if !reflect.DeepEqual(diff.probeSeedWrites, wantSeedWrites) {
		t.Fatalf("probeSeedWrites = %v, want %v", diff.probeSeedWrites, wantSeedWrites)
	}
}

func TestRefineDiffActionsPairsOnlyUniqueRefinableActions(t *testing.T) {
	base := actionGraph{
		actions: []actionNode{
			{kind: kindConfigure, actionKey: "configure|build|cwd=$SRC|argv=cmake -S $SRC", fullKey: "base-0", fingerprint: "base-0"},
		},
	}
	probe := actionGraph{
		actions: []actionNode{
			{kind: kindConfigure, actionKey: "configure|build|cwd=$SRC|argv=cmake -S $SRC", fullKey: "probe-0", fingerprint: "probe-0"},
		},
	}

	refined := refineDiffActions(base, probe, []int{0}, []int{0})
	if len(refined.pairs) != 1 {
		t.Fatalf("len(refined.pairs) = %d, want 1", len(refined.pairs))
	}
	if _, ok := refined.base[0]; !ok {
		t.Fatalf("refined.base[0] missing")
	}
	if _, ok := refined.probe[0]; !ok {
		t.Fatalf("refined.probe[0] missing")
	}
}

func TestRefineDiffActionsSkipsAmbiguousRepeatedRefinableActions(t *testing.T) {
	base := actionGraph{
		actions: []actionNode{
			{kind: kindConfigure, actionKey: "configure|build|cwd=$SRC|argv=cmake -S $SRC", fullKey: "base-a", fingerprint: "base-a"},
			{kind: kindConfigure, actionKey: "configure|build|cwd=$SRC|argv=cmake -S $SRC", fullKey: "base-b", fingerprint: "base-b"},
		},
	}
	probe := actionGraph{
		actions: []actionNode{
			{kind: kindConfigure, actionKey: "configure|build|cwd=$SRC|argv=cmake -S $SRC", fullKey: "probe-a", fingerprint: "probe-a"},
			{kind: kindConfigure, actionKey: "configure|build|cwd=$SRC|argv=cmake -S $SRC", fullKey: "probe-b", fingerprint: "probe-b"},
		},
	}

	refined := refineDiffActions(base, probe, []int{0, 1}, []int{0, 1})
	if len(refined.pairs) != 0 {
		t.Fatalf("len(refined.pairs) = %d, want 0", len(refined.pairs))
	}
	if len(refined.base) != 0 || len(refined.probe) != 0 {
		t.Fatalf("refined indexes = base %v probe %v, want both empty", refined.base, refined.probe)
	}
}

func TestDiffSeedUnmatchedProbeActionsSeparatesProfileOnlyActions(t *testing.T) {
	graph := actionGraph{
		scope: trace.Scope{InstallRoot: "/install"},
		actions: []actionNode{
			{kind: kindInstall, writes: []string{"/install/lib/libfoo.a"}},
			{kind: kindConfigure, reads: []string{"/build/config.in"}, writes: []string{"/build/control.log"}},
			{kind: kindLink, writes: []string{"/build/bin/foo"}},
			{kind: kindLink, reads: []string{"/build/bin/foo"}, writes: []string{"/build/bin/bar"}},
		},
		tooling: []bool{false, true, false, false},
		paths: map[string]pathFacts{
			"/install/lib/libfoo.a": {
				path: "/install/lib/libfoo.a",
				role: roleDelivery,
			},
			"/build/control.log": {
				path: "/build/control.log",
				role: roleTooling,
			},
			"/build/config.in": {
				path: "/build/config.in",
				role: rolePropagating,
			},
			"/build/bin/foo": {
				path:    "/build/bin/foo",
				role:    rolePropagating,
				readers: []int{3},
			},
			"/build/bin/bar": {
				path: "/build/bin/bar",
				role: rolePropagating,
			},
		},
	}
	state := diffState{
		probe:     graph,
		profile:   initOptionProfile(),
		probeOnly: []int{0, 1, 2},
		refined:   initRefinedDiffResult(),
	}

	diffInitPropagation(&state)
	diffSeedUnmatchedProbeActions(&state)

	if !reflect.DeepEqual(state.seedPaths, []string{"/build/bin/foo"}) {
		t.Fatalf("seedPaths = %v, want %v", state.seedPaths, []string{"/build/bin/foo"})
	}
	if _, ok := state.profile.deliveryWrites["/install/lib/libfoo.a"]; !ok {
		t.Fatalf("delivery-only unmatched action did not record delivery write")
	}
	if _, ok := state.profile.toolingWrites["/build/control.log"]; !ok {
		t.Fatalf("tooling unmatched action did not record tooling write")
	}
	if _, ok := state.profile.propagatingReads["/build/config.in"]; ok {
		t.Fatalf("unmatched configure/tooling action should not record reads")
	}
	if _, ok := state.profile.propagatingWrites["/build/bin/foo"]; !ok {
		t.Fatalf("business unmatched action did not record propagating write")
	}
}

func TestDiffPropagateUsesSeedPathsInsteadOfRootActions(t *testing.T) {
	graph := actionGraph{
		scope: trace.Scope{},
		actions: []actionNode{
			{kind: kindLink, writes: []string{"/build/libfoo.a"}},
			{kind: kindLink, reads: []string{"/build/libfoo.a", "/build/other-input"}, writes: []string{"/build/foo"}},
			{kind: kindLink, reads: []string{"/build/foo"}, writes: []string{"/build/bar"}},
			{kind: kindConfigure, reads: []string{"/build/libfoo.a"}, writes: []string{"/build/tooling.out"}},
		},
		tooling: []bool{false, false, false, true},
		paths: map[string]pathFacts{
			"/build/libfoo.a": {
				path:    "/build/libfoo.a",
				role:    rolePropagating,
				readers: []int{1, 3},
			},
			"/build/foo": {
				path:    "/build/foo",
				role:    rolePropagating,
				readers: []int{2},
			},
			"/build/other-input": {
				path: "/build/other-input",
				role: rolePropagating,
			},
			"/build/bar": {
				path: "/build/bar",
				role: rolePropagating,
			},
		},
	}
	state := diffState{
		probe:     graph,
		profile:   initOptionProfile(),
		visited:   make([]bool, len(graph.actions)),
		seedPaths: []string{"/build/libfoo.a"},
	}

	diffPropagate(&state)

	if _, ok := state.profile.propagatingReads["/build/libfoo.a"]; !ok {
		t.Fatalf("first propagated action did not record seed path read")
	}
	if _, ok := state.profile.propagatingWrites["/build/foo"]; !ok {
		t.Fatalf("first propagated action did not record write")
	}
	if _, ok := state.profile.propagatingReads["/build/foo"]; !ok {
		t.Fatalf("second propagated action did not record downstream read")
	}
	if _, ok := state.profile.propagatingWrites["/build/bar"]; !ok {
		t.Fatalf("second propagated action did not record downstream write")
	}
	if _, ok := state.profile.propagatingReads["/build/other-input"]; ok {
		t.Fatalf("propagation should not record sibling read unrelated to seed path")
	}
	if _, ok := state.profile.toolingWrites["/build/tooling.out"]; ok {
		t.Fatalf("tooling reader should not be propagated from seed path")
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

func TestMatchActionFingerprintsIgnoresCompileCwd(t *testing.T) {
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	base := buildGraphWithScope([]trace.Record{
		record(
			[]string{"cc", "-c", "/tmp/work/Foundation/src/pcre2_convert.c", "-o", "/tmp/work/_build/Foundation/CMakeFiles/Foundation.dir/src/pcre2_convert.c.o"},
			"/tmp/work",
			[]string{"/tmp/work/Foundation/src/pcre2_convert.c"},
			[]string{"/tmp/work/_build/Foundation/CMakeFiles/Foundation.dir/src/pcre2_convert.c.o"},
		),
	}, scope)
	probe := buildGraphWithScope([]trace.Record{
		record(
			[]string{"cc", "-c", "/tmp/work/Foundation/src/pcre2_convert.c", "-o", "/tmp/work/_build/Foundation/CMakeFiles/Foundation.dir/src/pcre2_convert.c.o"},
			"/tmp/work/_build/Foundation",
			[]string{"/tmp/work/Foundation/src/pcre2_convert.c"},
			[]string{"/tmp/work/_build/Foundation/CMakeFiles/Foundation.dir/src/pcre2_convert.c.o"},
		),
	}, scope)

	matched, baseOnly, probeOnly := matchActionFingerprints(base, probe)
	if matched != 1 {
		t.Fatalf("matched = %d, want 1", matched)
	}
	if len(baseOnly) != 0 || len(probeOnly) != 0 {
		t.Fatalf("baseOnly=%v probeOnly=%v, want both empty", baseOnly, probeOnly)
	}
}

func TestNormalizeScopeTokenPrefersNestedInstallRoot(t *testing.T) {
	scope := trace.Scope{
		SourceRoot:  "/workspace/project",
		BuildRoot:   "/workspace/project/_build",
		InstallRoot: "/workspace/project/_build/install",
	}

	if got, want := normalizeScopeToken("/workspace/project/_build/install/lib/foo.h", scope), "$INSTALL/lib/foo.h"; got != want {
		t.Fatalf("normalizeScopeToken(path) = %q, want %q", got, want)
	}
	if got, want := normalizeScopeToken("-I/workspace/project/_build/install/include", scope), "-I$INSTALL/include"; got != want {
		t.Fatalf("normalizeScopeToken(arg) = %q, want %q", got, want)
	}
}

func TestNormalizePathDoesNotRewriteLegitimateNumericTokens(t *testing.T) {
	cases := map[string]string{
		"/port/8080/config":  "/port/8080/config",
		"/releases/2024/bin": "/releases/2024/bin",
		"libfoo.so.1.2.345":  "libfoo.so.1.2.345",
	}
	for in, want := range cases {
		if got := normalizePath(in); got != want {
			t.Fatalf("normalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeScopeTokenNormalizesKnownBuildNoiseOnly(t *testing.T) {
	scope := trace.Scope{
		SourceRoot: "/workspace/project",
		BuildRoot:  "/workspace/project/_build",
	}

	cases := map[string]string{
		"/workspace/project/_build/CMakeFiles/CMakeScratch/TryCompile-12345":        "$BUILD/CMakeFiles/CMakeScratch/TryCompile-$$ID",
		"/workspace/project/_build/bin/cmTC_a1b2c3":                                 "$BUILD/bin/cmTC_$$ID",
		"/workspace/project/_build/tmp/output.tmp.1234":                             "$BUILD/tmp/output.tmp.$$ID",
		"/workspace/project/_build/CMakeFiles/CMakeScratch/TryCompile-doc/cmTC_tls": "$BUILD/CMakeFiles/CMakeScratch/TryCompile-doc/cmTC_tls",
		"/workspace/project/src/TryCompile-12345/cmTC_a1b2c3.tmp.1234":              "$SRC/src/TryCompile-12345/cmTC_a1b2c3.tmp.1234",
	}
	for in, want := range cases {
		if got := normalizeScopeToken(in, scope); got != want {
			t.Fatalf("normalizeScopeToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeScopeTokenStabilizesEmbeddedInstallRootWithNormalizedScope(t *testing.T) {
	scope := normalizeScope(trace.Scope{
		SourceRoot:  "/tmp/work",
		InstallRoot: "/tmp/work/out-a",
	})
	if got, want := normalizeScopeToken("--prefix=/tmp/work/out-a", scope), "--prefix=$INSTALL"; got != want {
		t.Fatalf("normalizeScopeToken(embedded install) = %q, want %q", got, want)
	}
}

func TestDebugLocalTraceoptionsShipProfile(t *testing.T) {
	if os.Getenv("LLAR_DEBUG_LOCALTRACE") == "" {
		t.Skip("set LLAR_DEBUG_LOCALTRACE=1 to inspect local trace dump")
	}

	path := os.Getenv("LLAR_TRACE_DUMP")
	if path == "" {
		path = filepath.Join("/Users/haolan/project/llar", "localtrace.log")
	}

	results, err := parseTraceDumpForTest(path)
	if err != nil {
		t.Fatalf("parseTraceDumpForTest(%q): %v", path, err)
	}

	base := results["api-off-cli-off-ship-off"]
	ship := results["api-off-cli-off-ship-on"]
	if len(base.Records) == 0 || len(ship.Records) == 0 {
		t.Fatalf("missing baseline or ship trace records")
	}

	baseGraph := buildGraphWithScope(base.Records, base.Scope)
	shipGraph := buildGraphWithScope(ship.Records, ship.Scope)
	matched, baseOnly, probeOnly := matchActionFingerprints(baseGraph, shipGraph)
	profile := diffProfile(baseGraph, shipGraph)
	refined := refineDiffActions(baseGraph, shipGraph, baseOnly, probeOnly)

	t.Logf("matched=%d base-only=%d probe-only=%d", matched, len(baseOnly), len(probeOnly))
	for _, diff := range refined.pairs {
		action := baseGraph.actions[diff.baseIdx]
		if !strings.Contains(action.actionKey, "configure|build|cwd=$SRC|argv=cmake -S $SRC") {
			continue
		}
		t.Logf(
			"refined root configure flags: trace_options(base=%v probe=%v) cmake_install(base=%v probe=%v) dependinfo(base=%v probe=%v)",
			slices.ContainsFunc(diff.baseWrites, func(path string) bool { return strings.Contains(path, "/_build/trace_options.h") }),
			slices.ContainsFunc(diff.probeWrites, func(path string) bool { return strings.Contains(path, "/_build/trace_options.h") }),
			slices.ContainsFunc(diff.baseWrites, func(path string) bool { return strings.Contains(path, "/_build/cmake_install.cmake") }),
			slices.ContainsFunc(diff.probeWrites, func(path string) bool { return strings.Contains(path, "/_build/cmake_install.cmake") }),
			slices.ContainsFunc(diff.baseWrites, func(path string) bool {
				return strings.Contains(path, "/_build/CMakeFiles/tracecore.dir/DependInfo.cmake")
			}),
			slices.ContainsFunc(diff.probeWrites, func(path string) bool {
				return strings.Contains(path, "/_build/CMakeFiles/tracecore.dir/DependInfo.cmake")
			}),
		)
	}
	logSelected := func(label string, graph actionGraph, indexes []int) {
		for _, idx := range indexes {
			action := graph.actions[idx]
			if !strings.Contains(action.actionKey, "src=$SRC/core.c") &&
				!strings.Contains(action.actionKey, "out=$BUILD/libtracecore.a") &&
				!strings.Contains(action.actionKey, "out=$BUILD/CMakeFiles/tracecore.dir/core.c.o") {
				continue
			}
			t.Logf("%s-selected: %s :: %s", label, action.actionKey, strings.Join(action.argv, " "))
		}
	}
	logSelected("base-only", baseGraph, baseOnly)
	logSelected("probe-only", shipGraph, probeOnly)
	t.Logf("propagating-writes=%v", slices.Sorted(maps.Keys(profile.propagatingWrites)))
	t.Logf("propagating-reads=%v", slices.Sorted(maps.Keys(profile.propagatingReads)))
	t.Logf("unknown-writes=%v", slices.Sorted(maps.Keys(profile.unknownWrites)))
	t.Logf("unknown-reads=%v", slices.Sorted(maps.Keys(profile.unknownReads)))
	t.Logf("delivery-writes=%v", slices.Sorted(maps.Keys(profile.deliveryWrites)))
}

func TestDebugLocalTraceoptionsApiCliCollision(t *testing.T) {
	if os.Getenv("LLAR_DEBUG_LOCALTRACE") == "" {
		t.Skip("set LLAR_DEBUG_LOCALTRACE=1 to inspect local trace dump")
	}

	path := os.Getenv("LLAR_TRACE_DUMP")
	if path == "" {
		path = filepath.Join("/Users/haolan/project/llar", "localtrace.log")
	}

	results, err := parseTraceDumpForTest(path)
	if err != nil {
		t.Fatalf("parseTraceDumpForTest(%q): %v", path, err)
	}

	base := results["api-off-cli-off-ship-off"]
	api := results["api-on-cli-off-ship-off"]
	cli := results["api-off-cli-on-ship-off"]
	if len(base.Records) == 0 || len(api.Records) == 0 || len(cli.Records) == 0 {
		t.Fatalf("missing baseline/api/cli trace records")
	}

	baseGraph := buildGraphWithScope(base.Records, base.Scope)
	apiGraph := buildGraphWithScope(api.Records, api.Scope)
	cliGraph := buildGraphWithScope(cli.Records, cli.Scope)

	matched, baseOnly, probeOnly := matchActionFingerprints(baseGraph, cliGraph)
	t.Logf("cli matched=%d base-only=%d probe-only=%d", matched, len(baseOnly), len(probeOnly))
	for _, idx := range probeOnly {
		action := cliGraph.actions[idx]
		if !strings.Contains(strings.Join(action.argv, " "), "tracecli") &&
			!strings.Contains(action.actionKey, "tracecli") &&
			!slices.ContainsFunc(action.reads, func(path string) bool { return strings.Contains(path, "/_build/libtracecore.a") }) &&
			!slices.ContainsFunc(action.writes, func(path string) bool { return strings.Contains(path, "/_build/tracecli") }) {
			continue
		}
		t.Logf("cli probe-only action: kind=%s key=%s reads=%v writes=%v argv=%s", action.kind.String(), action.actionKey, action.reads, action.writes, strings.Join(action.argv, " "))
	}

	apiProfile := diffProfile(baseGraph, apiGraph)
	cliProfile := diffProfile(baseGraph, cliGraph)

	t.Logf("api propagating-writes=%v", slices.Sorted(maps.Keys(apiProfile.propagatingWrites)))
	t.Logf("api propagating-reads=%v", slices.Sorted(maps.Keys(apiProfile.propagatingReads)))
	t.Logf("api unknown-writes=%v", slices.Sorted(maps.Keys(apiProfile.unknownWrites)))
	t.Logf("api unknown-reads=%v", slices.Sorted(maps.Keys(apiProfile.unknownReads)))
	t.Logf("api delivery-writes=%v", slices.Sorted(maps.Keys(apiProfile.deliveryWrites)))

	t.Logf("cli propagating-writes=%v", slices.Sorted(maps.Keys(cliProfile.propagatingWrites)))
	t.Logf("cli propagating-reads=%v", slices.Sorted(maps.Keys(cliProfile.propagatingReads)))
	t.Logf("cli unknown-writes=%v", slices.Sorted(maps.Keys(cliProfile.unknownWrites)))
	t.Logf("cli unknown-reads=%v", slices.Sorted(maps.Keys(cliProfile.unknownReads)))
	t.Logf("cli delivery-writes=%v", slices.Sorted(maps.Keys(cliProfile.deliveryWrites)))

	t.Logf("profilesCollide=%v", profilesCollide([]optionVariant{{profile: apiProfile}}, []optionVariant{{profile: cliProfile}}))
}

func parseTraceDumpForTest(path string) (map[string]ProbeResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	results := make(map[string]ProbeResult)
	var current string
	var records []trace.Record
	var digests map[string]string
	var currentRecord *trace.Record

	flushRecord := func() {
		if currentRecord == nil {
			return
		}
		records = append(records, *currentRecord)
		currentRecord = nil
	}
	flushCombo := func() {
		flushRecord()
		if current == "" {
			return
		}
		results[current] = ProbeResult{
			Records:      records,
			Scope:        inferScopeFromRecords(records),
			InputDigests: maps.Clone(digests),
		}
		current = ""
		records = nil
		digests = nil
	}

	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		switch {
		case strings.HasPrefix(line, "COMBO "):
			flushCombo()
			current = strings.TrimSpace(strings.TrimPrefix(line, "COMBO "))
		case strings.HasPrefix(line, "DIGESTS"):
			flushRecord()
			if digests == nil {
				digests = make(map[string]string)
			}
		case strings.HasPrefix(line, "   ") && currentRecord == nil && digests != nil:
			path, sum, ok := parseDigestLineForTest(strings.TrimSpace(line))
			if ok {
				digests[path] = sum
			}
		case recordPrefixIndex(line) >= 0:
			flushRecord()
			dot := recordPrefixIndex(line)
			argvLine := strings.TrimSpace(line[dot+2:])
			currentRecord = &trace.Record{Argv: strings.Fields(strings.TrimSpace(strings.TrimPrefix(argvLine, "argv: ")))}
		case strings.HasPrefix(line, "   pid: "):
			if currentRecord != nil {
				currentRecord.PID = parseInt64ForTest(strings.TrimSpace(strings.TrimPrefix(line, "   pid: ")))
			}
		case strings.HasPrefix(line, "   ppid: "):
			if currentRecord != nil {
				currentRecord.ParentPID = parseInt64ForTest(strings.TrimSpace(strings.TrimPrefix(line, "   ppid: ")))
			}
		case strings.HasPrefix(line, "   cwd: "):
			if currentRecord != nil {
				currentRecord.Cwd = strings.TrimSpace(strings.TrimPrefix(line, "   cwd: "))
			}
		case strings.HasPrefix(line, "   inputs: "):
			if currentRecord != nil {
				currentRecord.Inputs = splitCSVForTest(strings.TrimSpace(strings.TrimPrefix(line, "   inputs: ")))
			}
		case strings.HasPrefix(line, "   changes: "):
			if currentRecord != nil {
				currentRecord.Changes = splitCSVForTest(strings.TrimSpace(strings.TrimPrefix(line, "   changes: ")))
			}
		}
	}
	flushCombo()
	return results, nil
}

func recordPrefixIndex(line string) int {
	dot := strings.Index(line, ". ")
	if dot <= 0 {
		return -1
	}
	if _, err := strconv.Atoi(line[:dot]); err != nil {
		return -1
	}
	return dot
}

func parseDigestLineForTest(line string) (path, sum string, ok bool) {
	path, sum, ok = strings.Cut(line, " = ")
	if !ok {
		return "", "", false
	}
	path = strings.TrimSpace(path)
	sum = strings.TrimSpace(sum)
	if path == "" || sum == "" {
		return "", "", false
	}
	return path, sum, true
}

func TestParseTraceDumpForTestPreservesInputDigests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.log")
	content := strings.Join([]string{
		"COMBO debug-on",
		"DIGESTS",
		"   /tmp/work/_build/generated.h = aaaaaaaaaaaaaaaa",
		"1. argv: cc -c /tmp/work/core.c -o /tmp/work/_build/core.o",
		"   cwd: /tmp/work",
		"   inputs: /tmp/work/core.c, /tmp/work/_build/generated.h",
		"   changes: /tmp/work/_build/core.o",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(trace): %v", err)
	}

	results, err := parseTraceDumpForTest(path)
	if err != nil {
		t.Fatalf("parseTraceDumpForTest(%q): %v", path, err)
	}
	probe, ok := results["debug-on"]
	if !ok {
		t.Fatalf("results missing combo %q", "debug-on")
	}
	if got := probe.InputDigests["/tmp/work/_build/generated.h"]; got != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("probe.InputDigests[generated.h] = %q, want %q", got, "aaaaaaaaaaaaaaaa")
	}
}

func splitCSVForTest(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return slices.DeleteFunc(parts, func(part string) bool { return part == "" })
}

func parseInt64ForTest(value string) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func inferScopeFromRecords(records []trace.Record) trace.Scope {
	var scope trace.Scope
	for _, record := range records {
		for i := 0; i < len(record.Argv); i++ {
			switch record.Argv[i] {
			case "-S":
				if i+1 < len(record.Argv) {
					scope.SourceRoot = record.Argv[i+1]
				}
			case "-B":
				if i+1 < len(record.Argv) {
					scope.BuildRoot = record.Argv[i+1]
				}
			case "--prefix":
				if i+1 < len(record.Argv) {
					scope.InstallRoot = record.Argv[i+1]
				}
			}
			if strings.HasPrefix(record.Argv[i], "-S") && len(record.Argv[i]) > 2 {
				scope.SourceRoot = strings.TrimPrefix(record.Argv[i], "-S")
			}
			if strings.HasPrefix(record.Argv[i], "-B") && len(record.Argv[i]) > 2 {
				scope.BuildRoot = strings.TrimPrefix(record.Argv[i], "-B")
			}
		}
	}
	return scope
}
