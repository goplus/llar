package evaluator

import (
	"context"
	"slices"
)

type OutputSynthesisMode string

const (
	OutputSynthesisModeDirectMerge OutputSynthesisMode = "direct-merge"
	OutputSynthesisModeRootReplay  OutputSynthesisMode = "root-replay"
)

type OutputSynthesisIssue = OutputMergeIssue
type OutputSynthesisIssueKind = OutputMergeIssueKind

type RootReplaySummary struct {
	CandidateRoots   int
	EligibleRoots    int
	ChangedRoots     []string
	SelectedRoots    []string
	SelectedCommands []string
	SelectedWrites   int
	Unavailable      string
}

type OutputSynthesisResult struct {
	Mode     OutputSynthesisMode
	Status   OutputMergeStatus
	Root     string
	Metadata string
	Manifest OutputManifest
	Issues   []OutputSynthesisIssue
	Replay   *RootReplaySummary
}

func (r OutputSynthesisResult) Clean() bool {
	return r.Status == OutputMergeStatusMerged
}

func (r OutputSynthesisResult) NeedsRebuild() bool {
	return r.Status == OutputMergeStatusNeedsRebuild
}

func (r OutputSynthesisResult) AsMergeResult() (OutputMergeResult, bool) {
	if r.Mode != OutputSynthesisModeDirectMerge {
		return OutputMergeResult{}, false
	}
	return OutputMergeResult{
		Status:   r.Status,
		Root:     r.Root,
		Metadata: r.Metadata,
		Manifest: r.Manifest,
		Issues:   r.Issues,
	}, true
}

func synthesizeOutputTrees(ctx context.Context, base, left, right ProbeResult) (OutputSynthesisResult, error) {
	mergeResult, err := MergeOutputTrees(
		base.OutputDir,
		base.OutputManifest,
		left.OutputDir,
		left.OutputManifest,
		right.OutputDir,
		right.OutputManifest,
	)
	if err != nil {
		return OutputSynthesisResult{}, err
	}
	if mergeResult.Clean() {
		return synthesisResultFromMerge(mergeResult), nil
	}

	replayResult, err := synthesizeByRootReplay(ctx, base, left, right)
	if err != nil {
		return OutputSynthesisResult{}, err
	}
	if replayResult.Clean() {
		return replayResult, nil
	}
	replayResult.Issues = append(slices.Clone(mergeResult.Issues), replayResult.Issues...)
	return replayResult, nil
}

func synthesisResultFromMerge(mergeResult OutputMergeResult) OutputSynthesisResult {
	return OutputSynthesisResult{
		Mode:     OutputSynthesisModeDirectMerge,
		Status:   mergeResult.Status,
		Root:     mergeResult.Root,
		Metadata: mergeResult.Metadata,
		Manifest: mergeResult.Manifest,
		Issues:   mergeResult.Issues,
	}
}
