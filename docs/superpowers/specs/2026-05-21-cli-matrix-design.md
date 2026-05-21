# CLI Matrix Flag Design

## Goal

`llar make` and `llar test` should accept target matrix dimensions from the
command line and pass the resulting matrix string into the existing module
load/build flow.

This PR is intentionally scoped to matrix transmission only. It must not change
cross compiler selection, sysroot injection, `glibc` resolution, toolchain
download behavior, formula APIs, or build internals.

## Commands

The same matrix syntax applies to:

```text
llar make <module@version>
llar test <module@version>
```

Examples:

```text
llar make madler/zlib@v1.3.1 --os linux --arch amd64
llar test test/cmaketest@1.0.0 --os linux --arch amd64
llar make madler/zlib@v1.3.1 --matrix-debug true
llar make madler/zlib@v1.3.1 --matrix-output custom
```

If no matrix flags are provided, behavior remains unchanged: the command uses
the current host matrix from `hostMatrixCombo()`.

## Flag Resolution

Parsing order:

1. Known command flags are handled by the command.
   - `llar make`: `--verbose`/`-v`, `--output`/`-o`, `--help`/`-h`.
   - `llar test`: `--verbose`/`-v`, `--help`/`-h`.
2. Unknown long flags become matrix dimensions.
   - `--os linux` means `matrix["os"] = "linux"`.
   - `--arch=amd64` means `matrix["arch"] = "amd64"`.
3. `--matrix-<key>` always becomes a matrix dimension, even if `<key>`
   conflicts with a known command flag.
   - `--matrix-output foo` means `matrix["output"] = "foo"`.
4. Unknown short flags are errors. They do not become matrix dimensions.

Matrix flags support both separated and equals forms:

```text
--os linux
--os=linux
--matrix-debug true
--matrix-debug=true
```

Bare boolean-style matrix flags are not supported. `--debug` without a value is
an error because matrix dimensions are key-value pairs.

`--` stops flag parsing. Remaining items are positional arguments; because
`make` and `test` accept exactly one module argument, extra positional items
will use the existing argument-count error path.

## Matrix Keys And Values

Matrix keys must match:

```text
[A-Za-z0-9_][A-Za-z0-9_.-]*
```

Values must be non-empty.

Errors:

```text
missing value for matrix flag --os
missing matrix key in --matrix-
invalid matrix key "<key>"
```

If a key appears multiple times, the last value wins:

```text
--os darwin --os linux
```

produces `os=linux`.

## Matrix String Encoding

The CLI parser produces a map of matrix dimensions and then encodes it into the
existing matrix string format used by `modules.Load` and `build.NewBuilder`.

Platform dimensions form the primary segment:

```text
arch=amd64, os=linux -> amd64-linux
arch=arm64           -> arm64
```

Non-platform dimensions are appended after `|`, sorted by key for stable cache
keys and install paths:

```text
arch=amd64, os=linux, debug=true
-> amd64-linux|debug=true

arch=amd64, os=linux, debug=true, variant=release
-> amd64-linux|debug=true,variant=release
```

This preserves the current crosscompile behavior because `buildtarget.Parse`
already ignores everything after `|`. `crosscompile` continues to inspect only
the platform segment, while build cache keys and install directories keep the
full matrix string.

## Data Flow

`runMake` and `runTest` both derive `matrixStr` through the same CLI matrix
parser.

The selected matrix is passed unchanged to:

```go
modules.Load(..., modules.Options{MatrixStr: matrixStr})
build.NewBuilder(build.Options{MatrixStr: matrixStr})
```

No changes are made to `modules`, `build`, `crosscompile`, formula APIs, or
sysroot handling in this PR.

## Tests

Required tests:

- `llar make` passes `--os linux --arch amd64` as `amd64-linux`.
- `llar test` passes `--os linux --arch amd64` as `amd64-linux`.
- Known command flags keep their command meaning, for example `--output out`.
- `--matrix-output foo` becomes matrix key `output=foo`.
- `--matrix-debug true` encodes as `|debug=true`.
- Unknown short flags fail.
- Missing matrix values fail.
- No matrix flags preserves host matrix behavior.
