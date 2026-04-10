package internal

import (
	"strings"
	"testing"

	"github.com/goplus/llar/internal/trace"
)

func TestFormatTraceDump(t *testing.T) {
	got := formatTraceDump("madler/zlib@v1.3.1", "linux-amd64", []trace.Record{
		{
			Argv:    []string{"cmake", "--build", "build"},
			Cwd:     "/tmp/zlib",
			Inputs:  []string{"/tmp/zlib/CMakeLists.txt"},
			Changes: []string{"/tmp/zlib/build/libz.a"},
		},
	}, map[string]string{
		"/tmp/zlib/build/config.h": "aaaaaaaaaaaaaaaa",
	})

	checks := []string{
		"TRACE madler/zlib@v1.3.1 [linux-amd64]",
		"DIGESTS",
		"/tmp/zlib/build/config.h = aaaaaaaaaaaaaaaa",
		"1. argv: cmake --build build",
		"cwd: /tmp/zlib",
		"inputs: /tmp/zlib/CMakeLists.txt",
		"changes: /tmp/zlib/build/libz.a",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Fatalf("formatTraceDump() missing %q in %q", want, got)
		}
	}
}

func TestFormatTraceDumpEmpty(t *testing.T) {
	got := formatTraceDump("mod", "combo", nil, nil)
	if !strings.Contains(got, "(no records)") {
		t.Fatalf("formatTraceDump() = %q, want empty marker", got)
	}
}
