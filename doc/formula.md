LLAR Formula
====

## Example

```go
id "ninja-build/ninja"  // pkg id

fromVer "v1.0.0"  // run formula from this version

// Define build matrix: 'require' propagates downward, 'options' is limited to the current package
matrix require = {
        "os": ["linux", "darwin"],
        "arch": ["x86_64", "arm64"],
        "lang": ["c"]
}, options = {
        "tests": ["ON", "OFF"]
}

onRequire (proj, deps) => {  // abstract deps from this project
    cmake := proj.readFile("CMakeLists.txt")

    // find_package(re2c 2.0 REQUIRED)  -> {name: "re2c", version: "2.0"}
    // find_package(zlib REQUIRED)      -> {name: "zlib", version: ""}
    matches := findDeps(cmake)  // return [{name: "re2c", version: "2.0"}, {name: "zlib", version: ""}]

    for m in matches {
        deps.require(pkgID(m.Name), m.Version)
    }
}

onBuild (proj, out) => {  // build this project
    cmake "-DBUILD_SHARED_LIBS=OFF", "-DCMAKE_INSTALL_PREFIX=" + proj.dir, "."
    cmake "--build", "."
    cmake "--install", "."

    out.setLinkFlags("-I" + out.dir + "/include", "-L" + out.dir + "/lib", "-lninja")
}
```

## File Structure

A formula consists of three files:

```
{{owner}}/{{repo}}/
├── {{repo}}_cmp.gox      # version comparison function
├── versions.json         # dependency lookup table (optional)
└── {{repo}}_llar.gox     # formula main file
```

- `{{repo}}_cmp.gox`: Defines version comparison logic, separate file to avoid circular dependencies
- `versions.json`: Fallback dependency table when onRequire cannot auto-resolve dependencies
- `{{repo}}_llar.gox`: Contains id, fromVer, onRequire, onBuild

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

### Project

Project is the build context, and its interface is implemented as follows:

```go
type Graph interface {
    // Require specifies a package dependency
    Require(id string, ver version.Version)
}

type Project interface {
    fs.FS                    // Project source directory
    deps.Graph               // Dependency graph

    Dir()                    // Project source directory
    // ReadFile reads the content of a file from the project source directory
    ReadFile(filename string) ([]byte, error)

    // Matrix returns the current build matrix configuration
    Matrix()
}
```

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
    cmake := proj.readFile("CMakeLists.txt")
    matches := findDeps(cmake)

    for m in matches {
        deps.require(pkgID(m.Name), m.Version)
    }
}
```

### onBuild

Executes the project build and sets link flags.

**Parameters**:
- `proj`: Project context
  - `proj.matrix`: Current build matrix (contains os, arch, lang, etc.)
- `out`: Build output interface
  - `out.dir`: Output directory (read-only)
  - `out.setLinkFlags(flags...)`: Set link flags

**Build Requirements**:
- LLAR recommends using static libraries (`.a` files)
- Recommended to disable shared library builds (`-DBUILD_SHARED_LIBS=OFF`)
- Use `out.setLinkFlags()` to set include paths and library paths

**Example**:
```go
onBuild (proj, out) => {
    // Configure build (command-style call, no parentheses)
    cmake "-DBUILD_SHARED_LIBS=OFF", "-DCMAKE_INSTALL_PREFIX=" + proj.dir, "."

    // Execute build
    cmake "--build", "."
    cmake "--install", "."

    // Set link flags
    out.setLinkFlags("-I" + proj.dir + "/include", "-L" + proj.dir + "/lib", "-lninja")
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
    return semverCompare(v1, v2)
}
```

## versions.json

Optional dependency lookup table (similar to go.mod), used as fallback when onRequire cannot auto-resolve dependencies.

**Format**:
- Keys are exact package versions
- Values are dependency arrays (preserves dependency order)

**Example**:
```json
{
    "versions": {
        "v1.0.0": [],
        "v1.10.0": [
            {"name": "madler/zlib", "version": "1.2.13"},
            {"name": "skvadrik/re2c", "version": "2.0.3"}
        ]
    }
}
```

**Use Cases**:
- Fallback when onRequire fails to parse dependencies
- Upstream project does not explicitly declare dependency versions
- Historical version dependency information

## Cross-Formula Calls

Use `import(packageID)` to import other formulas.

**Returns**: Formula interface instance

**Example**:
```go
depFormula := import("madler/zlib")
```

## Version Management

**Version Retrieval**: Version lists are automatically retrieved from VCS (GitHub/GitLab), no manual maintenance required.

**Version Selection Algorithm**:
1. Select formula based on fromVer
2. If onRequire parses dependency versions, use parsed versions
3. If versions are not parsed, fall back to versions.json
4. System uses MVS algorithm to select version combination satisfying all constraints

## Complete Example

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
    // For example: certain architectures are not supported on some operating systems
    if matrix.require["os"] == "darwin" && matrix.require["arch"] == "mips" {
        return false  // Exclude macOS + MIPS combination
    }

    return true  // Keep other combinations
}

onRequire (proj, deps) => {
    cmake := proj.readFile("CMakeLists.txt")
    matches := findDeps(cmake)

    for m in matches {
        deps.require(pkgID(m.Name), m.Version)
    }
}

onBuild (proj, out) => {
    echo "Building cJSON", proj.version, "for", proj.matrix.os, "/", proj.matrix.arch

    cmake "-DBUILD_SHARED_LIBS=OFF", \
          "-DENABLE_CJSON_TEST=OFF", \
          "-DCMAKE_INSTALL_PREFIX=" + out.dir, \
          "."

    cmake "--build", "."
    cmake "--install", "."

    out.setLinkFlags(
        "-I" + out.dir + "/include",
        "-L" + out.dir + "/lib",
        "-lcjson"
    )
}
```

### cJSON_cmp.gox

```go
compareVer (v1, v2) => {
    return semverCompare(v1, v2)
}
```

### versions.json

```json
{
    "versions": {
        "v1.0.0": [],
        "v1.7.0": [
            {"name": "madler/zlib", "version": "1.2.13"}
        ]
    }
}
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

### Matrix Example

Given the matrix above:

**Require Combinations**: 2 × 2 × 2 = 8 combinations
```
x86_64-c-linux
x86_64-c-darwin
x86_64-cpp-linux
x86_64-cpp-darwin
arm64-c-linux
arm64-c-darwin
arm64-cpp-linux
arm64-cpp-darwin
```

**DefaultOptions Combinations**: 1 × 1 × 1 = 1 combination
```
zlibOFF-sslOFF-debugOFF
```

**Default Matrix Total**: 8 × 1 = 8 combinations
```
x86_64-c-linux|zlibOFF-sslOFF-debugOFF
x86_64-c-darwin|zlibOFF-sslOFF-debugOFF
x86_64-cpp-linux|zlibOFF-sslOFF-debugOFF
x86_64-cpp-darwin|zlibOFF-sslOFF-debugOFF
arm64-c-linux|zlibOFF-sslOFF-debugOFF
arm64-c-darwin|zlibOFF-sslOFF-debugOFF
arm64-cpp-linux|zlibOFF-sslOFF-debugOFF
arm64-cpp-darwin|zlibOFF-sslOFF-debugOFF
```

**Full Matrix Total**: 8 × 8 = 64 combinations (if all options built)

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
        cmake "-DCMAKE_OSX_ARCHITECTURES=" + m.require["arch"], "."
    }

    // Check options values
    if m.options["zlib"] == "zlibON" {
        cmake "-DWITH_ZLIB=ON", "."
    }

    cmake "--build", "."
    cmake "--install", "."

    out.setLinkFlags("-I" + out.dir + "/include", "-L" + out.dir + "/lib", "-lmylib")
}
```

### Filtering Invalid Matrix Combinations

In some cases, the **Cartesian product** of a matrix can generate invalid combinations that need to be filtered out.

**Why Filtering is Needed**:
- The Cartesian product generates all possible combinations, including technically infeasible ones.
- For example, certain operating systems do not support specific hardware architectures.
- Filters allow us to remove these invalid combinations after the Cartesian product is generated.

**Common Scenarios**:

Some combinations of architecture and operating system are unsupported:
- `os: darwin, arch: mips` is invalid (macOS does not support MIPS architecture)
- `os: linux, arch: mips` is valid (Linux supports MIPS architecture)
- `os: windows, arch: riscv` is invalid (Windows does not support RISC-V architecture)

**Defining the Filter Function**:

```javascript
matrix require = {
    "arch": ["x86_64", "arm64", "mips", "riscv"],
    "os": ["linux", "darwin", "windows"]
}, options = {
    "shared": ["static", "dynamic"]
}

// Filter invalid matrix combinations
filter matrix => {
    // macOS does not support MIPS or RISC-V
    if matrix.require["os"] == "darwin" && (matrix.require["arch"] == "mips" || matrix.require["arch"] == "riscv") {
        return false  // Exclude this combination
    }

    // Windows does not support RISC-V
    if matrix.require["os"] == "windows" && matrix.require["arch"] == "riscv" {
        return false
    }

    // All other combinations are valid
    return true  // Keep this combination
}
```

**Filter Semantics**:
- `return true`: Keep the matrix combination
- `return false`: Remove the matrix combination
- The filter function receives a `matrix` parameter containing all field values for the current combination
- Filters execute after matrix combinations are generated but before test strategies are applied
