# CLI Matrix Flags Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add shared matrix flag parsing for `llar make` and `llar test`, so command-line matrix dimensions are transmitted to the existing `modules.Load` and `build.NewBuilder` flow.

**Architecture:** Keep the change in `cmd/llar/internal`. Add a small unexported parser that separates known command flags from matrix flags, encodes matrix dimensions into the existing matrix string, and returns cleaned args for Cobra command execution. Do not modify `build`, `modules`, `crosscompile`, formula APIs, toolchain download, sysroot behavior, or `glibc` behavior.

**Tech Stack:** Go 1.24, Cobra, existing LLAR matrix string format, `go test -ldflags="-checklinkname=0"`.

---

## File Structure

- Create `cmd/llar/internal/matrix_flags.go`
  - Owns matrix flag parsing, key validation, matrix encoding, and host-matrix fallback.
  - Contains only unexported helpers used by `make.go` and `test.go`.
- Create `cmd/llar/internal/matrix_flags_test.go`
  - Unit tests for matrix parser behavior independent of real builds.
- Modify `cmd/llar/internal/make.go`
  - Use the parser before existing module argument parsing.
  - Continue using known `make` flags for `--verbose/-v` and `--output/-o`.
- Modify `cmd/llar/internal/test.go`
  - Use the same parser before existing module argument parsing.
  - Continue using known `test` flags for `--verbose/-v`.
- Modify `cmd/llar/internal/make_test.go` and `cmd/llar/internal/test_test.go` only where needed to verify command integration. `llar test` runs root `onTest` and bypasses the root build cache, so command-level matrix parsing for `test` should be verified without relying on a cached root build result.

---

### Task 1: Matrix Parser

**Files:**
- Create: `cmd/llar/internal/matrix_flags.go`
- Create: `cmd/llar/internal/matrix_flags_test.go`

- [ ] **Step 1: Write parser tests**

Create `cmd/llar/internal/matrix_flags_test.go`:

```go
package internal

import (
	"runtime"
	"testing"
)

func TestParseMatrixArgsUnknownLongFlags(t *testing.T) {
	gotArgs, matrix, err := parseMatrixArgs([]string{"madler/zlib@v1.3.1", "--os", "linux", "--arch=amd64"}, knownMatrixFlags{})
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
	gotArgs, matrix, err := parseMatrixArgs([]string{"--output", "out", "-v", "--os", "linux", "--arch", "amd64", "madler/zlib@v1.3.1"}, knownMakeMatrixFlags())
	if err != nil {
		t.Fatalf("parseMatrixArgs: %v", err)
	}
	wantArgs := []string{"--output", "out", "-v", "madler/zlib@v1.3.1"}
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
	gotArgs, matrix, err := parseMatrixArgs([]string{"madler/zlib@v1.3.1", "--arch", "amd64", "--os", "linux", "--matrix-output", "custom", "--matrix-debug=true"}, knownMakeMatrixFlags())
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

func TestParseMatrixArgsNoMatrixUsesHost(t *testing.T) {
	_, matrix, err := parseMatrixArgs([]string{"madler/zlib@v1.3.1"}, knownMatrixFlags{})
	if err != nil {
		t.Fatalf("parseMatrixArgs: %v", err)
	}
	want := runtime.GOARCH + "-" + runtime.GOOS
	if matrix != want {
		t.Fatalf("matrix = %q, want host matrix %q", matrix, want)
	}
}

func TestParseMatrixArgsDuplicateKeyLastWins(t *testing.T) {
	_, matrix, err := parseMatrixArgs([]string{"madler/zlib@v1.3.1", "--os", "darwin", "--os", "linux", "--arch", "amd64"}, knownMatrixFlags{})
	if err != nil {
		t.Fatalf("parseMatrixArgs: %v", err)
	}
	if matrix != "amd64-linux" {
		t.Fatalf("matrix = %q, want amd64-linux", matrix)
	}
}

func TestParseMatrixArgsKnownShortFlagsStayInArgs(t *testing.T) {
	gotArgs, matrix, err := parseMatrixArgs([]string{"-v", "madler/zlib@v1.3.1", "--os", "linux", "--arch", "amd64"}, knownMakeMatrixFlags())
	if err != nil {
		t.Fatalf("parseMatrixArgs: %v", err)
	}
	wantArgs := []string{"-v", "madler/zlib@v1.3.1"}
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
	_, _, err := parseMatrixArgs([]string{"madler/zlib@v1.3.1", "-x", "linux"}, knownMakeMatrixFlags())
	if err == nil {
		t.Fatal("parseMatrixArgs error = nil, want unknown short flag error")
	}
}

func TestParseMatrixArgsMissingValueFails(t *testing.T) {
	_, _, err := parseMatrixArgs([]string{"madler/zlib@v1.3.1", "--os"}, knownMatrixFlags{})
	if err == nil {
		t.Fatal("parseMatrixArgs error = nil, want missing value error")
	}
}

func TestParseMatrixArgsInvalidMatrixKeyFails(t *testing.T) {
	_, _, err := parseMatrixArgs([]string{"madler/zlib@v1.3.1", "--matrix-", "value"}, knownMatrixFlags{})
	if err == nil {
		t.Fatal("parseMatrixArgs error = nil, want missing matrix key error")
	}
	_, _, err = parseMatrixArgs([]string{"madler/zlib@v1.3.1", "--matrix-@bad", "value"}, knownMatrixFlags{})
	if err == nil {
		t.Fatal("parseMatrixArgs error = nil, want invalid matrix key error")
	}
}
```

- [ ] **Step 2: Run parser tests to verify they fail**

Run:

```bash
go test -ldflags="-checklinkname=0" ./cmd/llar/internal -run 'TestParseMatrixArgs' -count=1
```

Expected: FAIL because `parseMatrixArgs` does not exist.

- [ ] **Step 3: Implement matrix parser**

Create `cmd/llar/internal/matrix_flags.go`:

```go
package internal

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var matrixKeyRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

type knownMatrixFlags struct {
	long       map[string]bool
	short      map[string]bool
	needsValue map[string]bool
}

func knownMakeMatrixFlags() knownMatrixFlags {
	return knownMatrixFlags{
		long:       map[string]bool{"help": true, "verbose": true, "output": true},
		short:      map[string]bool{"h": true, "v": true, "o": true},
		needsValue: map[string]bool{"output": true, "o": true},
	}
}

func knownTestMatrixFlags() knownMatrixFlags {
	return knownMatrixFlags{
		long:       map[string]bool{"help": true, "verbose": true},
		short:      map[string]bool{"h": true, "v": true},
		needsValue: map[string]bool{},
	}
}

func parseMatrixArgs(args []string, known knownMatrixFlags) ([]string, string, error) {
	matrix := map[string]string{}
	clean := make([]string, 0, len(args))
	parseFlags := true

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !parseFlags {
			clean = append(clean, arg)
			continue
		}
		if arg == "--" {
			parseFlags = false
			clean = append(clean, arg)
			continue
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			clean = append(clean, arg)
			continue
		}
		if strings.HasPrefix(arg, "--") {
			key, value, hasValue, err := splitLongFlag(arg)
			if err != nil {
				return nil, "", err
			}
			if strings.HasPrefix(key, "matrix-") {
				matrixKey := strings.TrimPrefix(key, "matrix-")
				if matrixKey == "" {
					return nil, "", fmt.Errorf("missing matrix key in --matrix-")
				}
				if !validMatrixKey(matrixKey) {
					return nil, "", fmt.Errorf("invalid matrix key %q", matrixKey)
				}
				if !hasValue {
					if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
						return nil, "", fmt.Errorf("missing value for matrix flag --%s", key)
					}
					i++
					value = args[i]
				}
				if value == "" {
					return nil, "", fmt.Errorf("missing value for matrix flag --%s", key)
				}
				matrix[matrixKey] = value
				continue
			}
			if known.long[key] {
				clean = append(clean, arg)
				if !hasValue && known.needsValue[key] {
					if i+1 >= len(args) {
						return nil, "", fmt.Errorf("missing value for --%s", key)
					}
					i++
					clean = append(clean, args[i])
				}
				continue
			}
			if !validMatrixKey(key) {
				return nil, "", fmt.Errorf("invalid matrix key %q", key)
			}
			if !hasValue {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
					return nil, "", fmt.Errorf("missing value for matrix flag --%s", key)
				}
				i++
				value = args[i]
			}
			if value == "" {
				return nil, "", fmt.Errorf("missing value for matrix flag --%s", key)
			}
			matrix[key] = value
			continue
		}
		key := strings.TrimPrefix(arg, "-")
		if known.short[key] {
			clean = append(clean, arg)
			if known.needsValue[key] {
				if i+1 >= len(args) {
					return nil, "", fmt.Errorf("missing value for %s", arg)
				}
				i++
				clean = append(clean, args[i])
			}
			continue
		}
		return nil, "", fmt.Errorf("unknown short flag %q", arg)
	}

	if len(matrix) == 0 {
		return clean, hostMatrixCombo(), nil
	}
	matrixStr, err := encodeMatrix(matrix)
	if err != nil {
		return nil, "", err
	}
	return clean, matrixStr, nil
}

func splitLongFlag(arg string) (key, value string, hasValue bool, err error) {
	body := strings.TrimPrefix(arg, "--")
	if body == "" {
		return "", "", false, fmt.Errorf("invalid flag %q", arg)
	}
	key, value, hasValue = strings.Cut(body, "=")
	if key == "" {
		return "", "", false, fmt.Errorf("invalid flag %q", arg)
	}
	return key, value, hasValue, nil
}

func validMatrixKey(key string) bool {
	return matrixKeyRE.MatchString(key)
}

func encodeMatrix(matrix map[string]string) (string, error) {
	arch := matrix["arch"]
	osName := matrix["os"]
	var primary string
	switch {
	case arch != "" && osName != "":
		primary = arch + "-" + osName
	case arch != "":
		primary = arch
	case osName != "":
		return "", fmt.Errorf("matrix requires arch when os is set")
	default:
		primary = hostMatrixCombo()
	}

	keys := make([]string, 0, len(matrix))
	for key := range matrix {
		if key == "arch" || key == "os" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return primary, nil
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+matrix[key])
	}
	return primary + "|" + strings.Join(parts, ","), nil
}
```

- [ ] **Step 4: Run parser tests to verify they pass**

Run:

```bash
go test -ldflags="-checklinkname=0" ./cmd/llar/internal -run 'TestParseMatrixArgs' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit parser**

Run:

```bash
git add cmd/llar/internal/matrix_flags.go cmd/llar/internal/matrix_flags_test.go
git commit -m "feat(cmd): parse matrix flags"
```

---

### Task 2: Integrate Matrix Parser Into make And test

**Files:**
- Modify: `cmd/llar/internal/make.go`
- Modify: `cmd/llar/internal/test.go`
- Modify: `cmd/llar/internal/make_test.go`
- Modify: `cmd/llar/internal/test_test.go`

- [ ] **Step 1: Add integration tests for `make` and `test` matrix transmission**

Append to `cmd/llar/internal/make_test.go`:

```go
func TestMakeMatrixFlagsPassedToBuild(t *testing.T) {
	formulaDir := setupLocalFormulas(t)
	withMockRemoteStore(t, repo.New(formulaDir, &noopVCSRepo{}))
	workspaceDir := isolatedWorkspaceDir(t)
	prepopulateCache(t, workspaceDir, "test/liba", "1.0.0", "amd64-linux", "-lMATRIX")

	out, err := runMakeCmd(t, "test/liba@1.0.0", "--os", "linux", "--arch", "amd64", "-v")
	if err != nil {
		t.Fatalf("llar make failed: %v", err)
	}
	if strings.TrimSpace(out) != "-lMATRIX" {
		t.Fatalf("output = %q, want -lMATRIX", out)
	}
}

func TestMakeMatrixExplicitPrefixPassedToBuild(t *testing.T) {
	formulaDir := setupLocalFormulas(t)
	withMockRemoteStore(t, repo.New(formulaDir, &noopVCSRepo{}))
	workspaceDir := isolatedWorkspaceDir(t)
	prepopulateCache(t, workspaceDir, "test/liba", "1.0.0", "amd64-linux|debug=true", "-lDEBUG")

	out, err := runMakeCmd(t, "test/liba@1.0.0", "--arch", "amd64", "--os", "linux", "--matrix-debug", "true", "-v")
	if err != nil {
		t.Fatalf("llar make failed: %v", err)
	}
	if strings.TrimSpace(out) != "-lDEBUG" {
		t.Fatalf("output = %q, want -lDEBUG", out)
	}
}
```

Append to `cmd/llar/internal/test_test.go`:

```go
func TestTestMatrixFlagsPassedToBuild(t *testing.T) {
	cleanArgs, matrix, err := parseMatrixArgs([]string{"test/liba@1.0.0", "--os", "linux", "--arch", "amd64", "-v"}, knownTestMatrixFlags())
	if err != nil {
		t.Fatalf("parseMatrixArgs: %v", err)
	}
	if matrix != "amd64-linux" {
		t.Fatalf("matrix = %q, want amd64-linux", matrix)
	}
	wantArgs := []string{"test/liba@1.0.0", "-v"}
	if len(cleanArgs) != len(wantArgs) {
		t.Fatalf("args = %#v, want %#v", cleanArgs, wantArgs)
	}
	for i := range wantArgs {
		if cleanArgs[i] != wantArgs[i] {
			t.Fatalf("args = %#v, want %#v", cleanArgs, wantArgs)
		}
	}
}
```

If these helpers are not available in `test_test.go`, reuse the same package-level helpers already defined in `make_test.go`; both test files are in package `internal`.

- [ ] **Step 2: Run integration tests to verify they fail**

Run:

```bash
go test -ldflags="-checklinkname=0" ./cmd/llar/internal -run 'TestMakeMatrix|TestTestMatrix' -count=1
```

Expected: FAIL because `runMake` and `runTest` still use `hostMatrixCombo()` and Cobra still rejects unknown matrix flags.

- [ ] **Step 3: Integrate parser in `make.go`**

Modify `cmd/llar/internal/make.go`:

```go
var makeCmd = &cobra.Command{
	Use:                "make [module@version]",
	Short:              "Build a module to FormulaDir",
	Long:               `Make downloads and builds a module to FormulaDir.`,
	DisableFlagParsing: true,
	RunE:               runMake,
}
```

Then update the start of `runMake`:

```go
func runMake(cmd *cobra.Command, args []string) error {
	cleanArgs, matrixStr, err := parseMatrixArgs(args, knownMakeMatrixFlags())
	if err != nil {
		return err
	}
	cmd.Flags().Parse(cleanArgs)
	if cmd.Flags().Changed("help") {
		return cmd.Help()
	}
	args = cmd.Flags().Args()
	if err := cobra.ExactArgs(1)(cmd, args); err != nil {
		return err
	}

	pattern, version, isLocal, err := parseModuleArg(args[0])
	if err != nil {
		return err
	}
```

Remove the existing line:

```go
matrixStr := hostMatrixCombo()
```

Keep the rest of `runMake` unchanged.

- [ ] **Step 4: Integrate parser in `test.go`**

Modify `cmd/llar/internal/test.go`:

```go
var testCmd = &cobra.Command{
	Use:                "test [module@version]",
	Short:              "Build a module and run its onTest hook",
	Long:               `Test builds a module the same way as 'llar make', then executes
the module's onTest callback on the resulting artifacts.

The build cache is consulted as usual: if the module has already been built
with the same matrix, onBuild is skipped and onTest runs against the cached
artifacts. On a cache miss, onBuild runs and its result is cached for later
invocations before onTest executes.`,
	DisableFlagParsing: true,
	RunE:               runTest,
}
```

Then update the start of `runTest`:

```go
func runTest(cmd *cobra.Command, args []string) error {
	cleanArgs, matrixStr, err := parseMatrixArgs(args, knownTestMatrixFlags())
	if err != nil {
		return err
	}
	cmd.Flags().Parse(cleanArgs)
	if cmd.Flags().Changed("help") {
		return cmd.Help()
	}
	args = cmd.Flags().Args()
	if err := cobra.ExactArgs(1)(cmd, args); err != nil {
		return err
	}

	pattern, version, isLocal, err := parseModuleArg(args[0])
	if err != nil {
		return err
	}
```

Remove the existing line:

```go
matrixStr := hostMatrixCombo()
```

Keep the rest of `runTest` unchanged.

- [ ] **Step 5: Run integration tests to verify they pass**

Run:

```bash
go test -ldflags="-checklinkname=0" ./cmd/llar/internal -run 'TestMakeMatrix|TestTestMatrix' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit integration**

Run:

```bash
git add cmd/llar/internal/make.go cmd/llar/internal/test.go cmd/llar/internal/make_test.go cmd/llar/internal/test_test.go
git commit -m "feat(cmd): pass matrix flags to build"
```

---

### Task 3: Regression And Verification

**Files:**
- No new files expected.
- Modify tests only if existing tests need updated argument setup because `make` and `test` use `DisableFlagParsing`.

- [ ] **Step 1: Run targeted command tests**

Run:

```bash
go test -ldflags="-checklinkname=0" ./cmd/llar/internal -count=1
```

Expected: PASS, except if the local cache permission problem at `/Users/haolan/Library/Caches/.llar/workspaces` is still present. If that environment issue appears, rerun with an isolated cache:

```bash
GOCACHE="$(mktemp -d)" go test -ldflags="-checklinkname=0" ./cmd/llar/internal -count=1
```

If the failure still references `LLAR` workspace permissions rather than Go cache permissions, do not change production code. Record the failure and run narrower tests from Tasks 1 and 2.

- [ ] **Step 2: Run broad relevant tests**

Run:

```bash
go test -ldflags="-checklinkname=0" ./internal/build ./internal/modules ./cmd/llar/internal
```

Expected: PASS for build/modules and PASS for cmd unless blocked by the existing local workspace permission issue.

- [ ] **Step 3: Run build check**

Run:

```bash
go build ./internal/build ./internal/modules ./cmd/llar/internal
```

Expected: PASS.

- [ ] **Step 4: Manual CLI smoke checks**

Run:

```bash
go run -ldflags="-checklinkname=0" ./cmd/llar make --help
go run -ldflags="-checklinkname=0" ./cmd/llar test --help
```

Expected: both commands print help and exit successfully.

- [ ] **Step 5: Commit any test-only adjustments**

If Task 3 required changes to existing tests, run:

```bash
git add cmd/llar/internal
git commit -m "test(cmd): cover matrix flag regressions"
```

If no files changed, skip this step.

---

## Self-Review Notes

- Spec coverage:
  - `make` and `test` both covered in Task 2.
  - Unknown long flags and `--matrix-<key>` covered in Task 1.
  - Known command flag priority covered in Task 1.
  - Matrix encoding with `|` extension covered in Task 1.
  - No changes to build/modules/crosscompile enforced by file scope.
- Placeholder scan: no placeholder or unspecified implementation steps remain.
- Type consistency:
  - `parseMatrixArgs(args []string, known knownMatrixFlags) ([]string, string, error)` is used consistently.
  - Matrix encoding stays string-based to match existing `modules.Load` and `build.NewBuilder`.
