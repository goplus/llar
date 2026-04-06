# Nix 全链路测试系统调研（含测试平台）

调研日期：2026-02-28  
范围：Nixpkgs / NixOS / Hydra / ofborg 的完整测试系统与执行平台

---

## 1. 你关心的问题，先直接回答

### 1.1 Nix 有没有“全量跑完所有组合”？

没有。Nix 公开实现不是“options 全组合穷举”，而是：

- PR 阶段：以增量触发为主（ofborg）。
- 主线/发布：跑预定义的大规模关键集合（Hydra + `release.nix` / `nixos/release.nix`）。
- 平台与测试范围通过白名单和门控机制控制（`supportedSystems`、`hydraPlatforms`、`runTestOn`）。

### 1.2 Nix 的“整个测试系统”长什么样？

可拆成 4 层：

1. 触发层：GitHub PR、定时/轮询 jobset、手动命令触发。  
2. 评估层：把 Nix 表达式求值成可执行 job/build 集合。  
3. 执行层：构建与测试在可用 builder 平台上实际跑。  
4. 产物与反馈层：日志、状态、二进制缓存、Web/API 反馈。

### 1.3 测试平台具体是什么？

从公开资料看，核心平台组件是：

- **ofborg 平台**（PR 自动 eval/build/test 协助）
- **Hydra 平台**（持续评估、调度、构建、发布聚合）
- **Nix builders 平台**（按 system/features 选择可执行机器）
- **Nix store / binary cache**（构建输入输出与复用）

---

## 2. 全链路架构（系统视角）

```mermaid
flowchart LR
  A["GitHub PR / Commit"] --> B["ofborg: eval/build/test trigger"]
  B --> C["Nix eval\n(pkgs/top-level/release.nix\n+nixos/release.nix)"]
  C --> D["Hydra Evaluator\n(jobset evaluation)"]
  D --> E["Hydra Queue Runner\n(schedule builds/tests)"]
  E --> F["Builders\n(x86_64-linux / aarch64-linux / darwin ...)"]
  F --> G["Nix Store + Build Products"]
  G --> H["Binary Cache / Channels / Hydra UI/API"]
  H --> I["Maintainer / User feedback"]
```

说明：

- 这是“控制面（触发/评估/调度） + 执行面（builder） + 产物面（store/cache/UI）”分离架构。  
- Nix 的规模控制主要在“评估输出集合”和“执行平台门控”两个位置完成。

---

## 3. 测试平台拆解（你说的“平台”）

## 3.1 Hydra 平台（主线/发布核心）

Hydra 文档与架构说明给出的核心组件：

- `hydra-server`：Web 前端/API
- `hydra-evaluator`：拉源码、评估 jobset、入队 build
- `hydra-queue-runner`：消费队列、执行构建/测试、上传结果
- PostgreSQL：配置、队列、状态元数据
- Nix store：`.drv` 与输出
- destination store / binary cache：发布产物分发

Hydra installation 文档明确：三进程都要运行系统才完整可用。  
Hydra 还支持把构建调度到“配置好的 Nix hosts”（即远程 build 机器）。

## 3.2 ofborg 平台（PR 自动化核心）

ofborg README 明确：

- PR 自动构建触发依赖 commit 标题中的 attrpath。
- PR 自动 eval 在创建和后续 commit 变化时执行。
- 支持 `@ofborg eval` / `@ofborg build ...` / `@ofborg test ...` 手动扩展。

Trusted users 段落（当前文档状态）还给出：

- 功能当前禁用说明（darwin builder 原因）
- 但仍列出其设计目标支持平台：`x86_64-linux` / `aarch64-linux` / `x86_64-darwin` / `aarch64-darwin`

## 3.3 Builder 平台（真正跑测试/构建的地方）

### Hydra 侧

NixOS Hydra 模块（nixpkgs）显示：

- 通过 `buildMachinesFiles` 配置构建机器文件
- `NIX_REMOTE_SYSTEMS` 从该配置注入，供 queue-runner 调度
- `hydra-queue-runner` 负责实际执行构建

### NixOS VM 测试侧

`nixos/lib/testing/run.nix` 显示测试 derivation 需要系统特性：

- 基础特性：`nixos-test`
- Linux：`kvm`
- Darwin host：`apple-virt`

`nixos/lib/testing/meta.nix` 显示 NixOS tests 默认 `hydraPlatforms` 为 Linux，并写明 `hydra.nixos.org` 当前不支持 Darwin 虚拟化。

---

## 4. 测试系统的“范围控制”机制（避免爆炸）

## 4.1 平台白名单：`supportedSystems`

`release-supported-systems.json`（当前公开）为：

- `aarch64-linux`
- `aarch64-darwin`
- `x86_64-linux`
- `x86_64-darwin`

`pkgs/top-level/release.nix` 把该白名单作为发布评估入口。

## 4.2 包级平台裁剪：`meta.hydraPlatforms`

`meta.chapter.md`：

- `meta.hydraPlatforms` 默认等于 `meta.platforms`
- 可以改成子集，甚至空列表 `[]`

`release-lib.nix` 的 `getPlatforms` 逻辑：

- 优先 `drv.meta.hydraPlatforms`
- 否则 `meta.platforms - meta.badPlatforms`

## 4.3 测试级平台门控：`runTestOn`

`nixos/tests/all-tests.nix` 中：

- `runTestOn = systems: arg: if elem system systems then runTest arg else { }`

即每个 test 可定义“只在哪些系统跑”，不是全系统跑。

## 4.4 测试分层：把重测试从主构建解耦

`passthru.tests` 文档明确：

- Hydra/nixpkgs-review 默认不构建
- ofborg 只在相关 PR 或手动触发时跑

这就是“主构建门禁”与“扩展重测试”解耦。

## 4.5 缓存复用

nix.dev 明确：成功测试进入 Nix store 缓存，语义输入不变则不会重复执行。

---

## 5. 端到端执行流程（按场景）

## 5.1 PR 场景

1. 开发者提交 PR。  
2. ofborg 自动 eval（PR 创建与 commit 变化）。  
3. 根据 commit 标题 attrpath 自动触发 build；需要时手动 `@ofborg test` 扩展。  
4. 结果反馈到 PR 评论/状态。  

特点：增量快反馈，不追求全量覆盖。

## 5.2 主线/发布场景

1. Hydra evaluator 周期性评估 jobsets。  
2. 新/变更构建任务入队。  
3. queue-runner 调度到 Nix hosts 执行。  
4. 产物进入 store/cache，状态出现在 Hydra UI/API。  
5. release 聚合（例如 `nixpkgs` unstable 的 release-critical constituents）。  

特点：规模大、集合预定义、偏稳定性和发布质量。

## 5.3 NixOS 集成测试场景

1. tests 由 `nixos/tests/all-tests.nix` 聚合。  
2. 按 system 映射生成 job（可 `runTestOn` 裁剪）。  
3. VM driver 执行 Python `testScript`。  
4. 产物/日志回传 Hydra。  

---

## 6. 测试类型与写法（示例）

## 6.1 包内测试（`checkPhase`）

```nix
stdenv.mkDerivation {
  pname = "demo";
  version = "1.0.0";

  doCheck = true;
  nativeCheckInputs = [ ctest ];
  checkTarget = "test";
}
```

## 6.2 独立包测试（`passthru.tests`）

```nix
stdenv.mkDerivation {
  pname = "my-tool";
  version = "1.0.0";

  passthru.tests = {
    smoke = runCommand "my-tool-smoke" { nativeBuildInputs = [ my-tool ]; } ''
      my-tool --help >/dev/null
      touch $out
    '';
  };
}
```

## 6.3 NixOS VM 测试（`runNixOSTest`）

```nix
pkgs.testers.runNixOSTest {
  name = "nginx-smoke";

  nodes.server = { ... }: {
    services.nginx.enable = true;
  };

  nodes.client = { pkgs, ... }: {
    environment.systemPackages = [ pkgs.curl ];
  };

  testScript = ''
    start_all()
    server.wait_for_unit("nginx")
    client.succeed("curl -f http://server/")
  '';
}
```

## 6.4 本地运行入口（官方文档）

`pkgs/README.md` 给了主要入口：

- `nix-build --attr pkgs.PACKAGE.passthru.tests`
- `nix-build --attr nixosTests.NAME`
- `nix-build --attr tests.PACKAGE`

---

## 7. 对你当前需求的直接结论

如果你要“完整系统 + 测试平台”的评估标准，Nix 给你的启发是：

1. **把系统拆成控制面和执行面**：触发/评估/调度与 builder 运行分离。  
2. **平台先收敛后扩展**：`supportedSystems` 与 `hydraPlatforms` 是一等控制点。  
3. **测试分层**：主构建硬门禁 + 重测试按需执行，不做无限全量。  
4. **每层都保留可操作接口**：ofborg 命令、Hydra jobset/release、flake check、本地 test 入口。

---

## 8. 证据索引（核心）

### ofborg

- 自动构建触发与 commit 标题规则：  
  https://github.com/NixOS/ofborg/blob/master/README.md#automatic-building
- PR 自动 eval：  
  https://github.com/NixOS/ofborg/blob/master/README.md#eval
- Trusted users 当前禁用说明与平台列表：  
  https://github.com/NixOS/ofborg/blob/master/README.md#trusted-users-currently-disabled
- 公开配置（`disable_trusted_users`、runner 等）：  
  https://github.com/NixOS/ofborg/blob/master/config.public.json

### Hydra

- Hydra 组件（server/evaluator/queue-runner/store/cache）：  
  https://github.com/NixOS/hydra/blob/master/doc/architecture.md
- Hydra 安装与三进程职责：  
  https://github.com/NixOS/hydra/blob/master/doc/manual/src/installation.md
- Hydra 介绍与 CI/发布定位：  
  https://github.com/NixOS/hydra/blob/master/doc/manual/src/introduction.md

### Nixpkgs / NixOS 测试体系

- `passthru.tests` 默认 CI 行为：  
  https://github.com/NixOS/nixpkgs/blob/master/doc/stdenv/passthru.chapter.md
- `meta.hydraPlatforms`：  
  https://github.com/NixOS/nixpkgs/blob/master/doc/stdenv/meta.chapter.md
- Nixpkgs release 与 `supportedSystems`：  
  https://github.com/NixOS/nixpkgs/blob/master/pkgs/top-level/release.nix  
  https://github.com/NixOS/nixpkgs/blob/master/pkgs/top-level/release-supported-systems.json
- NixOS release tests 聚合：  
  https://github.com/NixOS/nixpkgs/blob/master/nixos/release.nix
- `runTestOn`：  
  https://github.com/NixOS/nixpkgs/blob/master/nixos/tests/all-tests.nix
- NixOS test driver/system features：  
  https://github.com/NixOS/nixpkgs/blob/master/nixos/lib/testing/run.nix  
  https://github.com/NixOS/nixpkgs/blob/master/nixos/lib/testing/driver.nix  
  https://github.com/NixOS/nixpkgs/blob/master/nixos/lib/testing/meta.nix
- Hydra NixOS module（buildMachinesFiles / queue-runner 等）：  
  https://github.com/NixOS/nixpkgs/blob/master/nixos/modules/services/continuous-integration/hydra/default.nix

### Flake 与本地测试入口

- `nix flake check`：  
  https://nixos.org/manual/nix/stable/command-ref/new-cli/nix3-flake-check.html
- `pkgs/README` 的本地测试入口与示例：  
  https://github.com/NixOS/nixpkgs/blob/master/pkgs/README.md
- NixOS VM 测试教程（含缓存行为）：  
  https://nix.dev/tutorials/nixos/integration-testing-using-virtual-machines.html

---

## 9. 行号级证据（关键断言）

- Hydra 三进程职责（`hydra-server` / `hydra-evaluator` / `hydra-queue-runner`）：  
  https://github.com/NixOS/hydra/blob/master/doc/manual/src/installation.md#L144-L164
- Hydra 平台组件（DB、queue、store、destination store/cache）：  
  https://github.com/NixOS/hydra/blob/master/doc/architecture.md#L6-L37
- ofborg 自动构建规则（commit 标题 attrpath）：  
  https://github.com/NixOS/ofborg/blob/master/README.md#L9-L31
- ofborg PR 自动 eval：  
  https://github.com/NixOS/ofborg/blob/master/README.md#L67-L69
- ofborg trusted-users 当前禁用与平台列表：  
  https://github.com/NixOS/ofborg/blob/master/README.md#L125-L144
- `passthru.tests` 默认不被 Hydra/nixpkgs-review 构建：  
  https://github.com/NixOS/nixpkgs/blob/master/doc/stdenv/passthru.chapter.md#L73-L75
- `meta.hydraPlatforms` 默认与可裁剪：  
  https://github.com/NixOS/nixpkgs/blob/master/doc/stdenv/meta.chapter.md#L143-L151
- release 平台白名单（`supportedSystems`）：  
  https://github.com/NixOS/nixpkgs/blob/master/pkgs/top-level/release-supported-systems.json
- `runTestOn` 系统门控：  
  https://github.com/NixOS/nixpkgs/blob/master/nixos/tests/all-tests.nix#L131
- NixOS tests `requiredSystemFeatures`（`nixos-test` / `kvm` / `apple-virt`）：  
  https://github.com/NixOS/nixpkgs/blob/master/nixos/lib/testing/run.nix#L96-L100
- NixOS tests 默认 `hydraPlatforms = linux` 的说明：  
  https://github.com/NixOS/nixpkgs/blob/master/nixos/lib/testing/meta.nix#L46-L53
- Hydra 模块中的构建机入口（`buildMachinesFiles`）：  
  https://github.com/NixOS/nixpkgs/blob/master/nixos/modules/services/continuous-integration/hydra/default.nix#L226-L235

---

## 10. 边界说明（避免误读）

1. 本文所有事实项都来自公开文档或源码。  
2. “Hydra 安装手册中 Linux 支持描述”与“README 强调 NixOS 模块部署路径”语境不同：一个讲运行条件，一个讲推荐部署方式。  
3. “Nix 不做 options 全组合穷举”是基于 release/jobset/runTestOn/hydraPlatforms 的公开实现推断，非内部私有策略猜测。
