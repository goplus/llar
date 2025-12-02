# LLAR Product Design

## 1. Product Background

### 1.1 Core Problem

Building local compilation environments requires:
- Installing toolchains (GCC, Clang, CMake)
- Understanding build systems (CMake, Make, Autotools)
- Resolving dependency conflicts manually
- Handling platform-specific issues

**This mental burden is the barrier to making software engineering accessible.**

### 1.2 LLAR's Solution

Three core advantages:

1. **Auto Dependency Resolution**: MVS algorithm eliminates "dependency hell"
2. **Lazy Build**: DefaultOptions pre-built, rare configs built by server on-demand
3. **Smart Formulas**: Auto-parse dependencies from CMakeLists.txt, low maintenance

## 2. Basic Concepts

### 2.1 Module

A module is a versioned source library (e.g., `DaveGamble/cJSON@1.7.18`), containing:
- **Formula**: Build instructions
- **Matrix**: Supported configurations (os/arch/options)
- **Versions**: Auto-retrieved from GitHub/GitLab (e.g., v1.0.0, v1.7.18, v2.0.0)
- **Dependencies**: Required modules

**Module vs Package**:
- **Module**: Has version number, is the unit of dependency management (e.g., `zlib@1.2.13`)
- **Package**: Build artifact (pre-built binaries) for a specific module version and matrix combination

### 2.2 Formula

Formula defines how to build a module across versions and platforms.

**Structure**: `{{repo}}_cmp.gox` (version comparison) + `versions.json` (deps fallback) + `{{repo}}_llar.gox` (build logic)

**Execution Flow**:

```mermaid
sequenceDiagram
    participant User
    participant Formula
    participant Source

    User->>Formula: Install cJSON@1.7.18
    Formula->>Source: onRequire: Parse CMakeLists.txt
    Source-->>Formula: Dependencies: re2c@2.0, zlib@1.2.13
    Formula->>Source: onBuild: cmake + build
    Source-->>Formula: Artifacts: libcjson.a
    Formula-->>User: Link flags: -lcjson -I/include -L/lib
```

### 2.3 Build Matrix

Matrix uses Cartesian product to generate all build combinations.

**Components**:
- **Require** (propagates to deps): os, arch, lang
- **Options** (local only): zlib, ssl, debug
- **DefaultOptions** (pre-built): Default option values

**Example**: `2 os × 2 arch × 1 lang = 4 require combinations`, `2 zlib × 2 ssl × 2 debug = 8 option combinations`
- **Full matrix**: 4 × 8 = 32 total combinations
- **Default matrix**: 4 × 1 = 4 pre-built combinations (e.g., `x86_64-c-linux|zlibOFF-sslOFF-debugOFF`)

**Propagation**: Require propagates to all deps, Options only to packages that declare them.

### 2.4 Lazy Build

**Current Implementation (v0.x)**: When pre-built artifact doesn't exist, users build locally.

```mermaid
sequenceDiagram
    participant User
    participant Cache

    User->>Cache: Request artifact
    alt Cache Hit
        Cache-->>User: Download (30s)
    else Cache Miss (Current)
        User->>User: Local build (5-10min)
        Note over User: Build happens locally<br/>Not shared with other users
    end
```

**Future Plan (v1.0+)**: Server lazy build will be implemented.

```mermaid
sequenceDiagram
    participant User
    participant Cache
    participant Server

    User->>Cache: Request artifact
    alt Cache Hit
        Cache-->>User: Download (30s)
    else Cache Miss (Future)
        Cache->>Server: Trigger server build
        Server->>Cache: Upload artifact (2-5min)
        Cache-->>User: Download (30s)
        Note over Cache: Cached for future users
    end
```

### 2.5 Version Management

**Auto-retrieval**: Versions fetched from GitHub/GitLab tags (e.g., v1.0.0, v1.7.18, v2.0.0)

**Formula Selection**: Select formula with max `fromVer <= target version`
- Example: Installing v1.7.18 → selects formula with fromVer=v1.5.0 (not v2.0.0)

**MVS Conflict Resolution**: When HTTP lib requires zlib ≥ 1.2.0 and Image lib requires zlib ≥ 1.2.8 → auto-select zlib 1.2.8

### 2.6 Dependency Management

**Two-tier resolution**:
1. **onRequire** (automatic): Parse CMakeLists.txt → extract dependencies
2. **versions.json** (fallback): Manual lookup table when parsing fails or version unspecified

**Example**: `ninja@1.11.0` → onRequire finds "need re2c" (no version) → fallback to `versions.json: v1.11.0 → re2c@2.0.3`

## 3. User Stories

### 3.1 Developer: Quick Install Dependencies

**As** a developer
**I want** to quickly install project dependencies
**So that** I can save build time and focus on business development

**Workflow**:
```mermaid
sequenceDiagram
    participant Dev as Developer
    participant CLI as LLAR CLI
    participant Cache as Pre-built Cache
    participant Cloud as Cloud Build

    Dev->>CLI: llar install cJSON@1.7.18
    CLI->>Cache: Query pre-built package (DefaultOptions)

    alt Pre-built exists
        Cache-->>CLI: Return artifact
        CLI->>Dev: Install complete (30s)
    else Pre-built not exists (Non-LLAR pre-built)
        CLI->>Cloud: Trigger cloud build
        CLI->>Dev: Waiting for server build...
        Cloud->>Cloud: Building...
        Cloud-->>Cache: Upload artifact
        Cloud-->>CLI: Build complete
        CLI->>Dev: Install complete (2-5min)
    end
```

**Acceptance Criteria**:
- Pre-built exists (DefaultOptions): Complete in 30s
- Pre-built not exists: Trigger server build, user waits
- Server build completes: Upload to cache and download
- Build artifacts are shared globally for future use

### 3.2 Developer: Consistent Dependency Versions

**As** a developer
**I want** LLAR to use exact dependency versions from formula
**So that** builds are reproducible and consistent

**Workflow**:
```mermaid
graph TD
    A[User: llar install cJSON@1.7.18] --> B[Load formula]
    B --> C[Read versions.json from formula]
    C --> D[Use exact versions specified]
    D --> E[Install dependencies]
    E --> F[Reproducible build]
```

**Acceptance Criteria**:
- versions.json maintained by formula maintainer
- Users read versions.json from formula repository
- Same formula version = same dependency versions
- Users cannot modify versions.json

### 3.3 Developer: List Available Versions

**As** a developer
**I want** to list all available versions of a module
**So that** I can choose the right version

**Command**: `llar list <module>`

**Acceptance Criteria**:
- Display all versions sorted by time (newest first)
- Auto-fetch from GitHub/GitLab tags
- Support `--json` output format

### 3.4 Developer: View Module Information

**As** a developer
**I want** to view detailed module build information
**So that** I can understand dependencies and build configuration

**Command**: `llar info <module>[@version]`

**Acceptance Criteria**:
- Display module metadata (version, matrix, build time)
- Show dependencies and link flags
- Support `--json` output format

### 3.5 Developer: Search Modules

**As** a developer
**I want** to search modules by keyword
**So that** I can find libraries I need

**Command**: `llar search <keyword>`

**Acceptance Criteria**:
- Search by module name and description
- Display matching modules with descriptions
- Support `--json` output format

### 3.6 Maintainer: Submit New Formula

**As** a formula maintainer
**I want** to submit formula for new library
**So that** community users can use this library

**Workflow**:
```mermaid
sequenceDiagram
    participant M as Maintainer
    participant Local as Local Env
    participant GitHub as Formula Repo
    participant Review as Reviewer

    M->>Local: Create formula files
    M->>Local: Write _cmp.gox
    M->>Local: Write _llar.gox
    M->>Local: Write versions.json (optional)
    M->>Local: Local test build

    alt Test success
        M->>GitHub: Create Pull Request
        GitHub->>Review: Notify review
        Review->>Review: Check formula spec
        Review->>Review: Test build

        alt Review pass
            Review->>GitHub: Merge PR
            GitHub->>M: Notify merge success
        else Review fail
            Review->>M: Feedback changes
            M->>Local: Fix formula
            M->>GitHub: Update PR
        end
    else Test fail
        M->>Local: Fix formula
        M->>Local: Retest
    end
```

**Acceptance Criteria**:
- Formula directory structure follows spec
- _cmp.gox implements compareVer
- _llar.gox implements necessary callbacks
- Local test build succeeds
- Pass PR review

### 3.7 Maintainer: Use onRequire to Reduce Maintenance

**As** a formula maintainer
**I want** to implement onRequire to auto-parse dependencies
**So that** I can auto-track upstream changes without manually maintaining versions.json

**Scenario Comparison**:

**Manual maintenance (versions.json)**:
```json
{
    "versions": {
        "v1.11.0": [
            {"name": "skvadrik/re2c", "version": "2.0.3"}
        ]
    }
}
```
- Problem: Need manual update when upstream changes version
- Maintenance cost: High

**Auto-parse (onRequire)**:
```go
onRequire (proj, deps) => {
    cmake := proj.readFile("CMakeLists.txt")
    // find_package(re2c 2.0 REQUIRED) -> auto parse
    matches := findDeps(cmake)

    for m in matches {
        deps.require(pkgID(m.Name), m.Version)
    }
}
```
- Advantage: Auto-tracks upstream CMakeLists.txt changes
- Maintenance cost: Low

**Acceptance Criteria**:
- onRequire can read build system files
- Auto-parse dependency names and versions
- Fallback to versions.json when parse fails

### 3.8 Maintainer: Initialize Formula Project

**As** a formula maintainer
**I want** to initialize a formula project
**So that** I can start writing formula for a module

**Command**: `llar init`

**Acceptance Criteria**:
- Create `versions.json` file
- Initialize with empty dependency list
- Ready to use `llar get` to add dependencies

### 3.9 Maintainer: Add Formula Dependency

**As** a formula maintainer
**I want** to add dependencies to formula's versions.json
**So that** I can declare module dependencies

**Command**: `llar get <module>[@version]`

**Acceptance Criteria**:
- Add dependency to `versions.json`
- Resolve version conflicts with MVS
- Record exact version

### 3.10 Maintainer: Clean Up Formula Dependencies

**As** a formula maintainer
**I want** to clean up unused dependencies in versions.json
**So that** I can keep formula configuration tidy

**Command**: `llar tidy`

**Acceptance Criteria**:
- Remove unused entries from `versions.json`
- Keep only actively used dependencies
- Preserve dependency order

## 4. Core Workflow

### 4.1 Module Install Flow

```mermaid
graph TB
    A[User: llar install cJSON@1.7.18] --> B[Parse module and version]
    B --> C[Get formula from repo]
    C --> D[Parse build matrix - DefaultOptions]
    D --> E[Execute MVS algorithm]

    E --> F[Generate BuildList]
    F --> G[Traverse dependencies]

    G --> H{Pre-built exists?<br/>DefaultOptions}
    H -->|Yes| I[Download pre-built]
    H -->|No<br/>Non-LLAR pre-built| J[Lazy build]

    J --> K[Trigger cloud build]
    K --> L[User waits for server]
    L --> M[Cloud builds artifact]
    M --> N[Upload to cache]
    N --> O[Download cloud artifact]

    O --> P[Install complete]
    I --> P
```

### 4.2 Dependency Resolution Flow

```mermaid
sequenceDiagram
    participant User
    participant LLAR
    participant Formula
    participant VersionsJSON as versions.json
    participant MVS

    User->>LLAR: llar install A@1.0.0

    LLAR->>Formula: Load formula

    alt onRequire implemented
        Formula->>Formula: Execute onRequire
        Formula->>Formula: Parse CMakeLists.txt
        Formula->>Formula: Call deps.require(pkgID, version)

        alt Version is empty
            Formula->>VersionsJSON: Read dependency version
            VersionsJSON-->>Formula: Return exact version
        end

        Formula-->>LLAR: Dependency list
    else onRequire not implemented
        LLAR->>VersionsJSON: Read dependencies directly
        VersionsJSON-->>LLAR: Dependency list
    end

    LLAR->>MVS: Pass dependency graph
    MVS->>MVS: Resolve conflicts
    MVS-->>LLAR: BuildList

    LLAR->>User: Start build
```

### 4.3 Formula Selection Flow

```mermaid
graph TD
    A[Request package@v1.7.0] --> B[Find all formulas]
    B --> C{fromVer <= v1.7.0?}

    C -->|v1.0.0: Yes| D[Candidate: v1.0.0]
    C -->|v1.5.0: Yes| E[Candidate: v1.5.0]
    C -->|v2.0.0: No| F[Skip: v2.0.0]

    D --> G[Select maximum fromVer]
    E --> G

    G --> H[Selected: v1.5.0]
    H --> I[Load formula and build]
```

## 5. Technical Design

### 5.1 Formula Execution Flow

**Dependency Resolution Phase** (top-down, breadth-first):
```
App (onRequire) → Parse dependencies
  ↓
HTTP Library (onRequire) → Parse dependencies
  ↓
OpenSSL (onRequire) → Parse dependencies
```

**Build Phase** (BuildList order, topological sort):
```
OpenSSL (onBuild) → Build
  ↓
HTTP Library (onBuild) → Build (can access OpenSSL artifact)
  ↓
App (onBuild) → Build (can access HTTP + OpenSSL artifacts)
```

### 5.2 Static Linking (Recommended)

LLAR recommends static libraries (`.a` files) for isolation, reproducibility, and portability.

**Note**: Dynamic library support is under development.

## 6. Design Advantages

| Feature | LLAR | Conan | Homebrew | Nix |
|---------|------|-------|----------|-----|
| Dependency Resolution | Auto (MVS) | Manual override | Manual | Manual |
| Missing Artifact | Server build | Local only | Not available | Local only |
| Formula Maintenance | Auto (onRequire) | Manual | Manual | Manual |
| Learning Curve | Low (XGo) | Medium | Low | High |
| Cross-platform | Yes | Yes | macOS/Linux | Yes |
| Default Build | DefaultOptions only | All configs | Limited | All configs |
