# LLAR 项目展示文档

<p align="center">
  <img width="200" height="200" alt="LLAR Logo" src="https://github.com/user-attachments/assets/38f48dc3-6676-420e-9745-c258ff8d487c" />
</p>

<p align="center"><strong>Cloud-based Package Manager for Native Libraries</strong></p>

---

## 📋 项目概述

**LLAR** 是一个专为 C/C++ 等原生语言设计的云端包管理服务，旨在解决原生库依赖管理复杂、构建配置困难的痛点。

### 核心价值

- **简化依赖管理**：类似 Go modules 的依赖声明方式，一个命令即可添加和构建依赖
- **自动化构建**：通过 Formula（构建配方）自动下载源码并完成构建
- **版本一致性**：采用 MVS 算法确保依赖版本的可重现性
- **跨平台支持**：统一的构建抽象，支持 Linux、macOS、Windows

---

## 🎯 核心特性

### 1. Minimal Version Selection (MVS)
借鉴 Go modules 的依赖解析算法，确保：
- 可预测的依赖版本选择
- 避免依赖地狱
- 构建结果可重现

### 2. Formula 构建系统
使用 XGo 语言编写的构建配方：
- 声明式依赖定义
- 灵活的构建逻辑
- 支持多版本适配

### 3. 智能缓存机制
- 构建结果缓存
- Formula 缓存
- 多进程安全锁

### 4. 版本控制集成
- 自动从 GitHub 拉取源码
- 基于 Git Tag 的版本管理
- 支持版本约束

---

## 🏗️ 系统架构

### 整体架构图

```mermaid
graph TB
    subgraph "CLI 命令层"
        CLI[llar CLI]
        INIT[init 初始化]
        GET[get 添加依赖]
        BUILD[build 构建]
        INSTALL[install 安装]
        TIDY[tidy 整理]
    end

    subgraph "核心引擎层"
        MODLOAD[ModLoad<br/>模块加载器]
        MVS[MVS Algorithm<br/>依赖解析]
        BUILDER[Builder<br/>构建管理]
        FORMULA[Formula Manager<br/>配方管理]
    end

    subgraph "基础设施层"
        VCS[VCS<br/>版本控制]
        CACHE[Build Cache<br/>构建缓存]
        LOADER[XGo Loader<br/>脚本加载器]
        LOCK[File Lock<br/>并发控制]
    end

    subgraph "数据层"
        VERSIONS[versions.json<br/>依赖配置]
        FORMULA_FILES[*.gox<br/>Formula 文件]
        BUILD_OUTPUT[build/<br/>构建产物]
    end

    CLI --> INIT
    CLI --> GET
    CLI --> BUILD
    CLI --> INSTALL
    CLI --> TIDY

    GET --> MODLOAD
    BUILD --> MODLOAD
    INSTALL --> MODLOAD
    TIDY --> MVS

    MODLOAD --> MVS
    MODLOAD --> FORMULA
    MODLOAD --> BUILDER

    BUILDER --> CACHE
    BUILDER --> LOCK
    BUILDER --> VCS

    FORMULA --> LOADER

    MODLOAD -.读取.-> VERSIONS
    GET -.写入.-> VERSIONS
    TIDY -.更新.-> VERSIONS

    FORMULA -.加载.-> FORMULA_FILES
    VCS -.下载.-> FORMULA_FILES

    BUILDER -.输出.-> BUILD_OUTPUT
```

### 依赖解析流程

```mermaid
sequenceDiagram
    participant User
    participant CLI
    participant ModLoad
    participant MVS
    participant Formula
    participant VCS

    User->>CLI: llar build
    CLI->>ModLoad: LoadPackages()

    ModLoad->>Formula: 加载主模块 Formula
    Formula->>ModLoad: OnRequire() 返回依赖列表

    ModLoad->>MVS: BuildList() 构建依赖图

    loop 递归加载依赖
        MVS->>Formula: 加载依赖的 Formula
        Formula->>VCS: 下载 Formula 文件
        VCS-->>Formula: 返回文件内容
        Formula->>MVS: OnRequire() 返回子依赖
    end

    MVS-->>ModLoad: 返回完整依赖列表

    ModLoad->>CLI: 按依赖顺序构建
    CLI-->>User: 构建完成
```

### 构建流程

```mermaid
flowchart TD
    START([开始构建]) --> READ_VERSIONS[读取 versions.json]
    READ_VERSIONS --> INIT_BUILDER[初始化 Builder]
    INIT_BUILDER --> SYNC_FORMULA[同步 Formula 仓库]

    SYNC_FORMULA --> LOAD_PKG[LoadPackages]

    LOAD_PKG --> LOAD_MAIN[加载主模块 Formula]
    LOAD_MAIN --> GET_DEPS[执行 OnRequire 获取依赖]
    GET_DEPS --> MVS_CALC[MVS 算法计算依赖图]
    MVS_CALC --> PARALLEL_LOAD[并行加载所有 Formula]

    PARALLEL_LOAD --> SORT_DEPS{拓扑排序依赖}

    SORT_DEPS --> BUILD_LOOP[遍历依赖列表]

    BUILD_LOOP --> CHECK_CACHE{检查缓存}
    CHECK_CACHE -->|命中| NEXT_DEP[下一个依赖]
    CHECK_CACHE -->|未命中| ACQUIRE_LOCK[获取构建锁]

    ACQUIRE_LOCK --> SYNC_CODE[同步源代码]
    SYNC_CODE --> EXEC_BUILD[执行 OnBuild]
    EXEC_BUILD --> SAVE_RESULT[保存构建结果]
    SAVE_RESULT --> RELEASE_LOCK[释放锁]
    RELEASE_LOCK --> NEXT_DEP

    NEXT_DEP --> BUILD_LOOP

    BUILD_LOOP --> BUILD_MAIN[构建主模块]
    BUILD_MAIN --> OUTPUT[输出 pkg-config 信息]
    OUTPUT --> END([构建完成])
```

---

## 💻 技术架构

### 核心模块

| 模块 | 职责 | 关键技术 |
|------|------|----------|
| **modload** | 模块加载与依赖解析 | MVS 算法、并行加载 |
| **mvs** | 最小版本选择算法 | 图算法、版本比较 |
| **build** | 构建系统 | 缓存机制、文件锁 |
| **formula** | 构建配方管理 | XGo 脚本、AST 解析 |
| **vcs** | 版本控制 | Git 集成 |
| **loader** | 脚本加载器 | XGo 解释器 (ixgo) |

### 技术栈

```mermaid
graph LR
    subgraph "开发语言"
        GO[Go 1.24]
        GOPLUS[XGo GoPlus]
    end

    subgraph "核心框架"
        COBRA[Cobra CLI]
        IXGO[ixgo 解释器]
        XMOD[x/mod 模块工具]
    end

    subgraph "基础设施"
        GIT[Git VCS]
        FILELOCK[文件锁]
        CACHE[构建缓存]
    end

    GO --> COBRA
    GOPLUS --> IXGO
    GO --> XMOD
    GO --> GIT
    GO --> FILELOCK
    GO --> CACHE
```

---

## 📊 关键数据流

```mermaid
graph LR
    A[versions.json<br/>依赖声明] --> B[module.Version<br/>模块标识]
    B --> C[Formula<br/>构建配方]
    C --> D[Project<br/>构建上下文]
    D --> E[BuildResult<br/>构建产物]
    E --> F[pkg-config<br/>集成信息]
```

---

## 🚀 使用示例

### 初始化项目
```bash
llar init
```

### 添加依赖
```bash
llar get github.com/example/libfoo@v1.2.3
```

### 构建项目
```bash
llar build
```

### 安装单个包
```bash
llar install github.com/example/libbar@latest
```

### 整理依赖
```bash
llar tidy
```

---

## 📈 项目状态

### 当前进度
- ✅ 核心 MVS 算法实现
- ✅ Formula 系统设计
- ✅ 基础 CLI 命令
- ✅ 构建缓存机制
- 🚧 构建器功能增强（当前分支：feat/builder）

### 已实现功能
- Minimal Version Selection 依赖解析
- XGo 语言编写的 Formula 系统
- 多进程安全的构建机制
- Git 版本控制集成
- 智能构建缓存
- pkg-config 信息生成

---

## 🎁 核心优势

### 1. 开发效率提升
- 一行命令添加依赖，无需手动配置
- 自动化构建流程，减少人工干预
- 智能缓存机制，加速重复构建

### 2. 版本管理可靠
- MVS 算法确保依赖版本一致性
- 避免版本冲突和依赖地狱
- 构建结果可重现

### 3. 扩展性强
- Formula 系统支持灵活的构建逻辑
- 插件化的 VCS 接口
- 自定义版本比较器

### 4. 跨平台兼容
- 统一的构建抽象
- 支持 Linux、macOS、Windows
- 构建矩阵支持多平台配置

---

## 📚 相关文档

- [Formula 开发指南](doc/formula.md)
- [项目 README](README.md)

---

<p align="center">
  <em>让原生库依赖管理像 Go modules 一样简单</em>
</p>
