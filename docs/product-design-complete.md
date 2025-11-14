# LLAR 产品设计文档（完整版）

## 1. 项目背景与出发点

### 1.1 问题定义

在C/C++开发中，我们经常会遇到这样一类问题：不同的库或者Module存在大量可选编译配置，以及有大量编译组合，通常更换一个配置或者换个构建平台，我们只能重头把这些库构建一遍。

这样行为其实相当浪费时间，因为这样重复的构建往往是没有意义的。

于是为了节约时间，有许多包管理器都提出了"预构建"包选项，例如著名的 Homebrew，Conan和APT等。

然而这些包管理器，为了节省空间，往往不对所有可能的选项进行构建，依然要用户本地构建。

同时，我们注意到，不只有C/C++存在这类问题，例如Python，WASM也有对应的问题。

### 1.2 LLAR的设计出发点

LLAR 虽然有反复造轮子的意味，但是LLAR设计出发点与conan，homebrew，xmake等工具是不同的。

Conan和Xmake都在关注C/C++生态编译，在这方面做了大量的工作，而Homebrew，APT等工具目标仅仅是为了提供能用的预构建包。虽然Conan尝试做预构建与编译方面的平衡，想要打造一款全能的构建工具，然而，Conan在这方面努力显然是不足的，因为他们管理不够完整，仅提供了部分的管理。这个问题出现的原因不言而喻--Conan其本身定位是构建工具，并非预构建下载。另一个Xmake，也有同样问题，虽然他们提供了Xrepo，但是Xrepo更多是Xmake扩展，而并非是Xmake关心的，他们更多也是关注在编译构建工具方面。

然而单独从编译构建工具来说，已经有太多太多成熟的工具了，GNU工具链，Ninja，Cmake.....我们没有必要再额外造一个编译构建工具。Conan的成功正是因为它不仅仅是一款构建工具，更重要的是，他为开发者提供了成熟的资产管理方案。

因此，LLAR出发点就是，我们想创造一款，既有完整资产管理方案（为预构建包提供在线编译管理需求），又可以提供便捷编译配置的包管理工具。换句话说，我们想追求的是二者的平衡。

### 1.3 名称含义

LLAR 名称来源是：`LL` + `AR`，AR为Archive缩写，C/C++的库打包后称为Archive，`LL`意思是它为`LLGo`类似的编译器设计，只不过它可以独立于`LLGo`

## 2. 竞品调研与机会分析

### 2.1 竞品分析

**Conan**: 通过 `Recipe` 定义包信息，根据环境设置查询远程仓库，优先下载预构建的二进制包，缺失时自动从源码构建，同时缓存结果供后续重用。而Recipe则使用Python进行编写。

**Homebrew**: 通过名为 `Formula` 的 Ruby 脚本来定义软件的编译规则与依赖关系；在用户执行安装命令时，它会优先从`Bottles`（预编译的二进制包）直接下载安装以提升效率，若没有对应平台的Bottles则自动在本地从源码编译。

**Xmake**: 类似于 `Conan` 和 `Homebrew`，只不过在二者基础上添加了多种自定义源，它可以从Conan或者VCPkgs下载包并进行安装

### 2.2 机会与意义

现有包管理工具无法预构建巨量的构建产物，多数都采取了云端预构建与用户自构建结合做法，即当不存在预构建产物时候就要求用户自行构建。

这样做法对于用户体验而言，是极其失败的，因为：

1. 由于用户不同需求，产生的构建产物无法被满足，多数情况下，所谓"通用"的预构建产物形同虚设，举个例子，当一个macOS x86_64平台经常缺少预构建包，大多数包管理器认为这个平台用户不够多而被忽略
2. 反复构建相同产物，极其浪费时间，这些产物可以被云端缓存起来

LLAR 是第一个尝试完全解决预构建产物问题的平台

## 3. 产品需求与设计目标

### 3.1 核心需求

1. 满足一个包多种构建产物带来的巨额产物云构建，存储需求
2. 提供一种规范化，标准化的包构建，管理方案

### 3.2 基本概念

#### 包 Package
我们将LLAR一个独立的库单位称之为包，之所以称之为包，是因为其包含以下几部分：

1. 构建信息
2. 版本信息

而构建信息又包含以下两部分：
1. 构建配方
2. 构建矩阵

#### 构建配方 Formula
构建配方，用于告诉构建者该包如何完成构建。

#### 构建矩阵 Build Matrix
构建矩阵，由于一个包可能只存在一种配方，但是这一种配方因为外部需求的变化会导致多种产物，为了代表这类变化，我们使用一个构建矩阵表达

#### 惰性构建 Lazy Compilation
由于我们出发点基于一个包存在巨额构建产物前提之下，因此不可能一次性就能完成所有构建产物的构建。我们提出了"惰性编译"方案以缓解这一点，
"惰性构建"并不是类似于常见包管理预构建和用户构建结合，而是：不存在的预构建包，用户和云端并发构建，构建完毕则无需用户构建，直接拉取云端缓存。

#### 中心化配方管理仓库 Formula Repository
配方仓库用于存放和管理构建配方，一般来说是基于GitHub之类的git协议管理平台

## 4. 用户故事与用户画像

### 4.1 用户画像
**目标用户**：使用模块拆分编译类语言（如C/C++），且对编译速度有追求的用户

### 4.2 用户故事

#### 用户（开发者）

用户可以：
- **安装包到本地**：通过 `LLAR Cli` 从中心化配方管理仓库获得需要包的配方，并通过`xgo run`执行配方，使用"惰性构建"设计，如果云端不存在预构建包，则触发云端和用户本地构建，否则直接下载云端预构建包
- **获取包的相关信息**，如构建信息，版本信息
- **仅下载预构建包**
- **仅下载包源码**

其流程图如下：

```mermaid
graph TD
    A[用户启动LLAR Cli] --> B[从中心化配方仓库获取包配方]
    B --> C[执行配方]
    C --> D{云端存在预构建包？}
    D -->|是| E[下载云端预构建包]
    D -->|否| F[触发构建过程]
    F --> G[进行云端构建]
    F --> H[进行用户本地构建]
    G --> I[构建完成]
    H --> I
    E --> I
```

#### 维护者

维护者可以（通过Pull Request）：
- **提交相关配方**至中心化配方管理仓库
- **更新中心化配方管理仓库的配方**

流程图如下：

```mermaid
flowchart TD
    A[维护者添加或更新配方] --> B[创建Pull Request]
    B --> C{审核是否通过?}
    C -->|否| D[根据反馈修改]
    D --> B
    C -->|是| E[合并至主仓库]
```

## 5. 包设计

包是我们定义的一个抽象的概念，其实现载体为配方(Formula)。

一个包存在Package Name， Package ID，Desc（描述），Homepage（原库主页）

### 5.1 LLAR Package Name

格式：`Owner/Repo`

这个一般取自源库来源

我们举个例子，`github.com/DaveGamble/cJSON`

其LLAR Package Name应该为: `DaveGamble/cJSON`

如果是非Github托管的，看起来不存在此类格式，不过，我们依然可以取得这类名称，因为Package Name并无来源限制

举个例子：`https://mumps-solver.org/MUMPS_1.2.tar.gz`

维护者可以给他取 `mumps-solver/MUMPS`

Package Name很重要的一个用途就是将该package以人类可阅读的形式展示给用户，并不用于标识符使用

#### 限制
Package Name唯一限制就是要求必须是ASCII中的可打印字符，不允许其他字符。

这个限制主要是因为，Package Name作为给用户展示的字符，不应该存在一些看起来乱码的字符

## 6. 构建流程设计

### 6.1 惰性构建流程

LLAR 采用 "惰性编译" 策略。这意味着当配方被合并时，系统并不会预先为所有可能的配置构建二进制包。相反，当用户请求一个二进制包时，LLAR Cli会通过 LLAR API查询 LLAR Backend。

如果请求的制品在后端已经存在，则会直接为用户下载。
如果未找到该制品，用户的请求会被提交给 LLAR Backend，以在云端触发一个新的构建任务。同时，为防止用户等待，系统会启动一个后备方案：在用户本地机器上也同时发起构建过程。

#### Cache Hit
```mermaid
sequenceDiagram
    participant U as User
    participant C as LLAR Cli
    participant A as LLAR API
    participant B as LLAR Backend

    U->>C: Request Binary
    C->>A: Query Artifact
    A->>B: Check Cache

    B-->>A: Artifact Found
    A-->>C: Download URL
    C->>B: Download Artifact
    B-->>C: Transfer Data
    C-->>U: Deliver Artifact
```

#### Cache Miss
```mermaid
sequenceDiagram
    participant U as User
    participant C as LLAR Cli
    participant A as LLAR API
    participant B as LLAR Backend
    participant S as Build System

    U->>C: Request Binary
    C->>A: Query Artifact
    A->>B: Check Cache

    B-->>A: Artifact Not Found
    A-->>C: Cache Miss Notification

    Note over U,S: Parallel Build Process
    B->>S: Trigger Cloud Build
    C->>U: Start Local Build

    S-->>B: Store Build Result
```

### 6.2 构建配方需求

#### 需求
- 为用户提供便捷的，简单环境来描述一个包构建方式
- 对包的依赖（版本和依赖）进行管理
- 为LLAR 用户API提供SDK

#### 基本概念

##### 源 Source
描述包的源码来源，一般为该项目源码的URL。但源并不局限于HTTP，它应该是一个通用的接口，允许用户使用非HTTP源。

##### 构建工具 Tool
虽然LLAR的构建配方可以作为构建工具使用，但对于许多C/C++项目来说，往往离不开传统的构建工具，例如CMake等。

#### 用户故事
用户可以：
- 描述构建过程
- 加载其他包构建配方，以实现依赖管理
- 自动解决版本冲突，解决构建矩阵冲突
- 自动解决工具链依赖(TODO)

#### 构建流程
总的流程分为两部分：Build -> Test

##### Build
Build 为构建流程，在此过程，应该完成：

1. 从源中拉取代码
2. 使用构建工具对代码进行构建，得到构建配置
3. 对于静态语言而言：使用构建配置，得到二进制产物；对于动态语言而言：使用构建配置，对产物进行打包

##### Test
Test 为测试过程，对于静态语言而言，使用产物进行编译测试即可；对于动态而言，需要进行运行测试

##### 流程图

```mermaid
graph TD
    Start(开始) --> A[从源中拉取代码]
    A -- 构建工具 --> B[构建配置]
    B --> C{语言类型?}
    C -->|静态语言| D[得到二进制产物]
    C -->|动态语言| E[对产物进行打包]
    D --> F[进行编译测试]
    E --> G[进行运行测试]
    F --> End(流程结束)
    G --> End
```

## 7. LLAR CLI设计

### 7.1 产品设计

#### 背景
LLAR Cli是将LLAR与用户连接的"门户"("Gateway")，用户通过LLAR Cli与LLAR进行互动

#### 需求
- 为用户提供统一的Cli接口用于LLAR管理
- 管理用户使用的配方脚本

#### 用户故事
用户可以：
- **获取已经编译或者打包好的Package**，以进行后续的编译操作
- **编译还没编译的Package**，以进行后续的编译操作
- **获取编译好或者未编译好的Package版本信息**，以了解目前有什么版本
- **获取编译好或者未编译好的Package构建信息**，以了解目前有什么依赖
- **下载Package源码**，但不进行编译
- **根据Package Name搜索Package**
- **从多个重名Package中**，提供具体信息让用户选择
- **执行配方**，生成配方
- **添加第三方源**（TODO）

### 7.2 具体设计

#### 预缓存Package
```bash
llar download <package>[@version]
```

参数：
- `-s` / `--source`: 仅获取源码不需要二进制包（默认仅获取二进制)
- `-a` / `--all`: 不仅获取源码还获取二进制（同一个目录）
- `-json`: 以JSON格式输出(默认无格式，仅方便人类阅读和编译器处理)

输出内容：包的构建信息，例如`-lcjson`

例子：
```
-lcjson -L/xxxx/cjson -I/xxxx/cjson/include
```

JSON格式：
```json
{
    "LDFlags": "-L/xxxx/cjson -lcjson",
    "CFlags": "-I/xxxx/cjson/include"
}
```

内部流程图：
```mermaid
graph TD
A[用户] --> B{检查中心化仓库是否存在}
B -- 是 --> C{检查更新}
B -- 否 --> D[拉取中心化仓库]
C -- 有更新 --> E[更新仓库]
C -- 无更新 --> F[根据Package Name查找配方]
E --> F
D --> F
F --> G[执行配方]
G --> H[惰性构建]
H --> I[结束]
```

#### 获取Package版本信息
```bash
llar list <package>
```

`-json`: 以JSON格式输出(默认无格式，仅方便人类阅读和编译器处理)

输出内容：包的版本信息

例子：
```
1.7.18
1.7.17
```

JSON格式：
```json
[{
    "Version": "1.7.18"
},
{
    "Version": "1.7.17"
}]
```

#### 获取Package构建信息
```bash
llar build info <package>[@version]
```

参数：
- `-f` / `--filter`: 正则过滤掉不需要的参数
- `-m` / `--match`: 正则匹配需要的参数

如果二者组合使用，那么先过滤，后匹配

#### 依赖管理

##### 初始化Package
```bash
llar init
```

无输出

##### 为当前Package添加依赖
```bash
llar get <package>[@version]
```

缺省：latest

例子: `llar get madler/zlib@1.2.1`

##### 整理当前依赖
```bash
llar tidy
```

#### 搜索Package
用户可以通过Package Name来模糊搜索Package

```bash
llar search <keyword>
```

`-json`: 以JSON格式输出(默认无格式，仅方便人类阅读和编译器处理)

输出：
```
Dave/cJSON:
    Desc: fast cJSON
    Homepage: xxxx

John/cjson:
    Desc: ultra fast cJSON, faster than above one
    Homepage: xxxx
```

JSON格式：
```json
[{
    "PackageName": "Dave/cJSON",
    "Homepage": "xxxx"
},
{
    "PackageName": "John/cjson",
    "Homepage": "xxxx"
}]
```

## 8. 配方管理仓库设计

### 8.1 背景
LLAR需要为相关配方提供存储管理，于是我们需要为其设计一个存储的仓库

### 8.2 具体设计

我们选用了GitHub作为我们默认的中心化配方管理仓库

#### 为什么选用GitHub
1. 公开，透明
2. 自带GitHub Actions这类的检查工具，能够帮助我们做自动化检查

其目录结构如下：

```
{{owner}}/
└── {{repo}}/
    ├──  versions.json
    └── {{repo名称首字母大写}}_llar.gox
```

 `_llar.gox`, `versions.json` 为必须入库文件
`go.mod`, `go.sum` 可选，当配方需要`import`时候必须入库，没有也可以

当然，如果存在因为库版本导致配方发生变更，也可以写成这样：

```
{{owner}}/
└── {{repo}}/
    ├── 1.x/
    │   └── {{repo名称首字母大写}}_llar.gox
    └── 2.x/
        └── {{repo名称首字母大写}}_llar.gox
```

**示例：** `github.com/DaveGamble/cJSON`

对应的目录结构将是：

```
DaveGamble/
└── cJSON/
    └── CJSON_llar.gox
```

### 8.3 CI系统

#### 产品设计

##### 需求

提交侧(Pull Request)：
- 检查配方是否编写正确
    - 检查是否加载并继承`FormulaApp`
    - 检查Package Name和Package ID是否已经填写
    - 检查依赖图是否无法自动完成解决或者有构建矩阵的冲突
- 运行配方构建，得到产物后运行测试
    - 超过20种可能性就随机抽样1/10构建矩阵组合，因为如果产生大量构建矩阵组合，将无法全部测试

##### 用户故事
用户可以：
- 使用PR提交到中心化仓库

## 9. 模块划分

### 9.1 系统架构

```mermaid
graph LR
    subgraph 0[" "]
        subgraph 1[应用层]
            1.1[LLAR Cli]
            1.2[LLAR 配方模块]
        end
        subgraph 2[中间层]
            2.1[LLAR 用户 API]
            2.2[LLAR 数据管理 API]
        end
        subgraph 0.1["LLAR 后端"]
            subgraph 3[数据处理层]
                3.1[消息处理模块]
                3.2[(计算集群)]
                3.3[集群调度模块]
            end
            subgraph 4[数据管理层]
                4.1[云存储模块]
                4.2[配方产物管理模块]
                4.3[构建任务管理模块]
                4.4[(中心化配方管理仓库)]
            end
        end
    end
```

### 9.2 模块输入输出

#### 依赖管理模块
功能：
1. 增量添加依赖
2. 通过Go MVS算法计算依赖有向图
3. 解析`versions.json`

输入：`versions.json` 的`[]byte` 或者文件路径

#### LLAR Cli模块
提供用户交互界面

#### FormulaApp
提供配方执行环境

#### ixgo运行模块
功能：
1. 自动配置xgo项目（RegisterProject)
2. 根据用户需求，找到需要的配方
3. 与依赖管理模块互动
4. 根据依赖管理模块，执行构建

#### 依赖管理模块互动
```mermaid
graph TD
A["初始化：调用配方Main()"] --> B[执行配方onRequire回调]
B --> C[依赖管理模块]
C --> D[获取依赖有向图]
D --> E[获得Buildlist]
E --> F[根据Buildlist执行配方构建]
```

## 10. MVP实现现状

### 10.1 MVP发现的问题
基于Issue #26，MVP发现的问题：

1. ixgo export.go与源码导入混用有副作用
2. 配方如何管理（需要讨论）
3. 产物输出目录（需要讨论）
4. 由于`compare`放在配方中会导致其存在版本变化，所以单独拆分出了`_version.gox`配方放`compare`
5. 产物信息传递

MVP：https://github.com/MeteorsLiu/llar-mvp
配方仓库：https://github.com/MeteorsLiu/llarformula

## 11. 业务价值与意义

### 11.1 解决的核心问题
1. **时间节约**：避免重复构建相同配置的包，大幅提升开发效率
2. **资源优化**：云端集中构建，减少本地资源消耗
3. **完整覆盖**：第一个尝试完全解决预构建产物问题的平台
4. **标准化**：提供规范化的包管理和构建方案

### 11.2 商业价值
- 为Go+/XGO生态提供完整的包管理解决方案
- 降低C/C++开发的技术门槛
- 提升跨平台开发效率
- 建立开放的包管理生态

## 12. 风险与挑战

### 12.1 技术挑战
- 大规模构建产物的云端管理和存储
- 复杂依赖关系的自动解析
- 构建环境的一致性保证
- 系统的高可用性和可扩展性

### 12.2 生态挑战
- 用户接受度和迁移成本
- 维护者社区的建设
- 与现有工具链的兼容性
- 配方质量的保证机制

---

*本文档基于LLAR项目开放issues (#9, #11, #12, #14, #15, #17, #18, #21, #22, #23, #24, #25, #26) 的完整分析整理，全面反映了LLAR的产品设计思路和发展方向。*