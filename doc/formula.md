LLAR Formula
====

## Example

### cJSON_llar.gox

```go
id "DaveGamble/cJSON"

fromVer "v1.0.0"

// Define build matrix: 'require' propagates downward, 'options' is limited to the current package
matrix require = {
        "os": ["linux", "darwin"],
        "arch": ["x86_64", "arm64"],
        "lang": ["c"]
}, options = {
        "tests": ["ON", "OFF"]
}

filter matrix => {
    // Filter out invalid matrix combinations
    if matrix.require["os"] == "darwin" && matrix.require["arch"] == "mips" {
        return false  // Exclude macOS + MIPS combination
    }
    return true
}


onRequire (proj, deps) => {  // extract deps from this project
    makefile := proj.readFile("CMakeLists.txt")

    // find_package(re2c 2.0 REQUIRED)  -> {name: "re2c", version: "2.0"}
    // find_package(zlib REQUIRED)      -> {name: "zlib", version: ""}
    matches := findDeps(makefile)  // return [{name: "re2c", version: "2.0"}, {name: "zlib", version: ""}]

    for m in matches {
        deps.require(moduleID(m.Name), m.Version)
    }
}

onBuild (proj, out) => {
    echo "Building cJSON", proj.version, "for", proj.matrix.os, "/", proj.matrix.arch

    cmake "-DBUILD_SHARED_LIBS=OFF", \
          "-DENABLE_CJSON_TEST=OFF", \
          "-DCMAKE_INSTALL_PREFIX=${proj.dir}", \
          "."

    cmake "--build", "."
    cmake "--install", "."

    out.setCompileFlags("-I${proj.dir}/include")
    out.setLinkFlags(
        "-L${proj.dir}/lib",
        "-lcjson"
    )
}
```

### cJSON_cmp.gox

```go
compareVer (v1, v2) => {
    return semver.Compare(v1, v2)
}
```

## Instructions

### id

Declares the pkg id in `owner/repo` format

```go
id "DaveGamble/cJSON"
```

### fromVer

Declares the starting version this formula applies to. LLAR selects the formula with the maximum fromVer where `fromVer <= target version`.

**Example**: For target version `v1.7.0`, if available formulas have fromVer `v1.0.0` and `v1.5.0`, LLAR selects `v1.5.0`

```go
fromVer "v1.5.0"  // handles package versions >= 1.5.0
```


### Formula

Formula interface provides access to package metadata and event registration.

```go
type Formula interface {
    // Id - supports overloading
    Id__0() string          // Returns the current package id (owner/repo format)
    Id__1(id string)        // Declares the package id

    // FromVer - supports overloading
    FromVer__0() string     // Returns the starting version this formula applies to
    FromVer__1(ver string)  // Declares the starting version

    // Matrix - supports overloading
    Matrix__0() matrix.Matrix      // Returns the current build matrix
    Matrix__1(m matrix.Matrix)     // Declares the build matrix

    // Version - returns the current package version
    Version() version.Version

    // OnRequire - registers dependency extraction callback (optional)
    OnRequire(fn func(proj Project, deps deps.Graph) error)

    // OnBuild - registers build execution callback (required)
    OnBuild(fn func(proj Project, out Output) error)

    // Filter - registers matrix filter callback (optional)
    Filter(fn func(m matrix.Matrix) bool)
}
```

### Output

Output interface provides build output operations.

```go
type Output interface {
    // SetCompileFlags sets the compile flags for this build output
    // flags: array of compile flags (e.g., "-I/path/include")
    SetCompileFlags(flags ...string)

    // SetLinkFlags sets the link flags for this build output
    // flags: array of link flags (e.g., "-L/path/lib", "-lname")
    SetLinkFlags(flags ...string)
}
```

### Type Definitions

Common type definitions used in formulas:

```go
type Version struct {
    Version string
}

type Matrix struct {
    Require map[string][]string `json:"require"`
    Options map[string][]string `json:"options"`
    DefaultOptions map[string][]string `json:"defaultOptions"`
}

type Dependency struct {
    ID        string `json:"id"`
    Version   string `json:"version"`
}

type Graph interface {
    // Require specifies a package dependency
    Require(id string, ver version.Version)
    BuildList() []Dependency
}
```

**Note**: Although the first parameter of events `onRequire` and `onBuild` are both named `proj`, they may be of different types and are not necessarily interfaces.

### onRequire

Extracts dependency declarations from project source code. This is an optional callback.

**Parameters**:
- `proj`: Project context with filesystem and dependency graph access
  - `proj.readFile(path)`: Read file content
  - `proj.matrix`: Current build matrix
- `deps`: Dependency declaration interface
  - `deps.require(packageID, version)`: Declare a dependency

**Workflow**:
1. Parse dependencies from build system files (e.g., CMakeLists.txt)
2. Use `deps.require()` to declare dependencies
3. If dependency has no version (version is empty), system falls back to versions.json
4. If onRequire is not implemented, system uses versions.json directly

**Example**:
```go
onRequire (proj, deps) => {
    makefile := proj.readFile("CMakeLists.txt")
    matches := findDeps(makefile)

    for m in matches {
        deps.require(moduleID(m.Name), m.Version)
    }
}
```

### onBuild

Executes the project build and sets link flags.

**Parameters**:
- `proj`: Project context
  - `proj.matrix`: Current build matrix (contains os, arch, lang, etc.)
  - `proj.dir`: Project source and installation directory
- `out`: Build output interface
  - `out.setCompileFlags(flags...)`: Set compile flags (e.g., -I)
  - `out.setLinkFlags(flags...)`: Set link flags (e.g., -L, -l)

**Build Requirements**:
- LLAR recommends using static libraries (`.a` files)
- Recommended to disable shared library builds (`-DBUILD_SHARED_LIBS=OFF`)
- Use `out.setCompileFlags()` to set include paths
- Use `out.setLinkFlags()` to set library paths and libraries

**Example**:
```go
onBuild (proj, out) => {
    // Configure build (command-style call, no parentheses)
    cmake "-DBUILD_SHARED_LIBS=OFF", "-DCMAKE_INSTALL_PREFIX=${proj.dir}", "."

    // Execute build
    cmake "--build", "."
    cmake "--install", "."

    // Set compile and link flags
    out.setCompileFlags("-I${proj.dir}/include")
    out.setLinkFlags("-L${proj.dir}/lib", "-lninja")
}
```

## Version Comparison

The `{{repo}}_cmp.gox` file defines version comparison logic for version sorting and selection.

**Return Values**:
- `-1`: v1 < v2
- `0`: v1 == v2
- `1`: v1 > v2

**Example**:
```go
// ninja_cmp.gox
compareVer (v1, v2) => {
    return semver.Compare(v1, v2)
}
```

## Cross-Formula Calls

Use `import(packageID)` to import other formulas.

**Returns**: Formula interface instance

**Example**:
```go
depFormula := import("madler/zlib")
```

## Built-in Helper Functions

LLAR formulas provide several built-in helper functions for common operations:

### semver.Compare

Semantic version comparison function from Go's `golang.org/x/mod/semver` module (auto-imported).

**Signature**: `semver.Compare(v1 string, v2 string) int`

**Parameters**:
- `v1`: First version string
- `v2`: Second version string

**Returns**:
- `-1`: v1 < v2
- `0`: v1 == v2
- `1`: v1 > v2

**Example**:
```go
compareVer (v1, v2) => {
    return semver.Compare(v1, v2)
}

result := semver.Compare("v1.2.3", "v1.3.0")  // Returns -1
```

## Build Matrix

Build matrix represents all possible build configurations for a package.

### Matrix Structure

```go
matrix {
    Require: {
        "arch": ["x86_64", "arm64"],
        "lang": ["c", "cpp"],
        "os": ["linux", "darwin"]
    },
    Options: {
        "zlib": ["zlibON", "zlibOFF"],
        "ssl": ["sslON", "sslOFF"],
        "debug": ["debugON", "debugOFF"]
    },
    DefaultOptions: {
        "zlib": ["zlibOFF"],
        "ssl": ["sslOFF"],
        "debug": ["debugOFF"]
    }
}
```

### Fields

**Require**:
- Build parameters that propagate to dependencies
- Examples: `os`, `arch`, `lang`
- All require combinations are built and tested

**Options**:
- Build parameters local to current package only
- Do not propagate to dependencies
- Examples: `zlib`, `ssl`, `debug`
- Full combinations may not be built (use DefaultOptions)

**DefaultOptions**:
- Subset of Options that defines default configurations
- LLAR builds DefaultOptions matrix by default
- Non-default options trigger lazy build (server builds on demand)
- DefaultOptions are fully tested

### Matrix Combination Format

Combination format: `{require}-{require}-{require}|{option}-{option}`

**Example**:
```
x86_64-c-linux|zlibOFF-sslOFF-debugOFF
arm64-cpp-darwin|zlibON-sslON-debugON
```

- Require values joined by `-`
- Options values joined by `-`, separated from require by `|`

### Matrix Calculation

Given the matrix above:

- **Require Combinations**: 2 (arch) × 2 (lang) × 2 (os) = 8 combinations
- **DefaultOptions Combinations**: 1 (zlib) × 1 (ssl) × 1 (debug) = 1 combination
- **Default Matrix Total**: 8 × 1 = **8 combinations** (built by default)
- **Full Matrix Total**: 8 × 8 = **64 combinations** (if all options built)

### Build Strategy

- **LLAR builds DefaultOptions matrix by default** (8 combinations in example)
- **Non-default options trigger lazy build** (remaining 56 combinations)
- Lazy build: Server builds on demand, user waits for completion

### Accessing Matrix in Formula

```go
onBuild (proj, out) => {
    // Access current matrix
    m := proj.matrix

    // Check require values
    if m.require["os"] == "darwin" {
        cmake "-DCMAKE_OSX_ARCHITECTURES=${m.require["arch"]}", "."
    }

    // Check options values
    if m.options["zlib"] == "zlibON" {
        cmake "-DWITH_ZLIB=ON", "."
    }

    cmake "--build", "."
    cmake "--install", "."

    out.setCompileFlags("-I${proj.dir}/include")
    out.setLinkFlags("-L${proj.dir}/lib", "-lmylib")
}
```

### Filtering Invalid Matrix Combinations

The Cartesian product can generate invalid combinations (e.g., certain OS/arch pairs are unsupported). Use the `filter` function to remove them.

**Example**:
```go
matrix require = {
    "arch": ["x86_64", "arm64", "mips"],
    "os": ["linux", "darwin"]
}

filter matrix => {
    // macOS does not support MIPS
    if matrix.require["os"] == "darwin" && matrix.require["arch"] == "mips" {
        return false  // Exclude this combination
    }
    return true  // Keep valid combinations
}
```

**Filter Semantics**:
- `return true`: Keep the combination
- `return false`: Remove the combination
- Filters execute after matrix generation but before builds

For more details on Build Matrix, see `docs/matrix.md`.
