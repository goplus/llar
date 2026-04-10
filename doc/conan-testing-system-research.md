# Conan 测试系统深度调研（含测试平台、复杂矩阵与测试例子）

调研日期：2026-02-28  
文档范围：Conan 2.x 官方文档、ConanCenter Index（CCI）官方规范与 FAQ、Conan 官方 examples2 示例

---

## 1. 先回答你最关心的问题

### 1.1 Conan 是“全量测试”吗？

不是。

- 在 ConanCenter（公共二进制服务）侧，做的是“固定 profile 列表 + `package_id` 去重后构建”，不是 options 全组合穷举。
- 在组织自建 CI（Conan 官方 CI 教程）侧，推荐“增量构建 + 产品级集成验证”，而不是全库全反向依赖全量重测。

### 1.2 Conan 面对庞大构建矩阵，核心策略是什么？

核心是 5 个动作：

1. 固定平台/配置集（profiles）而不是全组合。
2. 用 `package_id` 去重，避免重复构建。
3. 用 `conan graph build-order` 只重建需要重建的包，并按拓扑分层并行。
4. 用 `conan graph build-order-merge` 合并“多产品 x 多配置”构建计划，消除重复。
5. 用 lockfile 保证多机器并行时依赖版本一致。

### 1.3 Conan 有没有官方 pairwise（两两组合）测试方案？

公开文档里没有把 pairwise 作为官方策略。  
公开推荐路线是 `package_id`/binary model + build-order + lockfile + 产品流水线验证。

---

## 2. Conan 的“整个测试系统”到底包含哪些层

Conan 生态里“测试系统”要分三层看：

1. 配方级验证（`conan create` + `test_package` / `conan test`）  
作用：验证“这个包能被消费者正确使用”（smoke/consumer check）。

2. 包构建服务层（ConanCenter）  
作用：按既定平台矩阵批量产出公共二进制，不承担上游完整测试套件回归。

3. 组织 CI 集成层（官方 CI tutorial 的 packages/products pipeline）  
作用：验证“变更是否破坏组织关键产品”，并把通过验证的包逐级晋升（promote）。

这三层组合起来，才是 Conan 的完整测试平台思路。

---

## 3. ConanCenter（CCI）如何测试大矩阵

## 3.1 CCI 跑什么

CCI 文档明确写了流程：

- 对每个 Conan reference 遍历固定 profile 列表；
- 计算每个 profile 的 `packageID`；
- 去掉重复后，只构建剩余组合；
- PR 合并后再晋升到 ConanCenter。

文档还给了量级：当前一个 C++ 库可生成约 30 个二进制包。

## 3.2 CCI 不跑什么

CCI FAQ 明确：不在 recipe 里构建/执行上游 testsuite。主要理由：

- 100+ 配置下成本太高；
- CCI 的定位是二进制构建服务，不是上游库集成测试系统。

## 3.3 CCI 如何控制 options 维度

CCI 对常见 options 有明确约束：

- `shared`：默认建议 `False`，CI 会生成 `True/False` 组合。
- `fPIC`：默认建议 `True`，CI 会生成 `True/False` 组合（适用场景下）。
- `header_only`：默认 `False`，若存在该选项，CI 会额外加 `header_only=True`，但只产出一个 header-only 包。

CCI 还明确建议不要加 `build_testing` 这类选项，建议使用 `skip_test` 相关配置。

## 3.4 平台实现细节公开度

CCI FAQ 同时明确：其 Jenkins orchestration 库目前不公开。  
所以“内部调度代码怎么实现”无法从公开资料完整复原。

---

## 4. Conan CLI 的测试机制（配方作者层）

## 4.1 `conan create`

`conan create` 默认行为是：

- 创建包；
- 若存在 `test_package`，执行消费者测试工程验证包可用性。

常见控制项：

- `--test-folder=""`：跳过测试阶段。
- `--build=missing`：只有缺二进制时才从源码构建。
- `--test-missing`：配合 `--build=missing`，仅当本次确实源码构建时才跑 `test_package`。

## 4.2 `conan test`

`conan test` 可以独立执行 `test_package`，用于验证指定 reference。  
也支持 `--build=missing` 和 lockfile 参数体系。

## 4.3 官方 `test_package` 示例（可直接参考）

来源：Conan 官方 `examples2`

```python
from conan import ConanFile
from conan.tools.cmake import CMake, cmake_layout
from conan.tools.build import can_run
import os

class helloTestConan(ConanFile):
    settings = "os", "compiler", "build_type", "arch"
    generators = "CMakeDeps", "CMakeToolchain"

    def requirements(self):
        self.requires(self.tested_reference_str)

    def build(self):
        cmake = CMake(self)
        cmake.configure()
        cmake.build()

    def layout(self):
        cmake_layout(self)

    def test(self):
        if can_run(self):
            cmd = os.path.join(self.cpp.build.bindir, "example")
            self.run(cmd, env="conanrun")
```

重点：

- 这是消费者视角 smoke test，不是上游完整单测回归。
- 官方也建议 `test_package` 尽量只依赖被测包本身，不要引入复杂额外依赖。

---

## 5. Conan 官方 CI 方案如何做“规模化快速测试”

这里是你最关心的部分：不是只讲命令，而是讲系统。

## 5.1 两条流水线分工（packages / products）

官方 CI 教程把系统拆成两条线：

- packages pipeline：某个包变更后，先把该包在多配置下构建出来。
- products pipeline：再验证组织关键产品是否还能正确集成（必要时重建中间消费者）。

这比“全量反向依赖全跑”更可控，也比“只测当前包”更安全。

## 5.2 三仓库晋升模型（测试平台核心）

官方推荐至少三类仓库：

- `packages`：暂存 package pipeline 产物；
- `products`：暂存 products pipeline 验证产物；
- `develop`：对开发者和常规 CI 暴露的稳定仓库。

只有通过前一阶段验证，才 promote 到下一仓库。  
本质上是“分阶段放行”，避免坏包直接污染主仓库。

## 5.3 build-order：只重建必要包，而非全图重建

`conan graph build-order` 用于算“哪些包要重建、顺序是什么”。

典型命令（官方教程）：

```bash
conan graph build-order --requires=game/1.0 --build=missing --order-by=recipe --format=json > game_release.json
conan graph build-order --requires=game/1.0 --build=missing --order-by=recipe -s build_type=Debug --format=json > game_debug.json
```

输出 `order` 是“列表中的列表”（按层）：

- 同一层可以并行构建；
- 下一层必须等上一层完成。

这就是大图并行调度的关键。

## 5.4 多产品多配置去重：build-order-merge

多产品（如 `game`、`mapviewer`）+ 多配置（Release/Debug）时，官方建议：

1. 先分别计算每个 build-order（不要先 `--reduce`）。
2. 再统一 `build-order-merge`，最后 `--reduce` 得到最终“仅需构建项”。

```bash
conan graph build-order-merge \
  --file=game_release.json \
  --file=game_debug.json \
  --file=mapviewer_release.json \
  --file=mapviewer_debug.json \
  --reduce --format=json > build_order.json
```

这样可以减少重复构建，避免“多流水线各自重复打包同一二进制”。

## 5.5 lockfile：分布式并行时防止依赖漂移

官方教程强调：多机并行时若不锁依赖，可能不同配置拉到不同依赖版本，导致同版本包行为不一致。  
做法是先聚合 lockfile，再把同一个 lockfile 分发给各构建 agent。

示例：

```bash
conan lock create --requires=game/1.0 --lockfile-out=conan.lock
conan lock create --requires=game/1.0 -s build_type=Debug --lockfile=conan.lock --lockfile-out=conan.lock
conan lock create --requires=mapviewer/1.0 --lockfile=conan.lock --lockfile-out=conan.lock
conan lock create --requires=mapviewer/1.0 -s build_type=Debug --lockfile=conan.lock --lockfile-out=conan.lock
```

---

## 6. 你问的“改 zlib 怎么测”在 Conan 里的可落地流程

假设 `zlib` 改了，且你有很多下游，不想全跑到爆炸：

1. 先跑 `zlib` 自身矩阵（你定义的默认 options + 目标 profiles）。
2. 产物先放 `packages`（不直接进 `develop`）。
3. 选定“关键产品集”（例如你真实交付的 top-level app/lib，不是全仓所有包）。
4. 对每个产品 x 配置计算 `build-order`，再 `build-order-merge --reduce`。
5. 按 build-order 层级并行重建必要消费者（二进制缺失/失配的那些），并跑产品级测试。
6. 全通过后再 promote 到 `develop`。

效果：

- 不是“全量所有依赖全跑”；
- 也不是“只测 zlib 本包”；
- 而是“只重建对你交付产品有影响的必要路径”。

---

## 7. 关于 pairwise 可行性：基于证据的结论

官方文档没有提供 pairwise 作为推荐主策略。  
从公开机制看（此处为基于文档的工程推断）：

- `package_id` 由 settings/options/依赖信息共同决定；
- 依赖版本/修订变化会改变消费者二进制判定；
- 依赖关系与是否嵌入（embed/non-embed）也会影响是否必须重建；
- 逆向依赖在分布式仓库里本就难以完整建模。

所以 pairwise 在 Conan 这类 C/C++ 二进制图里，很难单独承担“正确性保证”的角色。  
Conan 官方路线更偏向：binary model + build-order + lockfile + 产品级集成验证。

---

## 8. 给 LLAR 的直接启发（贴近你当前 default-options + require 矩阵）

如果 LLAR 当前是“default options + require 矩阵”，建议借鉴 Conan 的不是“全量跑”，而是：

1. 保留包级矩阵（default options + 必要 profiles）作为第一层。
2. 增加“产品集”概念（只对关键交付物做集成守门）。
3. 增加 build-order 计算与层级并行执行，避免全图重建。
4. 增加 lockfile/快照机制，保证多机并行依赖一致。
5. 增加 staged promote（`packages -> products -> develop`），避免坏包直达主仓。

这套组合比“全量穷举”现实，也比“纯增量本包测试”更不容易把错误转嫁给用户。

---

## 9. 参考来源（均为官方/一手）

### ConanCenter Index（CCI）

- [Supported platforms and configurations](https://raw.githubusercontent.com/conan-io/conan-center-index/master/docs/supported_platforms_and_configurations.md)
- [Conanfile attributes（CCI policy）](https://raw.githubusercontent.com/conan-io/conan-center-index/master/docs/adding_packages/conanfile_attributes.md)
- [CCI FAQs](https://raw.githubusercontent.com/conan-io/conan-center-index/master/docs/faqs.md)

### Conan 官方命令与模型

- [`conan create`](https://docs.conan.io/2/reference/commands/create.html)
- [`conan test`](https://docs.conan.io/2/reference/commands/test.html)
- [`conan graph build-order`](https://docs.conan.io/2/reference/commands/graph/build_order.html)
- [`conan graph build-order-merge`](https://docs.conan.io/2/reference/commands/graph/build_order_merge.html)
- [`conan graph explain`](https://docs.conan.io/2/reference/commands/graph/explain.html)
- [Binary model: package_id](https://docs.conan.io/2/reference/binary_model/package_id.html)
- [Binary model: dependencies effect](https://docs.conan.io/2/reference/binary_model/dependencies.html)
- [global.conf（含 `tools.build:skip_test`）](https://docs.conan.io/2/reference/config_files/global_conf.html)

### Conan 官方 CI 教程

- [CI tutorial overview](https://docs.conan.io/2/ci_tutorial.html)
- [Packages pipeline](https://docs.conan.io/2/ci_tutorial/packages_pipeline.html)
- [Package pipeline: multi configuration](https://docs.conan.io/2/ci_tutorial/packages_pipeline/multi_configuration.html)
- [Package pipeline: multi configuration using lockfiles](https://docs.conan.io/2/ci_tutorial/packages_pipeline/multi_configuration_lockfile.html)
- [Products pipeline](https://docs.conan.io/2/ci_tutorial/products_pipeline.html)
- [Products pipeline: build-order](https://docs.conan.io/2/ci_tutorial/products_pipeline/build_order.html)
- [Products pipeline: multi-product multi-configuration](https://docs.conan.io/2/ci_tutorial/products_pipeline/multi_product.html)
- [Products pipeline: distributed full pipeline with lockfiles](https://docs.conan.io/2/ci_tutorial/products_pipeline/full_pipeline.html)

### 官方测试示例

- [examples2: test_package/conanfile.py](https://raw.githubusercontent.com/conan-io/examples2/master/tutorial/creating_packages/testing_packages/test_package/conanfile.py)
- [examples2: test_package/CMakeLists.txt](https://raw.githubusercontent.com/conan-io/examples2/master/tutorial/creating_packages/testing_packages/test_package/CMakeLists.txt)

