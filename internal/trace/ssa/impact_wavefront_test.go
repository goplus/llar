package ssa

import (
	"testing"

	"github.com/goplus/llar/internal/trace"
)

func TestWavefrontDiffMarksDuplicateReadyCandidatesAmbiguous(t *testing.T) {
	baseRecords := []trace.Record{
		record([]string{"touch", "build/generated.stamp"}, "/tmp/work", nil, []string{"/tmp/work/build/generated.stamp"}),
		record([]string{"touch", "build/generated.stamp"}, "/tmp/work", nil, []string{"/tmp/work/build/generated.stamp"}),
	}
	probeRecords := []trace.Record{
		record([]string{"touch", "build/generated.stamp"}, "/tmp/work", nil, []string{"/tmp/work/build/generated.stamp"}),
	}

	base := BuildGraph(BuildInput{Records: baseRecords})
	probe := BuildGraph(BuildInput{Records: probeRecords})
	baseRoles := ProjectRoles(base)
	probeRoles := ProjectRoles(probe)
	path := normalizePath("/tmp/work/build/generated.stamp")

	diff := WavefrontDiffWithEvidence(base, probe, baseRoles, probeRoles, &ImpactEvidence{
		Changed: map[string]bool{path: false},
	})
	if !diff.Ambiguous {
		t.Fatalf("WavefrontDiffWithEvidence().Ambiguous = false, want true")
	}
	if diff.Matched != 0 {
		t.Fatalf("WavefrontDiffWithEvidence().Matched = %d, want 0", diff.Matched)
	}
	if len(diff.Pairs) != 0 {
		t.Fatalf("WavefrontDiffWithEvidence().Pairs = %v, want no forced pairing", diff.Pairs)
	}
	if len(diff.ProbeClass) != 1 {
		t.Fatalf("len(WavefrontDiffWithEvidence().ProbeClass) = %d, want 1", len(diff.ProbeClass))
	}
	if diff.ProbeClass[0] != WavefrontProbeUnknown {
		t.Fatalf("WavefrontDiffWithEvidence().ProbeClass[0] = %v, want %v", diff.ProbeClass[0], WavefrontProbeUnknown)
	}

	result := AnalyzeWithEvidence(AnalysisInput{
		Base:  AnalysisSideInput{Records: baseRecords},
		Probe: AnalysisSideInput{Records: probeRecords},
	}, &ImpactEvidence{
		Changed: map[string]bool{path: false},
	})
	if !result.Profile.Ambiguous {
		t.Fatalf("AnalyzeWithEvidence().Profile.Ambiguous = false, want true")
	}
}

func TestAnalyzeKeepsBaselineSharedStablePrereqInNeed(t *testing.T) {
	baseRecords := []trace.Record{
		record([]string{"gen", "flags.in", "-o", "build/flags.txt"}, "/tmp/work", []string{"/tmp/work/flags.in"}, []string{"/tmp/work/build/flags.txt"}),
		record(
			[]string{"cc", "-c", "main.c", "-include", "common.h", "build/flags.txt", "-o", "build/main.o"},
			"/tmp/work",
			[]string{"/tmp/work/main.c", "/tmp/work/common.h", "/tmp/work/build/flags.txt"},
			[]string{"/tmp/work/build/main.o"},
		),
	}
	probeRecords := []trace.Record{
		record([]string{"gen", "--variant=A", "flags.in", "-o", "build/flags.txt"}, "/tmp/work", []string{"/tmp/work/flags.in"}, []string{"/tmp/work/build/flags.txt"}),
		record(
			[]string{"cc", "-c", "main.c", "-include", "common.h", "build/flags.txt", "-o", "build/main.o"},
			"/tmp/work",
			[]string{"/tmp/work/main.c", "/tmp/work/common.h", "/tmp/work/build/flags.txt"},
			[]string{"/tmp/work/build/main.o"},
		),
	}

	result := Analyze(AnalysisInput{
		Base:  AnalysisSideInput{Records: baseRecords},
		Probe: AnalysisSideInput{Records: probeRecords},
	})

	if len(result.Debug.Wavefront.ProbeClass) != 2 {
		t.Fatalf("len(result.Debug.Wavefront.ProbeClass) = %d, want 2", len(result.Debug.Wavefront.ProbeClass))
	}
	if result.Debug.Wavefront.ProbeClass[0] != WavefrontProbeMutationRoot {
		t.Fatalf("probe class[0] = %v, want %v", result.Debug.Wavefront.ProbeClass[0], WavefrontProbeMutationRoot)
	}
	if result.Debug.Wavefront.ProbeClass[1] != WavefrontProbeFlow {
		t.Fatalf("probe class[1] = %v, want %v", result.Debug.Wavefront.ProbeClass[1], WavefrontProbeFlow)
	}

	commonH := normalizePath("/tmp/work/common.h")
	if _, ok := result.Profile.NeedPaths[commonH]; !ok {
		t.Fatalf("NeedPaths missing shared stable prereq %q: %v", commonH, result.Profile.NeedPaths)
	}
	commonHState := ImpactStateKey{Path: commonH}
	if _, ok := result.Profile.NeedStates[commonHState]; !ok {
		t.Fatalf("NeedStates missing shared stable prereq state %+v: %v", commonHState, result.Profile.NeedStates)
	}
}

func TestAnalyzeWithEventsIgnoresConfigureSidecarsInImpact(t *testing.T) {
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	result := Analyze(AnalysisInput{
		Base: AnalysisSideInput{
			Events: traceoptionsEventTrace(false),
			Scope:  scope,
		},
		Probe: AnalysisSideInput{
			Events: traceoptionsEventTrace(true),
			Scope:  scope,
		},
	})

	if _, ok := result.Profile.SeedWrites[normalizeScopeToken("/tmp/work/_build/trace_options.h", scope)]; !ok {
		t.Fatalf("SeedWrites missing trace_options.h: %v", result.Profile.SeedWrites)
	}
	if _, ok := result.Profile.SlicePaths[normalizeScopeToken("/tmp/work/_build/libtracecore.a", scope)]; !ok {
		t.Fatalf("SlicePaths missing libtracecore.a propagation: %v", result.Profile.SlicePaths)
	}
	for _, path := range []string{
		"/tmp/work/_build/CMakeFiles/pkgRedirects",
		"/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects",
		"/tmp/work/_build/cmake_install.cmake",
	} {
		key := normalizeScopeToken(path, scope)
		if _, ok := result.Profile.SeedWrites[key]; ok {
			t.Fatalf("SeedWrites unexpectedly contains configure sidecar %q: %v", key, result.Profile.SeedWrites)
		}
		if _, ok := result.Profile.SlicePaths[key]; ok {
			t.Fatalf("SlicePaths unexpectedly contains configure sidecar %q: %v", key, result.Profile.SlicePaths)
		}
		if _, ok := result.Profile.NeedPaths[key]; ok {
			t.Fatalf("NeedPaths unexpectedly contains configure sidecar %q: %v", key, result.Profile.NeedPaths)
		}
	}
}

func TestAnalyzeWithEventsIgnoresArchiveTempSidecarsInImpact(t *testing.T) {
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	result := Analyze(AnalysisInput{
		Base: AnalysisSideInput{
			Events: traceoptionsMatrixEventTrace(false, false, false),
			Scope:  scope,
		},
		Probe: AnalysisSideInput{
			Events: traceoptionsMatrixEventTrace(false, false, true),
			Scope:  scope,
		},
	})

	for _, path := range []string{
		"/tmp/work/_build/stNjnHgT",
		"/tmp/work/_build/stvgaB7q",
		"/tmp/work/_build/stNMeD5X",
		"/tmp/work/_build/stdwbVyX",
	} {
		key := normalizeScopeToken(path, scope)
		if _, ok := result.Profile.SeedWrites[key]; ok {
			t.Fatalf("SeedWrites unexpectedly contains %q: %v", key, result.Profile.SeedWrites)
		}
		if _, ok := result.Profile.NeedPaths[key]; ok {
			t.Fatalf("NeedPaths unexpectedly contains %q: %v", key, result.Profile.NeedPaths)
		}
		if _, ok := result.Profile.SlicePaths[key]; ok {
			t.Fatalf("SlicePaths unexpectedly contains %q: %v", key, result.Profile.SlicePaths)
		}
	}
}
