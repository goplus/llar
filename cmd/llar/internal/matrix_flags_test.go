package internal

import (
	"runtime"
	"testing"

	"github.com/spf13/pflag"
)

func emptyMatrixFlagSet() *pflag.FlagSet {
	return pflag.NewFlagSet("test", pflag.ContinueOnError)
}

func makeMatrixFlagSet() *pflag.FlagSet {
	flags := emptyMatrixFlagSet()
	flags.BoolP("verbose", "v", false, "")
	flags.StringP("output", "o", "", "")
	return flags
}

func TestParseMatrixArgsUnknownLongFlags(t *testing.T) {
	gotArgs, matrix, err := parseMatrixArgs([]string{"madler/zlib@v1.3.1", "--os", "linux", "--arch=amd64"}, emptyMatrixFlagSet())
	if err != nil {
		t.Fatalf("parseMatrixArgs: %v", err)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "madler/zlib@v1.3.1" {
		t.Fatalf("args = %#v, want module arg only", gotArgs)
	}
	if matrix != "amd64-linux" {
		t.Fatalf("matrix = %q, want amd64-linux", matrix)
	}
}

func TestParseMatrixArgsKnownFlagsStayInArgs(t *testing.T) {
	gotArgs, matrix, err := parseMatrixArgs([]string{"--output", "out", "-v", "--os", "linux", "--arch", "amd64", "madler/zlib@v1.3.1"}, makeMatrixFlagSet())
	if err != nil {
		t.Fatalf("parseMatrixArgs: %v", err)
	}
	wantArgs := []string{"madler/zlib@v1.3.1"}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
		}
	}
	if matrix != "amd64-linux" {
		t.Fatalf("matrix = %q, want amd64-linux", matrix)
	}
}

func TestParseMatrixArgsExplicitMatrixPrefix(t *testing.T) {
	gotArgs, matrix, err := parseMatrixArgs([]string{"madler/zlib@v1.3.1", "--arch", "amd64", "--os", "linux", "--matrix-output", "custom", "--matrix-debug=true"}, makeMatrixFlagSet())
	if err != nil {
		t.Fatalf("parseMatrixArgs: %v", err)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "madler/zlib@v1.3.1" {
		t.Fatalf("args = %#v, want module arg only", gotArgs)
	}
	if matrix != "amd64-linux|debug=true,output=custom" {
		t.Fatalf("matrix = %q, want amd64-linux|debug=true,output=custom", matrix)
	}
}

func TestParseMatrixArgsUsesPflagForMatrixValues(t *testing.T) {
	gotArgs, matrix, err := parseMatrixArgs([]string{"--output=out", "madler/zlib@v1.3.1", "--arch", "amd64", "--os=linux", "--matrix-output", "custom"}, makeMatrixFlagSet())
	if err != nil {
		t.Fatalf("parseMatrixArgs: %v", err)
	}
	wantArgs := []string{"madler/zlib@v1.3.1"}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
		}
	}
	if matrix != "amd64-linux|output=custom" {
		t.Fatalf("matrix = %q, want amd64-linux|output=custom", matrix)
	}
}

func TestParseMatrixArgsKnownFlagValueCanLookLikeMatrixFlag(t *testing.T) {
	gotArgs, matrix, err := parseMatrixArgs([]string{"--output", "--os", "madler/zlib@v1.3.1", "--arch", "amd64"}, makeMatrixFlagSet())
	if err != nil {
		t.Fatalf("parseMatrixArgs: %v", err)
	}
	wantArgs := []string{"madler/zlib@v1.3.1"}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
		}
	}
	if matrix != "amd64" {
		t.Fatalf("matrix = %q, want amd64", matrix)
	}
}

func TestParseMatrixArgsNoMatrixUsesHost(t *testing.T) {
	_, matrix, err := parseMatrixArgs([]string{"madler/zlib@v1.3.1"}, emptyMatrixFlagSet())
	if err != nil {
		t.Fatalf("parseMatrixArgs: %v", err)
	}
	want := runtime.GOARCH + "-" + runtime.GOOS
	if matrix != want {
		t.Fatalf("matrix = %q, want host matrix %q", matrix, want)
	}
}

func TestParseMatrixArgsDuplicateKeyLastWins(t *testing.T) {
	_, matrix, err := parseMatrixArgs([]string{"madler/zlib@v1.3.1", "--os", "darwin", "--os", "linux", "--arch", "amd64"}, emptyMatrixFlagSet())
	if err != nil {
		t.Fatalf("parseMatrixArgs: %v", err)
	}
	if matrix != "amd64-linux" {
		t.Fatalf("matrix = %q, want amd64-linux", matrix)
	}
}

func TestParseMatrixArgsKnownShortFlagsStayInArgs(t *testing.T) {
	gotArgs, matrix, err := parseMatrixArgs([]string{"-v", "madler/zlib@v1.3.1", "--os", "linux", "--arch", "amd64"}, makeMatrixFlagSet())
	if err != nil {
		t.Fatalf("parseMatrixArgs: %v", err)
	}
	wantArgs := []string{"madler/zlib@v1.3.1"}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
		}
	}
	if matrix != "amd64-linux" {
		t.Fatalf("matrix = %q, want amd64-linux", matrix)
	}
}

func TestParseMatrixArgsUnknownShortFlagFails(t *testing.T) {
	_, _, err := parseMatrixArgs([]string{"madler/zlib@v1.3.1", "-x", "linux"}, makeMatrixFlagSet())
	if err == nil {
		t.Fatal("parseMatrixArgs error = nil, want unknown short flag error")
	}
	_, _, err = parseMatrixArgs([]string{"madler/zlib@v1.3.1", "-abc"}, makeMatrixFlagSet())
	if err == nil {
		t.Fatal("parseMatrixArgs error = nil, want grouped unknown short flag error")
	}
}

func TestParseMatrixArgsMissingValueFails(t *testing.T) {
	_, _, err := parseMatrixArgs([]string{"madler/zlib@v1.3.1", "--os"}, emptyMatrixFlagSet())
	if err == nil {
		t.Fatal("parseMatrixArgs error = nil, want missing value error")
	}
}

func TestParseMatrixArgsInvalidMatrixKeyFails(t *testing.T) {
	_, _, err := parseMatrixArgs([]string{"madler/zlib@v1.3.1", "--matrix-", "value"}, emptyMatrixFlagSet())
	if err == nil {
		t.Fatal("parseMatrixArgs error = nil, want missing matrix key error")
	}
	_, _, err = parseMatrixArgs([]string{"madler/zlib@v1.3.1", "--matrix-@bad", "value"}, emptyMatrixFlagSet())
	if err == nil {
		t.Fatal("parseMatrixArgs error = nil, want invalid matrix key error")
	}
}
