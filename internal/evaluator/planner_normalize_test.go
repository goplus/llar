package evaluator

import (
	"testing"

	"github.com/goplus/llar/internal/trace"
)

func TestNormalizeScopeTokenHeuristicBuildNoise(t *testing.T) {
	scope := trace.Scope{
		SourceRoot:  "/tmp/work",
		BuildRoot:   "/tmp/work/_build",
		InstallRoot: "/tmp/work/install",
	}
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{
			name:  "scratch workspace child",
			token: "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/CMakeFiles/pkgRedirects",
			want:  "$BUILD/CMakeFiles/$TMPDIR/TryCompile-$ID/CMakeFiles/pkgRedirects",
		},
		{
			name:  "generated scratch artifact",
			token: "/tmp/work/_build/CMakeFiles/CMakeScratch/TryCompile-doc/cmTC_deadbeef",
			want:  "$BUILD/CMakeFiles/$TMPDIR/TryCompile-$ID/cmTC_$ID",
		},
		{
			name:  "generic temp subtree",
			token: "/tmp/work/_build/probe/tmp/job-doc/result_4f3e2d1c.dir",
			want:  "$BUILD/probe/$TMPDIR/job-$ID/result_$ID.dir",
		},
		{
			name:  "tmp pid suffix",
			token: "/tmp/work/_build/cache/output.tmp.12345",
			want:  "$BUILD/cache/output.tmp.$ID",
		},
		{
			name:  "stable build artifact",
			token: "/tmp/work/_build/libtracecore.a",
			want:  "$BUILD/libtracecore.a",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeScopeToken(tc.token, scope); got != tc.want {
				t.Fatalf("normalizeScopeToken(%q) = %q, want %q", tc.token, got, tc.want)
			}
		})
	}
}
