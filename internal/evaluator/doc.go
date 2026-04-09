// Package evaluator owns Stage 2 matrix planning on top of tracessa and the
// artifact-side utilities needed by synthesis, replay, and debug reporting.
//
// The package deliberately does not implement trace SSA internals. All trace
// normalization, SSA construction, role projection, and wavefront impact
// analysis live under internal/trace/ssa.
package evaluator
