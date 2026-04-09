package ssa

import (
	"slices"
	"testing"

	"github.com/goplus/llar/internal/trace"
)

func TestBuildGraphExpandsEnvIntoInitialReadStates(t *testing.T) {
	graph := BuildGraph(BuildInput{
		Records: []trace.Record{{
			Argv:    []string{"cc", "-c", "core.c", "-o", "build/core.o"},
			Env:     []string{"PWD=/tmp/work", "CFLAGS=-O2", "TMPDIR=/tmp/work/.tmp"},
			Cwd:     "/tmp/work",
			Inputs:  []string{"/tmp/work/core.c"},
			Changes: []string{"/tmp/work/build/core.o"},
		}},
		Scope: trace.Scope{
			SourceRoot: "/tmp/work",
			BuildRoot:  "/tmp/work/build",
		},
	})

	if len(graph.Actions) != 1 {
		t.Fatalf("len(graph.Actions) = %d, want 1", len(graph.Actions))
	}
	if got := graph.Actions[0].Env; !slices.Equal(got, []string{"CFLAGS=-O2", "TMPDIR=$SRC/.tmp"}) {
		t.Fatalf("graph.Actions[0].Env = %v, want [CFLAGS=-O2 TMPDIR=$SRC/.tmp]", got)
	}

	envPath := envStatePath("CFLAGS")
	facts, ok := graph.Paths[envPath]
	if !ok {
		t.Fatalf("graph.Paths missing %q", envPath)
	}
	if len(facts.Readers) != 1 || facts.Readers[0] != 0 {
		t.Fatalf("graph.Paths[%q].Readers = %v, want [0]", envPath, facts.Readers)
	}

	found := false
	for _, read := range graph.ActionReads[0] {
		if read.Path != envPath {
			continue
		}
		found = true
		if len(read.Defs) != 1 {
			t.Fatalf("env read defs = %v, want one initial def", read.Defs)
		}
		if read.Defs[0].Writer != -1 || read.Defs[0].Path != envPath || read.Defs[0].Missing || read.Defs[0].Tombstone {
			t.Fatalf("env read def = %+v, want initial non-missing def", read.Defs[0])
		}
	}
	if !found {
		t.Fatalf("graph.ActionReads[0] missing env binding for %q: %v", envPath, graph.ActionReads[0])
	}
}

func TestAnalyzeTreatsEnvFootprintAsWavefrontInput(t *testing.T) {
	base := []trace.Record{{
		Argv:    []string{"cc", "-c", "core.c", "-o", "build/core.o"},
		Env:     []string{"CFLAGS=-O0"},
		Cwd:     "/tmp/work",
		Inputs:  []string{"/tmp/work/core.c"},
		Changes: []string{"/tmp/work/build/core.o"},
	}}
	probe := []trace.Record{{
		Argv:    []string{"cc", "-c", "core.c", "-o", "build/core.o"},
		Env:     []string{"CFLAGS=-O3"},
		Cwd:     "/tmp/work",
		Inputs:  []string{"/tmp/work/core.c"},
		Changes: []string{"/tmp/work/build/core.o"},
	}}

	result := AnalyzeWithEvidence(AnalysisInput{
		Base:  AnalysisSideInput{Records: base},
		Probe: AnalysisSideInput{Records: probe},
	}, &ImpactEvidence{
		Changed: map[string]bool{normalizePath("/tmp/work/build/core.o"): false},
	})

	if len(result.Debug.Wavefront.ProbeClass) != 1 {
		t.Fatalf("len(result.Debug.Wavefront.ProbeClass) = %d, want 1", len(result.Debug.Wavefront.ProbeClass))
	}
	if result.Debug.Wavefront.ProbeClass[0] != WavefrontProbeMutationRoot {
		t.Fatalf("result.Debug.Wavefront.ProbeClass[0] = %v, want %v", result.Debug.Wavefront.ProbeClass[0], WavefrontProbeMutationRoot)
	}
}
