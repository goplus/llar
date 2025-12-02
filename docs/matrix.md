# LLAR 构建矩阵设计文档

## 1. 背景与动机

### 1.1 问题定义

LLAR 的出发点是为了解决构建产物难题，即当构建配置无限进行膨胀时的管理问题。

**核心问题**：
- 一个包可能只存在一种配方，但因为外部需求的变化会导致多种产物
- 不同的库或 Module 存在大量可选编译配置
- 有大量编译组合，通常更换一个配置或换个构建平台，就需要重头把这些库构建一遍

### 1.2 现有方案的局限

**Conan 的方案**：
- 提出了类似的构建配置概念
- 将用户构建配置分为两部分：
  - **settings**：会向下传播的编译需要参数
  - **options**：仅限于当前 package，不会向下传播的配置
- 但是 Conan 并没有"构建矩阵"这个明确的概念

### 1.3 矩阵爆炸问题

构建矩阵的组合数量随着参数增加呈指数级增长，这会导致以下两个核心问题：

#### 问题1：无法进行全量测试

当构建矩阵组合数量过多时，对所有组合进行全量测试变得不现实。

**真实案例：Conan 的 Boost 库**

Boost 在 Conan 中配置了 **59 个布尔类型 options**，加上 require 参数（arch、os、compiler），理论组合数达到：

```
总组合数 ≈ 27 (require) × 2^59 (options) ≈ 1556 万亿亿种组合
测试时间 ≈ 1480 亿年（是宇宙年龄的 10.7 倍）
```

**结论**：必须采用配对测试等策略减少测试组合数。

#### 问题2：预构建产物趋于无限（已解决）

如果预先构建所有矩阵组合，产物数量会爆炸式增长，占用大量存储空间。

**解决方案**：通过**按需构建机制**（惰性构建），仅构建用户实际请求的矩阵组合，避免预先构建所有可能的组合。

#### 针对问题1的解决方案

针对"无法进行全量测试"问题，LLAR 提供两种解决方案：

##### 方案1：严格限制矩阵数量

直接限制 `options` 字段的数量或每个选项的值数量，从源头控制矩阵爆炸。

**限制规则**：
- `options` 字段最多 **3 个**
- 每个 option 最多 **3 个值**
- 总 options 组合数不超过 **27 个**（3³）

**示例配置**：
```go
matrix {
    Require: {
        "arch": ["x86_64"],
        "lang": ["c"],
        "os": ["linux"]
    },
    Options: {
        "zlib": ["zlibON", "zlibOFF"],
        "ssl": ["sslON", "sslOFF"],
        "debug": ["debugON", "debugOFF"]
    }
}
```

**组合计算**：
```
require 组合：1 种（x86_64-c-linux）
options 组合：2 × 2 × 2 = 8 种
总组合数：1 × 8 = 8 种

测试时间：8 × 5分钟 = 40分钟（可接受）
```

**适用场景**：
- 小型库或工具
- 配置选项较少的项目
- 需要完整测试覆盖的项目

##### 方案2：默认矩阵 + 配对测试法

提供一个默认矩阵组合进行全量测试，非默认组合根据数量决定测试策略。

**测试策略分级**：

1. **完整矩阵组合 < 5000 种**：
   - 对所有组合进行全量测试
   - 测试成本可接受，无需配对测试

2. **完整矩阵组合 ≥ 5000 种**：
   - 对默认矩阵组合进行全量测试
   - 对非默认组合采用**配对测试法（Pairwise Testing）**

**配对测试核心思想**：
- 配对测试法确保任意两个参数的所有组合至少被测试一次
- 研究表明 **70-95% 的缺陷由两个参数交互引起**
- 大幅减少测试用例数量，同时保持高缺陷检测率

**默认矩阵定义**：

在配方文件（formula.gox）中定义构建矩阵：

```go
// 声明构建矩阵
matrix {
    Require: {
        "arch": ["x86_64", "arm64", "mips"],
        "lang": ["c", "cpp"],
        "os": ["linux", "darwin"]
    },
    Options: {
        "zlib": ["zlibON", "zlibOFF"],
        "ssl": ["sslON", "sslOFF"],
        "debug": ["debugON", "debugOFF"]
    },
    DefaultOptions: {
        "zlib": ["zlibOFF"],    // 默认只测试关闭状态
        "ssl": ["sslOFF"],
        "debug": ["debugOFF"]
    }
}
```

**DefaultOptions 字段说明**：
- 只包含 Options 字段的子集，定义需要全量测试的默认 options 组合
- **Require 字段的所有组合默认都会参与测试**
- DefaultOptions 采用与 Options 相同的数组结构，但组合数量有限
- 上例中：
  - Require 全量组合：2 × 2 × 2 = **8 种组合**（arch × lang × os）
  - DefaultOptions 组合：1 × 1 × 1 = **1 种组合**（全部 OFF）
  - 默认矩阵总组合数：8 × 1 = **8 种组合**
- 这些默认组合始终进行**全量测试**，确保核心功能稳定

**测试策略**：

1. **默认组合（Full Testing）**：
   ```
   默认矩阵组合（8 种）：
   - x86_64-c-linux|zlibOFF-sslOFF-debugOFF
   - x86_64-c-darwin|zlibOFF-sslOFF-debugOFF
   - x86_64-cpp-linux|zlibOFF-sslOFF-debugOFF
   - x86_64-cpp-darwin|zlibOFF-sslOFF-debugOFF
   - arm64-c-linux|zlibOFF-sslOFF-debugOFF
   - arm64-c-darwin|zlibOFF-sslOFF-debugOFF
   - arm64-cpp-linux|zlibOFF-sslOFF-debugOFF
   - arm64-cpp-darwin|zlibOFF-sslOFF-debugOFF
   ```
   对所有默认组合进行完整的功能测试、集成测试、性能测试。

2. **非默认 Options 组合测试策略**：

   完整矩阵组合数：(2 × 2 × 2) × (2 × 2 × 2) = 8 × 8 = **64 种组合**
   默认矩阵组合数：8 × 1 = **8 种组合**（全量测试）
   剩余 Options 组合：8 × 7 = **56 种组合**

   **判断**：56 < 5000，对剩余 56 种组合进行**全量测试**

   总测试数：**64 种组合**（全部全量测试）

**配对测试示例**：

假设有以下参数：
- `arch`: [x86_64, arm64]
- `os`: [linux, darwin]
- `zlib`: [zlibON, zlibOFF]
- `ssl`: [sslON, sslOFF]

完全组合需要测试：2 × 2 × 2 × 2 = **16 种组合**

配对测试只需：**6-8 种组合** 即可覆盖所有两两配对：

```
测试组合：
1. x86_64-c-linux|zlibON-sslON
2. x86_64-c-darwin|zlibOFF-sslOFF
3. arm64-c-linux|zlibOFF-sslON
4. arm64-c-darwin|zlibON-sslOFF
5. x86_64-c-linux|zlibOFF-sslON
6. arm64-c-linux|zlibON-sslON
```

**覆盖率分析**：
- 测试覆盖率从 16 组减少到 6 组，**节省 62.5% 测试资源**
- 仍能发现大部分参数交互问题

**配对覆盖表**：

| 配对参数 | 组合1 | 组合2 | 组合3 | 组合4 | 组合5 | 组合6 |
|---------|------|------|------|------|------|------|
| **(arch, os)** | x86_64, linux | x86_64, darwin | arm64, linux | arm64, darwin | x86_64, linux | arm64, linux |
| **(arch, zlib)** | x86_64, ON | x86_64, OFF | arm64, OFF | arm64, ON | x86_64, OFF | arm64, ON |
| **(arch, ssl)** | x86_64, ON | x86_64, OFF | arm64, ON | arm64, OFF | x86_64, ON | arm64, ON |
| **(os, zlib)** | linux, ON | darwin, OFF | linux, OFF | darwin, ON | linux, OFF | linux, ON |
| **(os, ssl)** | linux, ON | darwin, OFF | linux, ON | darwin, OFF | linux, ON | linux, ON |
| **(zlib, ssl)** | ON, ON | OFF, OFF | OFF, ON | ON, OFF | OFF, ON | ON, ON |

**配对测试组合生成方法**：

配对测试的核心目标是：**确保任意两个参数的所有值组合至少被测试一次**。

**需要覆盖的配对**：

对于 4 个参数 (arch, os, zlib, ssl)，有 6 种两两配对：
- (arch, os)
- (arch, zlib)
- (arch, ssl)
- (os, zlib)
- (os, ssl)
- (zlib, ssl)

每种配对有 2×2 = 4 种值组合，总共需要覆盖 6×4 = **24 个配对值组合**。

**组合生成示例**：

通过精心选择 6 个测试组合，可以覆盖所有 24 个配对值组合：

| 测试组合 | arch | os | zlib | ssl | 覆盖的关键配对 |
|---------|------|-------|------|-----|-------------|
| **组合1** | x86_64 | linux | ON | ON | (x86_64,linux), (x86_64,ON), (linux,ON), (ON,ON) |
| **组合2** | x86_64 | darwin | OFF | OFF | (x86_64,darwin), (x86_64,OFF), (darwin,OFF), (OFF,OFF) |
| **组合3** | arm64 | linux | OFF | ON | (arm64,linux), (arm64,OFF), (linux,OFF), (OFF,ON) |
| **组合4** | arm64 | darwin | ON | OFF | (arm64,darwin), (arm64,ON), (darwin,ON), (ON,OFF) |
| **组合5** | x86_64 | linux | OFF | ON | 补充覆盖遗漏的配对 |
| **组合6** | arm64 | linux | ON | ON | 补充覆盖遗漏的配对 |

**覆盖验证**：

验证所有配对的 4 种值组合都被测试到：

```
(arch, os) 的 4 种组合：
  (x86_64, linux)  → 组合1, 5
  (x86_64, darwin) → 组合2
  (arm64, linux)   → 组合3, 6
  (arm64, darwin)  → 组合4

(arch, zlib) 的 4 种组合：
  (x86_64, ON)     → 组合1
  (x86_64, OFF)    → 组合2, 5
  (arm64, ON)      → 组合4, 6
  (arm64, OFF)     → 组合3

... 其他 4 种配对同理，全部覆盖
```

**效果对比**：
- 完全组合测试：2×2×2×2 = **16 种组合**
- 配对测试：**6 种组合**
- 节省比例：**(16-6)/16 = 62.5%**

**工具支持**：

LLAR 可以使用现成的配对测试工具（如 PICT、ACTS）自动生成最优的测试组合，无需手动计算。

**超过 5000 种组合的示例（需配对测试）**：

假设一个复杂库（如 Boost）有以下配置：
```go
matrix {
    Require: {
        "arch": ["x86_64", "arm64", "x86"],
        "os": ["linux", "darwin", "windows"],
        "lang": ["cpp"]
    },
    Options: {
        "shared": ["static", "dynamic"],
        "zlib": ["ON", "OFF"],
        "bzip2": ["ON", "OFF"],
        "lzma": ["ON", "OFF"],
        "zstd": ["ON", "OFF"],
        "icu": ["ON", "OFF"],
        "python": ["ON", "OFF"],
        "iostreams": ["ON", "OFF"],
        "serialization": ["ON", "OFF"],
        "filesystem": ["ON", "OFF"]
        // ... 共 10 个 options
    },
    DefaultOptions: {
        "shared": ["static"],
        "zlib": ["OFF"],
        "bzip2": ["OFF"],
        "lzma": ["OFF"],
        "zstd": ["OFF"],
        "icu": ["OFF"],
        "python": ["OFF"],
        "iostreams": ["OFF"],
        "serialization": ["OFF"],
        "filesystem": ["OFF"]
    }
}

完整矩阵：(3 × 3 × 1) × 2^10 = 9 × 1024 = 9,216 种组合
Require 全量组合：3 × 3 × 1 = 9 种
DefaultOptions 组合：1^10 = 1 种
默认矩阵：9 × 1 = 9 种组合
```

**测试策略**：
```
完整矩阵组合数：9,216 种
判断：9,216 > 5000，需要采用配对测试

默认组合（全量测试）：9 个组合
  - x86_64-cpp-linux|static-OFF-OFF-OFF-OFF-OFF-OFF-OFF-OFF-OFF
  - x86_64-cpp-darwin|static-OFF-OFF-OFF-OFF-OFF-OFF-OFF-OFF-OFF
  - x86_64-cpp-windows|static-OFF-OFF-OFF-OFF-OFF-OFF-OFF-OFF-OFF
  - arm64-cpp-linux|static-OFF-OFF-OFF-OFF-OFF-OFF-OFF-OFF-OFF
  - ... （共 9 种 Require 组合）

非默认 Options 组合（配对测试）：9 × (1024 - 1) = 9,207 种组合
  配对测试只需：约 100-150 个组合

总测试数：9 + 100-150 = 109-159 个组合
节省比例：(9,216 - 159) / 9,216 = 98.27%
```

**配对测试工具**：

LLAR 系统可以集成现有的配对测试工具生成测试组合：
- **PICT** (Microsoft)：命令行配对测试工具
- **ACTS** (NIST)：高级组合测试系统
- **AllPairs** (Python)：Python 配对测试库

**适用场景**：
- 大型库或复杂系统
- 配置选项多的项目（完整矩阵组合 ≥ 5000 种时配对测试才生效）
- 需要在灵活性和测试成本间平衡的项目

##### 方案对比

| 维度 | 方案1：严格限制 | 方案2：默认矩阵 + 分级测试 |
|-----|---------------|----------------|
| **实现复杂度** | 简单 | 中等（需要判断阈值和配对算法） |
| **配置灵活性** | 低（限制 options 数量） | 高（支持大量 options） |
| **测试覆盖率** | 100%（全量测试） | < 5000: 100%；≥ 5000: 70-95% |
| **测试用例数** | 小（< 27个组合） | < 5000: 全量；≥ 5000: 配对测试 |
| **维护成本** | 低 | 中等（需要管理默认配置） |
| **适用项目规模** | 小型项目（< 27 组合） | 中大型项目（27-无限组合） |

##### 方案选择决策树

```
完整矩阵组合数量？
    ├─ < 27 种 → 使用方案1：严格限制矩阵数量
    │             - 限制 options 数量 ≤ 3
    │             - 每个 option 值数量 ≤ 3
    │             - 进行全量测试
    │
    └─ ≥ 27 种 → 使用方案2：默认矩阵 + 分级测试
                  - 定义默认矩阵（核心测试组合）
                  - 完整矩阵 < 5000 种：全量测试
                  - 完整矩阵 ≥ 5000 种：
                    - 默认矩阵：全量测试
                    - 非默认矩阵：配对测试
```

### 1.4 我们的目标

- 通过一种标准化的方案去表达这些构建配置
- 提供自动完成构建配置选择的能力
- 解决矩阵爆炸导致的测试和存储问题

## 2. 设计目标

1. **标准化**：能够提供一种标准化的构建配置解决方案
2. **自动化**：能够自动完成构建配置选择
3. **可扩展**：支持任意数量和类型的构建参数
4. **传播控制**：精确控制配置参数的传播范围

## 3. 核心概念

### 3.1 构建矩阵（Matrix）

构建矩阵用于表达一个包在不同构建配置下的所有可能产物组合。

**构成要素**：
- 由于一个包可能只存在一种配方
- 但这一种配方因为外部需求的变化会导致多种产物
- 为了代表这类变化，我们使用一个构建矩阵表达
- 矩阵通过**笛卡尔积**生成所有可能的配置组合

**示例**：
```
一个 C 库可能需要支持：
- 多个平台：x86_64, arm64
- 多个操作系统：Linux, macOS, Windows
- 多个编译器：GCC, Clang, MSVC
- 多个可选特性：zlib 支持开启/关闭

通过笛卡尔积：2 (平台) × 3 (系统) × 3 (编译器) × 2 (zlib)
= 36 种不同的构建产物组合
```

### 3.2 require 和 options 的概念

类似于 Conan，构建矩阵由两部分组成：

**require（必需参数）**：
- 类似于 Conan 的 `settings`
- 默认向下传播给所有依赖包
- 代表构建环境的必需配置
- 例如：arch（架构）、os（操作系统）、toolchain（工具链）

**options（可选参数）**：
- 类似于 Conan 的 `options`
- 不会向下传播，仅限于当前 package
- 代表包特定的可选特性
- 例如：是否启用 zlib、是否编译测试代码

### 3.3 为什么需要 require 和 options 概念？

在构建矩阵第一版设计中，是不存在这个区分的，默认参数都会从最上层的包一层一层进行传播。

**问题发现**：

经过讨论后发现以下事实：

1. **底层依赖的矩阵膨胀问题**：
   - 如果一层一层传播，最底层的依赖会得到一个巨大的构建矩阵
   - 很多字段其实并不需要

2. **复杂情况分析**（A 依赖 B）：
   - **Case 1**：A 有 B 不需要的字段，A 传导给了 B
     - 这个问题不大，B 可以忽略不需要的字段
   - **Case 2**：B 有 A 不存在的字段
     - 这个往往出现在 optional 的参数
     - 但这个参数没有值，那么此时该参数要不要往下继续传播？
     - 因为我们不能确定下一个依赖是否依赖了该参数

**解决方案**：

引入 `require` 和 `options` 概念后，Case 2 所导致的复杂问题可以被简单解决：
- `options` 默认不会被传播
- 只能接受来自最上层的传入
- 避免了中间层参数传播的不确定性

## 4. 矩阵结构设计

### 4.1 基本结构

在配方文件（formula.gox）中声明构建矩阵：

```go
// 声明构建矩阵
matrix {
    Require: {
        "arch": ["amd64", "arm64"],
        "lang": ["c", "cpp"],
        "os": ["linux", "darwin"],
        "key1": ["value 1", "value 2", ... "value n"],
        "key2": ["value 1", "value 2", ... "value n"],
        ...
        "keyn": ["value 1", "value 2", ... "value n"]
    },
    Options: {
        "zlib": ["zlibON", "zlibOFF"],
        "key1": ["value 1", "value 2", ... "value n"]
    }
}
```

### 4.2 结构化定义

```go
type PackageMatrix struct {
    Require map[string][]string `json:"require"`
    Options map[string][]string `json:"options"`
}
```

### 4.3 字段说明

**require 字段**：
- key：配置项名称（如 arch、os、lang）
- value：该配置项的所有可能取值数组

**options 字段**：
- key：可选特性名称（如 zlib、tests）
- value：该特性的所有可能取值数组

## 5. 必需字段与可选字段

### 5.1 必需字段

以下字段在 `require` 中是必需的：

#### arch（架构）

**说明**：代表编译平台架构

**常见取值**：
- `x86_64` / `amd64`：64位 x86 架构
- `arm64` / `aarch64`：64位 ARM 架构
- `x86` / `i386`：32位 x86 架构
- `arm`：32位 ARM 架构

**示例**：
```go
matrix {
    Require: {
        "arch": ["x86_64", "arm64"]
    }
}
```

#### lang（语言）

**说明**：代表该包的语言

**常见取值**：
- `c`：C 语言
- `cpp` / `cxx`：C++ 语言
- `py` / `python`：Python 语言

**示例**：
```go
matrix {
    Require: {
        "lang": ["c", "cpp"]
    }
}
```

### 5.2 特殊可选字段

以下字段在 `require` 中是可选的，但具有特殊含义：

#### os（操作系统）

**说明**：代表目标操作系统

**为什么是可选的**：
- 有些包可能允许在裸机环境下运行
- 不是所有包都依赖操作系统

**常见取值**：
- `linux`：Linux 系统
- `darwin`：macOS 系统
- `windows`：Windows 系统
- `freebsd`：FreeBSD 系统

**示例**：
```go
matrix {
    Require: {
        "os": ["linux", "darwin", "windows"]
    }
}
```

**注意**：工具链（toolchain）不属于构建矩阵系统，而是作为独立的系统进行管理。工具链涉及版本号管理（如 gcc-9, gcc-10, clang-12），需要独立的下载、安装、配置机制。

### 5.3 自定义字段

除了标准字段外，可以定义自定义字段：

**字段命名规范**：
- key 必须代表某种编译集成配置
- 例如：`sse2` 应该代表该包全部组件启用 `sse2`，而不是某个部分

**示例**：
```go
matrix {
    Require: {
        "arch": ["x86_64"],
        "simd": ["sse2", "avx2", "neon"]
    },
    Options: {
        "debug": ["on", "off"],
        "tests": ["enabled", "disabled"]
    }
}
```

## 6. 矩阵组合算法

### 6.1 组合生成规则

矩阵组合采用 **笛卡尔积（Cartesian Product）** 算法生成所有可能的构建配置组合。

**笛卡尔积定义**：
- 对于 n 个集合 A₁, A₂, ..., Aₙ，它们的笛卡尔积是所有可能的有序元组 (a₁, a₂, ..., aₙ) 的集合
- 其中 a₁ ∈ A₁, a₂ ∈ A₂, ..., aₙ ∈ Aₙ
- 笛卡尔积的大小为：|A₁| × |A₂| × ... × |Aₙ|

**LLAR 矩阵组合**：
- `require` 字段的所有值进行笛卡尔积
- `options` 字段的所有值进行笛卡尔积
- 最终组合数 = (require 笛卡尔积) × (options 笛卡尔积)

由于 JSON Object 在 RFC 规范中定义是无序组合，因此我们需要对其 Object key 进行按字母排序，然后从排序顺序前至后进行笛卡尔积组合。

**算法步骤**：
1. 对 `require` 的所有 key 进行字母排序
2. 按层序顺序进行笛卡尔积组合（第一层和第二层组合，其结果再和第三层组合，依次递归）
3. 用 `-` 连接 `require` 的值
4. 如果存在 `options`，对其进行笛卡尔积组合，用 `|` 连接 `options` 的值

### 6.2 组合生成示例

#### 基本示例

**输入**：
```json
{
    "matrix": {
        "require": {
            "os": ["linux", "darwin"],
            "arch": ["x86_64", "arm64"],
            "lang": ["c", "cpp"]
        }
    }
}
```

**计算过程（笛卡尔积）**：
1. 排序得到 key 顺序：`arch`, `lang`, `os`
2. 进行笛卡尔积组合：
   - `arch` × `lang`：`x86_64-c`, `arm64-c`, `x86_64-cpp`, `arm64-cpp`
   - 结果 × `os`：得到完整的笛卡尔积
3. 总组合数：2 (arch) × 2 (lang) × 2 (os) = **8 种组合**

**输出**：
```
x86_64-c-linux
x86_64-c-darwin
arm64-c-linux
arm64-c-darwin
x86_64-cpp-linux
x86_64-cpp-darwin
arm64-cpp-linux
arm64-cpp-darwin
```

#### 包含 options 的示例

**输入**：
```json
{
    "matrix": {
        "require": {
            "arch": ["x86_64", "arm64"],
            "lang": ["c"],
            "os": ["linux", "darwin"]
        },
        "options": {
            "zlib": ["zlibON", "zlibOFF"]
        }
    }
}
```

**计算过程（笛卡尔积）**：
1. 先完成 `require` 字段的笛卡尔积：
   ```
   2 (arch) × 1 (lang) × 2 (os) = 4 种 require 组合

   x86_64-c-linux
   x86_64-c-darwin
   arm64-c-linux
   arm64-c-darwin
   ```
2. 再与 `options` 进行笛卡尔积（用 `|` 连接）：
   ```
   4 (require 组合) × 2 (zlib 选项) = 8 种最终组合
   ```

**输出**：
```
x86_64-c-linux|zlibON
x86_64-c-linux|zlibOFF
x86_64-c-darwin|zlibON
x86_64-c-darwin|zlibOFF
arm64-c-linux|zlibON
arm64-c-linux|zlibOFF
arm64-c-darwin|zlibON
arm64-c-darwin|zlibOFF
```

### 6.3 矩阵组合表示格式

**格式规范**：
- `require` 组合：用 `-` 连接，按字母排序
- `options` 组合：用 `|` 分隔
- 完整格式：`{{require组合}}|{{options组合}}`

**示例**：
- 仅 require：`x86_64-c-linux`
- 包含 options：`x86_64-c-linux|zlibON`
- 多个 options：`x86_64-c-linux|debug-on-zlibON`

## 7. 矩阵传播机制

### 7.1 传播规则

**require 字段**：
- **必须向下传播**
- 所有依赖包的 `require` 必须是入口包的交集
- 如果依赖包的 `require` 不是入口包的交集，系统会终止并报错

**options 字段**：
- **不向下传播**
- 仅在声明了该 option 的包中生效
- 只接受来自最上层的传入

### 7.2 传播示例

#### 示例 1：无 options

```mermaid
graph TD
A[Root x86_64-c-linux] --> B[B x86_64-c-linux]
A --> C[C x86_64-c-linux]
```

**说明**：
- Root 的 `require` 配置 `x86_64-c-linux` 向下传播
- 所有依赖包使用相同的 `require` 配置

#### 示例 2：仅 C 有 options

```mermaid
graph TD
A["Root x86_64-c-linux|zlibON"] --> B[B x86_64-c-linux]
A --> C["C x86_64-c-linux|zlibON"]
C --> D["D x86_64-c-linux"]
```

**说明**：
- Root 的 `require` 配置 `x86_64-c-linux` 向下传播给所有依赖
- Root 的 `options` 配置 `zlibON` 只传播给声明了 `zlib` option 的包 C
- B 和 D 没有声明 `zlib` option，不受影响

#### 示例 3：非交集情况（错误）

```mermaid
graph TD
A[Root x86_64-c-linux] --> |Panic|B["B arm64-c-linux"]
A --> C[C x86_64-c-linux]
```

**说明**：
- Root 要求 `x86_64-c-linux`
- 但 B 要求 `arm64-c-linux`
- 这是非交集情况，系统会终止并报错

### 7.3 传播规则的意义

**为什么 require 必须是交集**：
- `require` 代表构建环境的基本要求
- 如果出现了某个配方接受到了不预期的 `require`，往往只有一种结果：该包无法编译出该平台或该配置
- 此时应该强制 panic 以让配方编写者知道问题

**为什么 options 不传播**：
- `options` 是包特定的可选特性
- 不是所有依赖都需要相同的可选特性
- 只有声明了该 option 的包才处理对应的配置

## 8. 矩阵的选择与使用

### 8.1 哈希表实现

构建矩阵在实现上可以使用哈希表进行实现。

**优势**：
- 选择过程就变成了使用哈希进行交集匹配
- 可以忽略顺序问题
- 类似于 Go BuildTags

**实现机制**：
- 一个配方只需要取其适合的配置信息
- 忽视掉不需要的配置项

### 8.2 配方中使用矩阵

#### 声明矩阵

```javascript
matrix {
    Require: {
        "os": ["linux", "darwin"],
        "arch": ["amd64", "arm64"],
        "lang": ["c"]
    },
    Options: {
        "zlib": ["zlibON", "zlibOFF"]
    }
}
```

#### 在 onBuild 中使用矩阵

```javascript
onBuild => {
    args := []

    // 根据架构设置编译参数
    if matrix.require["arch"] == "arm64" {
        args <- "-DARCH=ARM64"
    }

    // 根据操作系统设置编译参数
    if matrix.require["os"] == "darwin" {
        args <- "-DCMAKE_OSX_ARCHITECTURES=arm64"
    }

    // 处理可选配置
    if matrix.options["zlib"] == "zlibON" {
        args <- "-DWITH_ZLIB=ON"
    }

    args <- "."

    // 执行CMake配置和构建
    cmake args
    cmake "--build" "."
    cmake "--install" "."

    // 返回构建产物
    return {
        Dir: "./install",
        LinkArgs: ["-I./install/include", "-L./install/lib", "-lmylib"]
    }, nil
}
```

#### 过滤无效矩阵组合

在某些情况下，矩阵的**笛卡尔积**会产生一些无效的组合，需要将其剔除。

**为什么需要过滤**：
- 笛卡尔积会生成所有可能的组合，包括技术上不可行的组合
- 例如：某些操作系统不支持特定的硬件架构
- 过滤器允许我们在笛卡尔积生成后，剔除这些无效组合

**典型场景**：

某些架构和操作系统的组合是不支持的：
- `os: darwin, arch: mips` 无效（macOS 不支持 MIPS 架构）
- `os: linux, arch: mips` 有效（Linux 支持 MIPS 架构）
- `os: windows, arch: riscv` 无效（Windows 不支持 RISC-V 架构）

**定义 filter 函数**：

```javascript
matrix {
    Require: {
        "arch": ["x86_64", "arm64", "mips", "riscv"],
        "os": ["linux", "darwin", "windows"]
    },
    Options: {
        "shared": ["static", "dynamic"]
    }
}

// 过滤无效的矩阵组合
filter matrix => {
    // macOS 不支持 MIPS 和 RISC-V
    if matrix.require["os"] == "darwin" && (matrix.require["arch"] == "mips" || matrix.require["arch"] == "riscv") {
        return false  // 剔除该组合
    }

    // Windows 不支持 RISC-V
    if matrix.require["os"] == "windows" && matrix.require["arch"] == "riscv" {
        return false
    }

    // 其他组合都是有效的
    return true  // 保留该组合
}
```

**Filter 语义**：
- `return true`：保留该矩阵组合
- `return false`：剔除该矩阵组合
- filter 函数接收 `matrix` 参数，包含当前组合的所有字段值
- filter 在矩阵组合生成后、测试策略应用前执行

**组合生成流程**：

```
1. 生成完整笛卡尔积
   原始组合数（笛卡尔积）：4 (arch) × 3 (os) × 2 (shared) = 24 种

2. 应用 filter 过滤无效组合
   过滤掉的组合：
   - darwin-mips-*（2 种）
   - darwin-riscv-*（2 种）
   - windows-riscv-*（2 种）

   剩余有效组合：24 - 6 = 18 种

3. 应用测试策略
   根据剩余 18 种有效组合应用全量测试或配对测试
```

**总结**：Filter 不改变笛卡尔积的生成算法，只是在笛卡尔积生成后进行后处理，剔除不合法的组合。

**完整示例**：

```javascript
matrix {
    Require: {
        "arch": ["x86_64", "arm64", "mips"],
        "os": ["linux", "darwin", "windows"],
        "lang": ["c"]
    },
    Options: {
        "shared": ["static", "dynamic"]
    },
    DefaultOptions: {
        "shared": ["static"]
    }
}

filter matrix => {
    // macOS 不支持 MIPS
    if matrix.require["os"] == "darwin" && matrix.require["arch"] == "mips" {
        return false
    }

    // Windows 不支持 MIPS（示例）
    if matrix.require["os"] == "windows" && matrix.require["arch"] == "mips" {
        return false
    }

    return true
}

// 组合计算：
// 原始组合：3 × 3 × 1 × 2 = 18 种
// 过滤后剔除：
//   - darwin-mips-* 剔除 2 种（2 种 options）
//   - windows-mips-* 剔除 2 种（2 种 options）
// 有效组合：18 - 4 = 14 种
```

**最佳实践**：
- Filter 应该只过滤确实无法构建的组合
- 避免过度过滤，除非确认该组合在技术上不可行
- Filter 逻辑应该清晰明确，便于维护
- Filter 中可以访问 Require 和 Options 的所有字段

### 8.3 矩阵访问接口

**matrix 对象提供的方法**：

```go
// 获取 require 字段的值
matrix.require["arch"]  // 返回 "x86_64" 或 "arm64"
matrix.require["os"]    // 返回 "linux" 或 "darwin"
matrix.require["lang"]  // 返回 "c" 或 "cpp"

// 获取 options 字段的值
matrix.options["zlib"]  // 返回 "zlibON" 或 "zlibOFF"
matrix.options["debug"] // 返回 "on" 或 "off"

// 判断相等
matrix.require["arch"] == "arm64"
matrix.options["zlib"] == "zlibON"
```

## 9. 矩阵冲突检测

### 9.1 冲突场景

当依赖包的 `require` 不是入口包的交集时，会产生矩阵冲突。

**示例场景**：
```
入口包：要求 x86_64-c-linux
依赖包：要求 arm64-c-linux

冲突：arch 字段不匹配（x86_64 vs arm64）
```

### 9.2 冲突检测流程

```mermaid
graph TD
A[解析依赖包的矩阵] --> B[提取 require 字段]
B --> C{是否是入口包的交集?}
C -- 是 --> D[继续构建]
C -- 否 --> E[报错：矩阵冲突]
E --> F[显示冲突的字段]
F --> G[终止构建]
```

### 9.3 错误信息示例

```
Error: Build matrix conflict detected

Entry package requires:
  arch: x86_64
  lang: c
  os: linux

Dependency 'madler/zlib' requires:
  arch: arm64
  lang: c
  os: linux

Conflict in field: arch (x86_64 vs arm64)

The dependency cannot be built for the target platform.
Please check the build matrix configuration.
```

## 10. 矩阵设计最佳实践

> **完整配方示例参见**：[formulas.md 第 9 章 - 完整配方示例](formulas.md#9-完整配方示例)
>
> formulas.md 中包含了完整的配方示例，涵盖所有回调函数（onSource、onRequire、filter、onBuild）和矩阵使用方法。

### 10.1 配方维护者

#### 矩阵设计原则

1. **最小化原则**：
   - 只包含必要的构建配置
   - 避免过度细化导致矩阵膨胀

2. **语义清晰**：
   - 字段名称应该清晰表达含义
   - 取值应该是自解释的

3. **合理使用 require 和 options**：
   - 环境相关的配置放在 `require`（如 arch、os）
   - 包特定的特性放在 `options`（如 debug、tests）

#### 矩阵配置示例

**好的设计**：
```json
{
    "require": {
        "arch": ["x86_64", "arm64"],
        "os": ["linux", "darwin"],
        "lang": ["c"]
    },
    "options": {
        "zlib": ["enabled", "disabled"],
        "tests": ["on", "off"]
    }
}
```

**不好的设计**：
```json
{
    "require": {
        "arch": ["x86_64", "arm64"],
        "os": ["linux", "darwin"],
        "lang": ["c"],
        "zlib": ["enabled", "disabled"],      // zlib 应该放在 options
        "compiler_version": ["9", "10", "11"] // 过度细化
    }
}
```

### 10.2 矩阵冲突处理

**场景 1：依赖包需要特定架构**

如果依赖包只支持特定架构，应该在配方中明确声明：

```javascript
matrix {
    Require: {
        "arch": ["x86_64"],  // 只支持 x86_64
        "os": ["linux"],
        "lang": ["c"]
    }
}
```

**场景 2：条件编译**

根据矩阵参数条件编译：

```javascript
onBuild => {
    args := []

    // 只在 Linux 上启用某些特性
    if matrix.require["os"] == "linux" {
        args <- "-DLINUX_SPECIFIC_FEATURE=ON"
    }

    // ARM64 需要特殊处理
    if matrix.require["arch"] == "arm64" {
        args <- "-DENABLE_ARM_OPTIMIZATIONS=ON"
    }

    cmake args
    cmake "--build" "."
    cmake "--install" "."

    // 返回构建产物
    return {
        Dir: "./install",
        LinkArgs: ["-I./install/include", "-L./install/lib", "-lmylib"]
    }, nil
}
```

### 10.3 矩阵测试

**测试策略**：

1. **全量测试**（小规模矩阵）：
   - 当矩阵组合数量较少时（< 20 个组合）
   - 测试所有矩阵组合

2. **抽样测试**（大规模矩阵）：
   - 当矩阵组合数量巨大时（> 20 个组合）
   - 随机抽样 1/10 的组合进行测试

3. **重点测试**：
   - 主流平台组合（如 x86_64-linux, arm64-darwin）
   - 极端配置组合（如全部 options 开启/关闭）

## 11. 惰性构建与矩阵

### 11.1 惰性构建的必要性

由于我们的出发点基于一个包存在巨额构建产物的前提之下，因此不可能一次性就能完成所有构建产物的构建。

#### 矩阵组合爆炸问题

矩阵采用**笛卡尔积**算法生成所有可能的组合，导致组合数随参数数量呈指数级增长。

**笛卡尔积公式**：
```
总组合数 = 选项1个数 × 选项2个数 × 选项3个数 × ...
         = |A₁| × |A₂| × |A₃| × ... × |Aₙ|（笛卡尔积大小）
```

**例子**：假设有 50 个构建选项，每个选项 2 种可能
```
2 × 2 × 2 × ... × 2 (50个2) = 2⁵⁰ = 1,125,899,906,842,624
```
**约 1125 万亿种组合** —— 预构建所有组合是不可能的。

**学术公式**：
```
Lₙ = ∏ᵢ₌₁ⁿ |Vᵢ|
```
当所有 `|Vᵢ| = k` 时：`Lₙ = kⁿ`

#### 问题分析

- 一个包的矩阵组合可能有数十甚至上百种
- 预先构建所有组合需要巨大的计算和存储资源
- 大部分组合可能永远不会被用户使用

### 11.2 惰性构建机制

**构建策略**：

| 矩阵类型 | 构建方式 | 说明 |
|----------|----------|------|
| 常用矩阵 | 预构建 + 抽样测试 | 云端预先构建主流组合 |
| 冷门矩阵 | 用户构建 → 触发云端构建 | 用户成功后通知云端缓存 |

**流程**：

```mermaid
sequenceDiagram
    participant U as User
    participant C as LLAR Cli
    participant A as LLAR API
    participant B as LLAR Backend

    U->>C: Request Binary (特定矩阵组合)
    C->>A: Query Artifact
    A->>B: Check Cache

    alt Cache Hit (常用矩阵)
        B-->>A: Artifact Found
        A-->>C: Download URL
        C->>B: Download Artifact
        B-->>C: Transfer Data
        C-->>U: Deliver Artifact
    else Cache Miss (冷门矩阵)
        B-->>A: Artifact Not Found
        A-->>C: Cache Miss
        C->>C: Local Build
        C-->>U: Deliver Local Artifact
        C->>A: Report Build Success
        A->>B: Trigger Cloud Build (async)
        Note over B: 云端构建并缓存<br/>供后续用户使用
    end
```

### 11.3 矩阵分类

**常用矩阵（预构建 + 抽样测试）**：
- 主流平台：x86_64-linux、x86_64-darwin、arm64-linux、arm64-darwin
- 主流编译器：gcc、clang 最新稳定版
- 抽样测试验证配方正确性

**冷门矩阵（用户触发）**：
- 旧版本编译器、小众架构、特殊 options
- 用户本地构建成功后，触发云端构建
- 云端缓存后，后续用户可直接下载

**优势**：
- 用户无需等待云端，本地优先
- 云端资源集中于常用组合
- 冷门组合按需扩展缓存覆盖

## 12. CLI 与矩阵

### 12.1 指定矩阵参数

用户可以通过 CLI 参数指定构建矩阵：

```bash
# 指定 require 参数
llar download DaveGamble/cJSON@1.7.18 --arch=x86_64 --os=linux --lang=c

# 指定 options 参数
llar download DaveGamble/cJSON@1.7.18 --arch=x86_64 --os=linux --lang=c --zlib=zlibON

# 使用当前平台默认值
llar download DaveGamble/cJSON@1.7.18
```
