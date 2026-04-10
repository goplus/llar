package evaluator

import (
	"context"
	"reflect"
	"testing"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/trace"
)

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

func event(seq *int64, pid, parent int64, cwd string, kind trace.EventKind, path string, argv ...string) trace.Event {
	current := *seq
	*seq = current + 1
	return trace.Event{
		Seq:       current,
		PID:       pid,
		ParentPID: parent,
		Cwd:       cwd,
		Kind:      kind,
		Path:      path,
		Argv:      argv,
	}
}

func traceoptionsEventProbe(apiOn, cliOn, shipOn bool) ProbeResult {
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	configureArgv := []string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}
	if apiOn {
		configureArgv = append(configureArgv, "-DTRACE_FEATURE_API=ON")
	}
	arTemp, ranlibTemp := traceoptionsArchiveTemps(apiOn, cliOn, shipOn)
	if cliOn {
		configureArgv = append(configureArgv, "-DTRACE_BUILD_CLI=ON")
	}
	if shipOn {
		configureArgv = append(configureArgv, "-DTRACE_INSTALL_ALIAS=ON")
	}
	traceOptionsDigest := "trace-options-off"
	libDigest := "libtracecore-off"
	if apiOn {
		traceOptionsDigest = "trace-options-api-on"
		libDigest = "libtracecore-api-on"
	}
	installScriptDigest := "cmake-install-default"
	if cliOn {
		installScriptDigest = "cmake-install-cli"
	}
	if shipOn {
		installScriptDigest = "cmake-install-ship"
	}
	compileArgv := []string{
		"/usr/bin/cc",
		"-I/tmp/work",
		"-I/tmp/work/_build",
		"-o", "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o",
		"-c", "/tmp/work/core.c",
	}
	if apiOn {
		compileArgv = []string{
			"/usr/bin/cc",
			"-DTRACE_FEATURE_API",
			"-I/tmp/work",
			"-I/tmp/work/_build",
			"-o", "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o",
			"-c", "/tmp/work/core.c",
		}
	}

	seq := int64(1)
	events := []trace.Event{
		event(&seq, 100, 1, "/tmp/work", trace.EventExec, "", configureArgv...),
		event(&seq, 100, 0, "/tmp/work", trace.EventRead, "/tmp/work/CMakeLists.txt"),
		event(&seq, 100, 0, "/tmp/work", trace.EventRead, "/tmp/work/trace_options.h.in"),
		event(&seq, 100, 0, "/tmp/work", trace.EventRead, "/tmp/work/_build/CMakeFiles/pkgRedirects"),
		event(&seq, 100, 0, "/tmp/work", trace.EventRead, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects"),
		event(&seq, 100, 0, "/tmp/work", trace.EventWrite, "/tmp/work/_build/trace_options.h"),
		event(&seq, 100, 0, "/tmp/work", trace.EventWrite, "/tmp/work/_build/CMakeFiles/pkgRedirects"),
		event(&seq, 100, 0, "/tmp/work", trace.EventWrite, "/tmp/work/_build/cmake_install.cmake"),

		event(&seq, 110, 100, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc", trace.EventExec, "", "/usr/bin/gmake", "-f", "Makefile", "cmTC_deadbeef/fast"),
		event(&seq, 110, 0, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc", trace.EventRead, "/tmp/work/_build/CMakeFiles/pkgRedirects"),
		event(&seq, 110, 0, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc", trace.EventRead, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/Makefile"),
		event(&seq, 111, 110, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc", trace.EventExec, "", "/usr/bin/cc", "-o", "CMakeFiles/cmTC_deadbeef.dir/CheckIncludeFile.c.o", "-c", "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CheckIncludeFile.c"),
		event(&seq, 111, 0, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc", trace.EventRead, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CheckIncludeFile.c"),
		event(&seq, 111, 0, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc", trace.EventWrite, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects"),
		event(&seq, 111, 0, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc", trace.EventWrite, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/cmTC_deadbeef"),

		event(&seq, 200, 1, "/tmp/work", trace.EventExec, "", "cmake", "--build", "/tmp/work/_build", "--config", "Release"),
		event(&seq, 200, 0, "/tmp/work", trace.EventRead, "/tmp/work/_build/CMakeCache.txt"),
		event(&seq, 201, 200, "/tmp/work/_build", trace.EventExec, "", "/usr/bin/gmake", "-f", "Makefile"),
		event(&seq, 201, 0, "/tmp/work/_build", trace.EventRead, "/tmp/work/_build/Makefile"),
		event(&seq, 202, 201, "/tmp/work/_build", trace.EventExec, "", "/usr/bin/gmake", "-s", "-f", "CMakeFiles/tracecore.dir/build.make", "CMakeFiles/tracecore.dir/build"),
		event(&seq, 202, 0, "/tmp/work/_build", trace.EventRead, "/tmp/work/_build/CMakeFiles/tracecore.dir/build.make"),
		event(&seq, 203, 202, "/tmp/work/_build", trace.EventExec, "", compileArgv...),
		event(&seq, 203, 0, "/tmp/work/_build", trace.EventRead, "/tmp/work/core.c"),
		event(&seq, 203, 0, "/tmp/work/_build", trace.EventRead, "/tmp/work/trace.h"),
		event(&seq, 203, 0, "/tmp/work/_build", trace.EventRead, "/tmp/work/_build/trace_options.h"),
		event(&seq, 203, 0, "/tmp/work/_build", trace.EventWrite, "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o"),
		event(&seq, 204, 202, "/tmp/work/_build", trace.EventExec, "", "/usr/bin/ar", "qc", "/tmp/work/_build/libtracecore.a", "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o"),
		event(&seq, 204, 0, "/tmp/work/_build", trace.EventRead, "/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o"),
		event(&seq, 204, 0, "/tmp/work/_build", trace.EventWrite, "/tmp/work/_build/libtracecore.a"),
		event(&seq, 204, 0, "/tmp/work/_build", trace.EventWrite, "/tmp/work/_build/"+arTemp),
		event(&seq, 205, 202, "/tmp/work/_build", trace.EventExec, "", "/usr/bin/ranlib", "/tmp/work/_build/libtracecore.a"),
		event(&seq, 205, 0, "/tmp/work/_build", trace.EventRead, "/tmp/work/_build/libtracecore.a"),
		event(&seq, 205, 0, "/tmp/work/_build", trace.EventWrite, "/tmp/work/_build/"+ranlibTemp),
		event(&seq, 205, 0, "/tmp/work/_build", trace.EventWrite, "/tmp/work/_build/libtracecore.a"),
	}

	if cliOn {
		events = append(events,
			event(&seq, 206, 201, "/tmp/work/_build", trace.EventExec, "", "/usr/bin/gmake", "-s", "-f", "CMakeFiles/tracecli.dir/build.make", "CMakeFiles/tracecli.dir/build"),
			event(&seq, 206, 0, "/tmp/work/_build", trace.EventRead, "/tmp/work/_build/CMakeFiles/tracecli.dir/build.make"),
			event(&seq, 207, 206, "/tmp/work/_build", trace.EventExec, "", "/usr/bin/cc", "/tmp/work/cli.c", "/tmp/work/_build/libtracecore.a", "-o", "/tmp/work/_build/tracecli"),
			event(&seq, 207, 0, "/tmp/work/_build", trace.EventRead, "/tmp/work/cli.c"),
			event(&seq, 207, 0, "/tmp/work/_build", trace.EventRead, "/tmp/work/_build/libtracecore.a"),
			event(&seq, 207, 0, "/tmp/work/_build", trace.EventWrite, "/tmp/work/_build/tracecli"),
		)
	}

	events = append(events,
		event(&seq, 300, 1, "/tmp/work", trace.EventExec, "", "cmake", "--install", "/tmp/work/_build", "--prefix", "/tmp/work/install"),
		event(&seq, 300, 0, "/tmp/work", trace.EventRead, "/tmp/work/_build/cmake_install.cmake"),
		event(&seq, 300, 0, "/tmp/work", trace.EventRead, "/tmp/work/_build/libtracecore.a"),
		event(&seq, 300, 0, "/tmp/work", trace.EventRead, "/tmp/work/trace.h"),
		event(&seq, 300, 0, "/tmp/work", trace.EventRead, "/tmp/work/_build/trace_options.h"),
	)
	if cliOn {
		events = append(events, event(&seq, 300, 0, "/tmp/work", trace.EventRead, "/tmp/work/_build/tracecli"))
	}
	events = append(events,
		event(&seq, 300, 0, "/tmp/work", trace.EventWrite, "/tmp/work/install/lib/libtracecore.a"),
		event(&seq, 300, 0, "/tmp/work", trace.EventWrite, "/tmp/work/install/include/trace.h"),
		event(&seq, 300, 0, "/tmp/work", trace.EventWrite, "/tmp/work/install/include/trace_options.h"),
		event(&seq, 300, 0, "/tmp/work", trace.EventWrite, "/tmp/work/_build/install_manifest.txt"),
	)
	if cliOn {
		events = append(events, event(&seq, 300, 0, "/tmp/work", trace.EventWrite, "/tmp/work/install/bin/tracecli"))
	}
	if shipOn {
		events = append(events, event(&seq, 300, 0, "/tmp/work", trace.EventWrite, "/tmp/work/install/include/trace_alias.h"))
	}
	inputDigests := map[string]string{
		"/tmp/work/CMakeLists.txt":                                                        "src-cmakelists",
		"/tmp/work/trace_options.h.in":                                                    "src-trace-options-in",
		"/tmp/work/core.c":                                                                "src-core",
		"/tmp/work/trace.h":                                                               "src-trace-h",
		"/tmp/work/_build/CMakeCache.txt":                                                 "build-cmake-cache",
		"/tmp/work/_build/Makefile":                                                       "build-makefile",
		"/tmp/work/_build/CMakeFiles/pkgRedirects":                                        "build-pkgredirects",
		"/tmp/work/_build/CMakeFiles/tracecore.dir/build.make":                            "build-tracecore-make",
		"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/Makefile":                "try-makefile",
		"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CheckIncludeFile.c":      "try-source",
		"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects": "try-pkgredirects",
		"/tmp/work/_build/trace_options.h":                                                traceOptionsDigest,
		"/tmp/work/_build/CMakeFiles/tracecore.dir/core.c.o":                              libDigest + "-obj",
		"/tmp/work/_build/libtracecore.a":                                                 libDigest,
		"/tmp/work/_build/cmake_install.cmake":                                            installScriptDigest,
	}
	if cliOn {
		inputDigests["/tmp/work/cli.c"] = "src-cli"
		inputDigests["/tmp/work/_build/CMakeFiles/tracecli.dir/build.make"] = "build-tracecli-make"
		inputDigests["/tmp/work/_build/tracecli"] = "tracecli-on"
	}
	outputManifest := OutputManifest{
		Entries: map[string]OutputEntry{
			"include/trace.h":         {Kind: "file", Digest: "install-trace-h"},
			"include/trace_options.h": {Kind: "file", Digest: traceOptionsDigest},
			"lib/libtracecore.a":      {Kind: "archive", Digest: libDigest},
		},
	}
	if cliOn {
		outputManifest.Entries["bin/tracecli"] = OutputEntry{Kind: "file", Digest: "tracecli-on", Executable: true}
	}
	if shipOn {
		outputManifest.Entries["include/trace_alias.h"] = OutputEntry{Kind: "file", Digest: "trace-alias-on"}
	}
	return ProbeResult{
		Events:         events,
		Scope:          scope,
		InputDigests:   inputDigests,
		OutputManifest: outputManifest,
	}
}

func traceoptionsArchiveTemps(apiOn, cliOn, shipOn bool) (string, string) {
	switch {
	case apiOn:
		return "stCXhzz0", "stQugPSt"
	case cliOn:
		return "stizVeGp", "stiZkO0K"
	case shipOn:
		return "stNMeD5X", "stdwbVyX"
	default:
		return "stNjnHgT", "stvgaB7q"
	}
}

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

func TestWatchIgnoresConfigureSidecarsForShipOnlyOption(t *testing.T) {
	matrix := formula.Matrix{
		Options: map[string][]string{
			"api":  {"api-off", "api-on"},
			"cli":  {"cli-off", "cli-on"},
			"ship": {"ship-off", "ship-on"},
		},
		DefaultOptions: map[string][]string{
			"api":  {"api-off"},
			"cli":  {"cli-off"},
			"ship": {"ship-off"},
		},
	}
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	traces := map[string][]trace.Record{
		"api-off-cli-off-ship-off": {
			record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt"},
				[]string{
					"/tmp/work/_build/trace_options.h",
					"/tmp/work/_build/CMakeFiles/pkgRedirects",
					"/tmp/work/_build/cmake_install.cmake",
				}),
			record([]string{"cmake", "-E", "echo", "probe"}, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc",
				[]string{"/tmp/work/_build/CMakeFiles/pkgRedirects"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects"}),
			record([]string{"cc", "-c", "/tmp/work/src/core.c", "-o", "/tmp/work/_build/core.o"}, "/tmp/work/_build",
				[]string{"/tmp/work/src/core.c", "/tmp/work/_build/trace_options.h"},
				[]string{"/tmp/work/_build/core.o"}),
			record([]string{"ar", "rcs", "/tmp/work/_build/libtracecore.a", "/tmp/work/_build/core.o"}, "/tmp/work/_build",
				[]string{"/tmp/work/_build/core.o"},
				[]string{"/tmp/work/_build/libtracecore.a"}),
		},
		"api-on-cli-off-ship-off": {
			record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build", "-DAPI=ON"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt"},
				[]string{
					"/tmp/work/_build/trace_options.h",
					"/tmp/work/_build/CMakeFiles/pkgRedirects",
					"/tmp/work/_build/cmake_install.cmake",
				}),
			record([]string{"cmake", "-E", "echo", "probe"}, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc",
				[]string{"/tmp/work/_build/CMakeFiles/pkgRedirects"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects"}),
			record([]string{"cc", "-DAPI", "-c", "/tmp/work/src/core.c", "-o", "/tmp/work/_build/core.o"}, "/tmp/work/_build",
				[]string{"/tmp/work/src/core.c", "/tmp/work/_build/trace_options.h"},
				[]string{"/tmp/work/_build/core.o"}),
			record([]string{"ar", "rcs", "/tmp/work/_build/libtracecore.a", "/tmp/work/_build/core.o"}, "/tmp/work/_build",
				[]string{"/tmp/work/_build/core.o"},
				[]string{"/tmp/work/_build/libtracecore.a"}),
		},
		"api-off-cli-on-ship-off": {
			record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt"},
				[]string{
					"/tmp/work/_build/trace_options.h",
					"/tmp/work/_build/CMakeFiles/pkgRedirects",
					"/tmp/work/_build/cmake_install.cmake",
				}),
			record([]string{"cmake", "-E", "echo", "probe"}, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc",
				[]string{"/tmp/work/_build/CMakeFiles/pkgRedirects"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects"}),
			record([]string{"cc", "-c", "/tmp/work/src/core.c", "-o", "/tmp/work/_build/core.o"}, "/tmp/work/_build",
				[]string{"/tmp/work/src/core.c", "/tmp/work/_build/trace_options.h"},
				[]string{"/tmp/work/_build/core.o"}),
			record([]string{"ar", "rcs", "/tmp/work/_build/libtracecore.a", "/tmp/work/_build/core.o"}, "/tmp/work/_build",
				[]string{"/tmp/work/_build/core.o"},
				[]string{"/tmp/work/_build/libtracecore.a"}),
			record([]string{"cc", "/tmp/work/src/cli.c", "/tmp/work/_build/libtracecore.a", "-o", "/tmp/work/_build/tracecli"}, "/tmp/work/_build",
				[]string{"/tmp/work/src/cli.c", "/tmp/work/_build/libtracecore.a"},
				[]string{"/tmp/work/_build/tracecli"}),
		},
		"api-off-cli-off-ship-on": {
			record([]string{"cmake", "-S", "/tmp/work", "-B", "/tmp/work/_build"}, "/tmp/work",
				[]string{"/tmp/work/CMakeLists.txt"},
				[]string{
					"/tmp/work/_build/trace_options.h",
					"/tmp/work/_build/CMakeFiles/pkgRedirects",
					"/tmp/work/_build/cmake_install.cmake",
				}),
			record([]string{"cmake", "-E", "echo", "probe"}, "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc",
				[]string{"/tmp/work/_build/CMakeFiles/pkgRedirects"},
				[]string{"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects"}),
			record([]string{"cc", "-c", "/tmp/work/src/core.c", "-o", "/tmp/work/_build/core.o"}, "/tmp/work/_build",
				[]string{"/tmp/work/src/core.c", "/tmp/work/_build/trace_options.h"},
				[]string{"/tmp/work/_build/core.o"}),
			record([]string{"ar", "rcs", "/tmp/work/_build/libtracecore.a", "/tmp/work/_build/core.o"}, "/tmp/work/_build",
				[]string{"/tmp/work/_build/core.o"},
				[]string{"/tmp/work/_build/libtracecore.a"}),
			record([]string{"cmake", "--install", "/tmp/work/_build"}, "/tmp/work",
				[]string{
					"/tmp/work/_build/cmake_install.cmake",
					"/tmp/work/_build/libtracecore.a",
					"/tmp/work/_build/trace_options.h",
				},
				[]string{
					"/tmp/work/install/lib/libtracecore.a",
					"/tmp/work/install/include/trace_alias.h",
				}),
		},
	}

	got, trusted, err := Watch(context.Background(), matrix, func(_ context.Context, combo string) (ProbeResult, error) {
		return ProbeResult{Records: traces[combo], Scope: scope}, nil
	})
	if err != nil {
		t.Fatalf("Watch() unexpected error: %v", err)
	}
	if !trusted {
		t.Fatalf("Watch() trusted = false, want true")
	}

	want := []string{
		"api-off-cli-off-ship-off",
		"api-off-cli-off-ship-on",
		"api-off-cli-on-ship-off",
		"api-on-cli-off-ship-off",
		"api-on-cli-on-ship-off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}

func TestWatchIgnoresConfigureSidecarsForShipOnlyOptionWithEvents(t *testing.T) {
	matrix := formula.Matrix{
		Options: map[string][]string{
			"api":  {"api-off", "api-on"},
			"cli":  {"cli-off", "cli-on"},
			"ship": {"ship-off", "ship-on"},
		},
		DefaultOptions: map[string][]string{
			"api":  {"api-off"},
			"cli":  {"cli-off"},
			"ship": {"ship-off"},
		},
	}
	probes := map[string]ProbeResult{
		"api-off-cli-off-ship-off": traceoptionsEventProbe(false, false, false),
		"api-on-cli-off-ship-off":  traceoptionsEventProbe(true, false, false),
		"api-off-cli-on-ship-off":  traceoptionsEventProbe(false, true, false),
		"api-off-cli-off-ship-on":  traceoptionsEventProbe(false, false, true),
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
		"api-off-cli-off-ship-off",
		"api-off-cli-off-ship-on",
		"api-off-cli-on-ship-off",
		"api-on-cli-off-ship-off",
		"api-on-cli-on-ship-off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Watch() = %v, want %v", got, want)
	}
}
