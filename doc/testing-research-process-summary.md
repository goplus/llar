# LLAR 测试系统调研过程总结

调研日期：2026-03-06
目的：总结本轮围绕“大规模构建矩阵测试”开展的调研路径、关键结论，以及 LLAR 设计是如何逐步收敛到当前形态的。

---

## 1. 调研起点

本轮调研的起点，不是一般意义上的“怎么写测试”，而是一个更具体的问题：

- LLAR 当前面对的是 `default options + require` 带来的大规模构建矩阵。
- 这个矩阵既不能全量跑完，也不能简单依赖随机抽样。
- 我们不希望把“配方错误”最终转嫁给用户侧运行时才暴露。

因此，调研目标被明确为：

1. 现有主流包管理器到底如何处理大规模矩阵。
2. 它们是全量测试、增量测试，还是分层测试。
3. 是否存在可直接借鉴的 pairwise 或其他大规模缩减方案。
4. LLAR 在“黑盒、多语言、云端”约束下，能够采用什么更现实的设计。

---

## 2. 第一阶段：外部生态事实调研

### 2.1 Nix / NixOS / Hydra / ofborg

我们先完整调研了 Nix 的测试系统和测试平台，重点不是单个命令，而是整条链路：

- PR 阶段主要依赖 ofborg 做增量评估和有限构建。
- 主线和发布依赖 Hydra 做持续评估、调度、构建和产物发布。
- 测试范围不是“全组合穷举”，而是通过 `supportedSystems`、`hydraPlatforms`、`runTestOn` 等机制控制。
- NixOS 测试系统本质上是“控制面和执行面分离”的平台化设计。

这一步的核心结论是：

- Nix 没有尝试把所有组合跑完。
- 它解决问题的方法是“预定义关键集合 + 平台门控 + 分层执行平台”。

更具体的业界例子包括：

- 在 Nixpkgs 的 PR 流程中，维护者可以通过 `@ofborg eval`、`@ofborg build ...`、`@ofborg test ...` 触发对应的评估、构建和测试流程。ofborg 只会在允许的机器和允许的目标上执行，而不是对所有平台做无差别全跑。
- Nixpkgs 中的包可以通过 `meta.platforms` 声明逻辑支持的平台，也可以通过 `meta.hydraPlatforms = [ ]` 等方式控制 Hydra 是否为其产出官方二进制。这意味着“逻辑支持”和“官方承诺构建交付”是两层不同语义。

这一类做法的本质是：

- 先缩小系统承诺的矩阵。
- 对没有被纳入承诺范围的组合，不给出官方构建或官方验证承诺。

### 2.2 Conan / ConanCenter

随后调研了 Conan，重点补齐了两类问题：

- 复杂构建矩阵到底怎么跑。
- 测试例子怎么写。

调研结论包括：

- ConanCenter 使用“固定 profile 列表 + `package_id` 去重”来构建二进制，不做 options 全组合穷举。
- ConanCenter 明确不负责跑上游完整 testsuite，而是侧重二进制构建与消费者验证。
- 面对多产品、多配置场景，Conan 通过 `build-order` 和 `build-order-merge` 降低重复构建。

这一步的核心结论是：

- Conan 也没有采用 pairwise 作为主策略。
- 它依赖的是“固定矩阵 + 去重 + 受影响产品重建 + lockfile 一致性”。

更具体的业界例子包括：

- Conan 官方文档在 `libpng` 的例子中，用 `conan graph build-order --requires=libpng/1.5.30 --order-by=recipe` 展示了依赖重建顺序。结果会先列出 `zlib`，再列出 `libpng`；如果 `zlib` 的二进制已经存在于 cache 中，则不会重复重建。
- Conan 的 header-only 教程中，`sum/0.1` 通过 `package_id()` 里的 `self.info.clear()` 明确声明 Debug、C++ 标准等维度不影响二进制身份，因此这些维度不会生成新的 package id。

这一类做法的本质是：

- 系统不去测所有组合。
- 而是由 recipe 作者明确声明“哪些维度真正影响二进制身份”，并据此做构建与缓存去重。

### 2.3 Bazel

虽然 Bazel 不是包管理器，但它的测试系统值得纳入调研，因为它展示了另一种成熟的“测试组织方式”：

- Bazel 把测试定义为一等目标，使用 `bazel test` 统一执行。
- 测试可以通过 `test_suite` 聚合，而不是按目录或脚本零散触发。
- 测试规则有明确的标签与约束系统，例如 `small`、`medium`、`large`、`smoke`、`manual`、`exclusive`。
- 测试动作的执行平台由 test toolchain 和 execution platform 共同决定，而不是简单继承构建平台。

更具体的例子包括：

- Bazel 的 `test_suite` 可以显式列出要跑的测试，也可以按标签筛选测试；例如可以只组织 `smoke_tests`，或通过 `-flaky` 排除不稳定测试。
- Bazel 官方文档明确把 `smoke` 解释为“应在提交代码前运行的测试”，把 `manual` 解释为“不自动包含进通配符测试”，把 `exclusive` 解释为“运行时不与其他测试并发”。
- Bazel 的 test encyclopedia 还明确规定了 test action 的执行平台由测试工具链和平台约束共同决定，避免例如“在 Windows 上执行 Linux 测试二进制”这类错误配置。

这一步的核心结论是：

- Bazel 解决的重点不是“如何缩减包管理器的 options 矩阵”，而是“如何把测试目标、测试集合、测试资源约束和执行平台一起纳入统一图模型”。
- 它更像是一个“测试组织系统”，而不是一个“矩阵缩减系统”。

### 2.4 Homebrew

Homebrew 这一条线的价值在于观察“社区包仓库如何把 PR 测试、构建和合并流程绑定在一起”。

调研结论包括：

- Homebrew 的 BrewTestBot 就是其自动化 review 与测试系统。
- 它在固定的 macOS / Linux 机器池上执行 bottle 构建和自动化检查，并把结果直接反馈到 PR。
- 对 formula 贡献者来说，官方要求的本地验证流程也很明确：先 `brew install --build-from-source`，再 `brew test`，再 `brew audit`。
- 对 maintainer 来说，CI 是否通过直接决定 PR 是否能被 BrewTestBot 自动合并或进入手动发布流程。

更具体的例子包括：

- BrewTestBot 文档明确写到，`brew test-bot` 负责对 Homebrew 或其 taps 的变更执行 bottle builds 和自动测试，并自动更新 PR 状态。
- Homebrew 的 pull request 文档明确要求，formula 变更在提交前要运行 `brew install --build-from-source <CHANGED_FORMULA>`、`brew test <CHANGED_FORMULA>` 和 `brew audit --strict --online <CHANGED_FORMULA>`。
- BrewTestBot for Maintainers 文档则明确说明：只有在 CI 通过且 PR 获得 maintainer 审批后，BrewTestBot 才会自动合并；否则需要进入更保守的人工发布流程。

这一步的核心结论是：

- Homebrew 的策略不是缩减所有理论组合，而是围绕“固定支持平台 + PR 生命周期 + bottle 构建 + formula smoke test”建立一套强约束流程。
- 它的测试系统更像“仓库准入与交付流水线”，而不是“通用组合矩阵缩减算法”。

### 2.5 Debian / autopkgtest / debci / britney

Debian 这一条线真正相关的不是 reproducible builds，而是 `autopkgtest` 及其与迁移系统的联动。

调研结论包括：

- Debian 包可以声明 `autopkgtest` 测试。
- 这些测试运行在 Debian CI 基础设施上，由 `debci` 提供执行框架。
- `britney` 会在包从 unstable 迁移到 testing 的过程中触发相关测试，并使用结果影响迁移决策。
- 但 Debian 并不会因此对所有受影响包做“全库全矩阵重测”；对于库迁移之类的场景，它只运行由该库包所触发的相关测试，而不是所有可能依赖该库的新版本包的 autopkgtest。

更具体的例子包括：

- Debian Wiki 明确写到，维护者可以为包添加 autopkgtest，这些测试会在 `ci.debian.net` 上运行。
- 同一份说明还明确指出，`britney` 会调用 `debci` 的 API 来测试 migration candidate，并用结果影响 unstable 向 testing 的迁移。
- 文档也明确举例说明：对于 library transition，Debian 只运行由该库触发的测试，而不会对所有用到新库版本构建出来的包重新跑一遍 autopkgtest。

这一步的核心结论是：

- Debian 的策略不是全矩阵，而是“包声明测试 + CI 平台执行 + 迁移系统按候选触发相关测试”。
- 它很强调“测试结果服务于发行迁移决策”，而不是把所有组合都视为同等优先级。

---

## 3. 第二阶段：对主流做横向归纳

在 Nix、Conan、Bazel、Homebrew 和 Debian 这几条线都跑通之后，我们得到了一个比较稳定的外部事实：

1. 没有看到主流包管理器把 pairwise 当作主发布门禁。
2. 没有看到任何生态尝试对 options 做全量组合穷举。
3. 主流系统更常用的是：
   - 固定平台矩阵；
   - 固定测试入口与测试骨架；
   - 测试集合分层；
   - 标签、白名单或显式约束；
   - 增量触发；
   - 关键产品或关键测试集合；
   - 构建/测试分层；
   - 二进制去重；
   - 锁依赖版本的一致性机制。

这一步把问题界定清楚了：

- “为什么别人不做 pairwise”不是偶然，因为它不具备工程上的确定性，很难承担发布正确性的主责任。
- “为什么别人不全跑”也不是偷懒，而是因为大规模矩阵在工程上本来就不可全量覆盖。

如果把这些例子进一步压缩成一句话，可以得到一个更清楚的模式：

- Nix：先缩小平台与测试承诺范围。
- Conan：用 binary identity 去重，只为真正不同的二进制身份付费。
- Bazel：把测试本身组织成图中的一等目标，并用 test suite、标签和平台约束来控制执行。
- Homebrew：把测试嵌进 PR 准入和 bottle 交付流程。
- Debian：把测试结果接入迁移决策，只对相关候选和相关依赖触发测试。

这也帮助我们明确了一个边界：

- 业界有很多“缩范围、做缓存、做 provenance”的成熟做法。
- 但没有现成方案能在黑盒、跨语言、拒绝抽样、且要求对未物理测试组合给出绝对放行证明的前提下，直接解决 LLAR 的问题。

---

## 4. 第三阶段：研究路线补充调研

在调研主流工程实践之外，我们也补充看了一些常见研究路线，目的是明确哪些思路值得借鉴，哪些不适合作为 LLAR 的主线。

### 4.1 Pairwise / covering arrays

这条路线的典型论点是：

- 许多缺陷往往由少量参数交互触发。
- 因此用 pairwise 或更高阶的 t-wise 覆盖，就可以用很少测试覆盖大量交互情形。

典型例子包括：

- NIST 的材料会用类似 `if (A && B)` 的布尔分支说明：很多错误由少数维度交互触发，因此组合覆盖能以很小代价获得很高的缺陷发现率。
- 但 NIST 自己也明确提到，真实故障的触发交互强度可能达到 6，pairwise 并不能保证覆盖所有关键故障。

因此这条路线适合：

- 高性价比找 bug。

但它不适合：

- 对未测试组合给出绝对放行证明。

### 4.2 Family-based / product-line model checking

这条路线的核心思路是：

- 先建立正式的 feature model 和行为模型。
- 再把整个产品族作为一个统一模型进行 SAT / IC3 / IMC 等验证，而不是逐个配置跑。

典型例子是：

- 研究会把一个 feature family 编译成单个 SMV 模型，然后对整个产品族一次性做模型检测。

这条路线很强，但它的前提是：

- 你必须拥有正式 feature model。
- 你必须拥有行为模型或可供验证的语义对象。

对于只调度 shell 构建、且不理解语言语义的 LLAR 来说，这一前提并不成立。

### 4.3 Variational execution

这条路线的核心思路是：

- 在同一次执行里共享不同配置的公共路径，减少重复执行成本。

典型例子是：

- OOPSLA 2018 的 variational execution 研究通过改写 JVM bytecode，在 7 个高可配置系统上实现了 2 到 46 倍的提速。

但它的前提是：

- 需要深入具体语言或 runtime 的执行模型。
- 通常需要字节码或解释器层面的改造。

因此它不符合 LLAR 对语言无关、黑盒调度器角色的要求。

---

## 5. 第四阶段：把问题拉回 LLAR 真实约束

在调研中，一个重要的转折点是，我们逐步把问题从“外部生态怎么做”拉回到了 LLAR 自己的约束上：

- LLAR 是多语言包管理器，不适合把某一门语言的 ABI 规则（如 C/C++ 的 DWARF 分析）变成平台基础设施。
- LLAR 是黑盒调度系统，不应该要求配方作者理解复杂的分析模型。
- LLAR 采用全云端构建，需要一种在 CI 阶段就能产生普适认证模型的方法，而不是依赖本地验证。

也正是在这个阶段，我们逐步确认：单纯谈“增量触发”不够，因为这并未解决 options 爆炸带来的隐性耦合风险（例如 C 语言中的结构体布局联动陷阱）。LLAR 需要的是一套更贴近“黑盒矩阵缩减”的方案。

---

## 6. 第五阶段：探索 LLAR 自己的矩阵缩减思路

在这个阶段里，我们从“物理足迹”、“正交分析”、“ABI 安全网”等思路开始，在遭遇多个极端工程反例后，逐步推演出一套成熟的黑盒自动化缩减模型。

### 6.1 初始思路：文件足迹与正交推导（被否决）

初始想法非常直观：
- 对每个 option 做单变量 probe（探测构建）；
- 观察其读取路径、写入路径的变化；
- 如果两个 option 修改的源码文件完全没有交集，就判定为正交，从而将 option 划分成若干“独立岛屿”。

发现的致命漏洞（The `struct S` Padding 陷阱）：
这种粗粒度的文件级观测无法防范隐性 ABI 破坏。典型反例是“结构体布局联动”：如果 A 和 B 都修改了同一个头文件中的结构体 `struct S` 的不同条件编译分支，单看增量，它们似乎各自平行增加了不同字段。但如果在 A+B 组合下，C 语言的内存对齐（Padding）规则会导致整个结构体的大小和内存偏移量发生非线性突变（`Size(A+B) != Size(A) + Size(B)`）。如果仅仅因为它们修改了不同字段就误判为“正交安全”而不去跑 A+B 的组合测试，将导致极其严重的运行时崩溃。

### 6.2 修正思路：基于语言 ABI 的底层语义分析（被否决）

为了解决上述布局联动问题，方案一度转向使用 `abidiff` 等工具，通过直接对比二进制的 DWARF 调试信息来分析内存布局的物理变化。

发现的工程阻碍：
这种方案虽然精准，但深度绑定了 C/C++ 语言环境。LLAR 的核心定位是“语言无关的通用黑盒调度器”，引入 `abidiff` 违背了这一初衷，它无法处理 Python 的动态扩展、Go 模块或纯二进制包的分发冲突。

### 6.3 进阶思路：构建动作图碰撞 (Action Graph Collision)

在“保持黑盒”与“确保绝对安全”的双重挤压下，我们借鉴了 Google Bazel 的核心思想，将视角从“理解代码语义”转向**“观测构建流水线的物理行为”**。
- 将构建过程拆解为原子动作集合（如单条 `gcc` 编译命令）。
- 关注每个 option 改变了哪些原子动作的“输入指纹”（包括命令行参数、环境变量、输入文件哈希）。
- 解决 `struct S` 陷阱：在 `struct S` 案例中，尽管 A 和 B 增加的是不同字段，但它们都会试图修改“编译该头文件所在的 `.c` 文件”这一原子动作的输入参数（如传入了不同的宏 `-DA` 和 `-DB`）。系统在动作层面敏锐地捕捉到了交叠，立即判定发生**“动作碰撞”**，强制进行组合测试。

### 6.4 终极收敛：动作图路线暴露出的两大工程陷阱

在将“动作图”应用于实际 C 语言项目（如 libcurl）时，我们又遭遇了传统静态分析难以跨越的两个鸿沟。这一步的意义，不是这些问题已经被彻底攻克，而是我们明确识别出了这条路线最容易失效的边界：

难题 A：“合并类动作”导致的构建漏斗陷阱 (The Linker Funnel)
- 现象：无论前期的编译动作多么正交，所有产生的 `.o` 文件最终都会汇聚到同一个链接器动作（如 `ld -o lib.so a.o b.o`）中。如果只要触碰同一个动作就算碰撞，所有选项都会因为最后的 `ld` 连在一起，大矩阵降维彻底失败。
- 讨论结论：如果后续仍沿动作图方向深入，这里就必须区分**“内容变换动作（如编译）”**和**“仅合并动作（如链接、打包归档）”**。否则所有选项都会因为最终链接步骤而被误并为一个大碰撞岛。这个问题在讨论中被识别出来，但当前实现还没有把它作为独立规则完全解决。

难题 B：“虚假碰撞”带来的降维失效 (The `config.h` Problem)
- 现象：在 C/C++ 中，几乎所有的选项都会向一个全局的 `config.h` 文件中写入宏定义（如 `#define HAVE_ZLIB`）。由于这个文件被所有编译动作读取，这会导致整个动作图的输入指纹全面变化，系统会误判所有选项都发生了严重的“干涉”。
- 讨论结论：单纯依赖共享输入路径会导致严重的保守过度。讨论中曾经设想过通过更强的产物级证明来识别“伪碰撞”，但这仍然属于后续可能继续探索的方向，并不是当前已经落地的能力。

这一步真正稳定下来的，不是“所有难题都已经解决”，而是：我们确认了动作图路线依然是最合理的主方向，同时也明确了它在工程上最需要谨慎对待的两个边界问题。

### 6.5 Linux 实证：libarchive 真实 CMake 项目

在设计讨论基本收敛后，我们又在 Linux 机器上用真实 CMake 项目做了一轮实证，目的是验证：

- 当前实现到底能把真实矩阵从多少缩到多少。
- “工具类 option 可能可跳过”的判断，是否能在真实项目里观察到。

实验对象选择了 `libarchive/libarchive@v3.8.2`，因为它同时包含两类 option：

- 更像附加工具的选项：`ENABLE_TAR`、`ENABLE_CPIO`、`ENABLE_CAT`
- 更像核心能力的选项：`ENABLE_ACL`、`ENABLE_ZLIB`、`ENABLE_ZSTD`

实验方法保持与当前实现一致：

- 用真实 `llar make --matrix ...` 执行构建。
- 用 Linux 下的 `strace` 产生 `trace.Record`。
- evaluator 仍然按 `baseline + 单变量 probe + diff surface + collision components` 的当前逻辑运行。
- baseline 设为六个选项全关，以便最大化观察“纯新增、独立输出”的可能性。

矩阵规模与结果：

- 总组合数：`64`
- baseline：`arm64-linux|acl-off-cat-off-cpio-off-tar-off-zlib-off-zstd-off`
- 当前 evaluator 返回的必测组合数：`64`
- 缩减比例：`0%`

碰撞结构非常激进：

- 6 个 option 两两全部碰撞，共 `15/15` 对。
- 最终只形成了一个连通分量：
  - `{acl, cat, cpio, tar, zlib, zstd}`

进一步看 surface 的来源，可以看到当前实现为什么会完全保守：

- 所有 probe 的公共交集里包含大量 `/tmp/$TMP/.git/objects/*`
  - 这说明当前 trace 把源码同步过程也计入了观察面。
- 即使去掉 `.git` 与系统库噪音，pairwise overlap 仍然存在：
  - `CMakeTmp`
  - `.ninja_deps`
  - `.ninja_log`
  - `_build/libarchive/CMakeFiles/archive*.o.d`
- 也就是说，除了 source sync 之外，CMake configure 与共享 `libarchive` 构建路径本身也足以把选项重新汇成一个大碰撞岛。

但如果把视角从 trace 切换到**最终安装产物**，可以看到一个更细的事实：

- `ENABLE_CAT`
  - 只新增 `bin/bsdcat` 和 `share/man/man1/bsdcat.1`
  - `libarchive.a`、`libarchive.so*` 哈希与 baseline 完全一致
- `ENABLE_CPIO`
  - 只新增 `bin/bsdcpio` 和 `share/man/man1/bsdcpio.1`
  - `libarchive.a`、`libarchive.so*` 哈希与 baseline 完全一致
- `ENABLE_TAR`
  - 只新增 `bin/bsdtar` 和 `share/man/man1/bsdtar.1`
  - `libarchive.a`、`libarchive.so*` 哈希与 baseline 完全一致

这三项非常接近我们讨论中所说的“purely additive, isolated outputs”。

与之相对：

- `ENABLE_ACL`
  - 没有新增独立交付文件
  - `libarchive.a`、`libarchive.so*` 全部变化
  - `libarchive.pc` 新增 `-lacl`
- `ENABLE_ZLIB`
  - `libarchive.a`、`libarchive.so*` 全部变化
  - `libarchive.pc` 新增 `-lz`
- `ENABLE_ZSTD`
  - `libarchive.a`、`libarchive.so*` 全部变化
  - `libarchive.pc` 新增 `-lzstd`

这说明：

- 从**真实交付物语义**看，工具类 option 与核心 feature option 的行为确实不同。
- 但从**当前 trace + simplified action graph** 看，它们仍会因为 source sync、configure 噪音和共享构建路径被全部并岛。

这轮实证把一个关键边界彻底坐实了：

- “实际存在可跳过测试的 option” 与 “当前 evaluator 能识别出来的 option” 不是一回事。
- 当前实现的保守程度，在真实 CMake 项目上足以把本应更像附加物的工具类选项也吞进大碰撞岛。

为了避免把上述判断建立在“读完整 report 后的主观解释”上，我们又补了一轮**定向正式测试**，只观察 `baseline` 与 `cat-on`：

- baseline：`arm64-linux|acl-off-cat-off-cpio-off-tar-off-zlib-off-zstd-off`
- probe：`arm64-linux|acl-off-cat-on-cpio-off-tar-off-zlib-off-zstd-off`

这轮定向测试里直接观察到：

- `baseline_actions = 9536`
- `probe_actions = 9407`
- `seed_count = 7578`

也就是说，`cat-on` 相对 baseline 的差异并不是在最后生成 `bsdcat` 时才出现，而是在极早阶段就已经扩散开。

前几个 seed action 具体包括：

- 顶层 `evaluator.test make --matrix ...`
- `git index-pack`
- `ninja --version`
- `ld --help`
- `CMakeScratch` 目录中的 `ninja -t recompact`
- `CMakeScratch` 目录中的 `ninja -t restat`
- `ninja cmTC_*`
- `as ...`

这说明在正式测试中，`cat-on` 的 diff surface 很早就已经吸收了：

- source sync 行为
- CMake try-compile 行为
- Ninja bookkeeping 行为

我们还做了另一轮更窄的正式测试，试图抓出“第一个未匹配的 `cmake -S` configure action”，结果是：

- 没有观察到未匹配的 `cmake -S` action

这意味着，至少在这轮正式测试里，不能把“未缩减成功”简单归因到某一条独立的 configure 命令本身。

同时，我们把 `cat` 与其他 probe 的 overlap 里最显眼的 `.git/objects/*` 去掉后，仍然观察到大量共享路径：

- `cat` vs `cpio`：仍有 `117` 个共享路径
- `cat` vs `tar`：仍有 `78` 个共享路径
- `cat` vs `zlib`：仍有 `114` 个共享路径

这些共享路径里，正式测试明确出现了：

- `_build/.ninja_deps`
- `_build/.ninja_log`
- `_build/CMakeFiles/CMakeTmp/*`
- `_build/build.ninja`
- `_build/config.h`
- `_build/libarchive/CMakeFiles/archive*.o.d`

因此，当前能被正式测试支持的表述是：

- `.git/objects/*` 确实是碰撞来源之一；
- 但即使去掉这一层，共享 build graph 路径仍然足以让 `cat` 与其他 option 保持碰撞；
- 当前还不能把问题收缩成单一的某条 configure 命令或单一的某个机制。

---

## 7. 当前收敛到的判断

本轮调研最终形成了以下较稳定的共识：

1. LLAR 不应追求“全量矩阵跑完”。
2. LLAR 也不应把 pairwise 当作正确性来源。
3. LLAR 更适合采用“黑盒 probe + 动作图碰撞分析 + 岛屿化缩减”的自动模式。
4. `onTest` 应继续作为统一的产物验证入口。
5. 在 llarhub 的 CI 阶段，系统职责不是跑完所有组合，而是通过动作图自动找出“必须真正测试”的碰撞组合，并为正交组合发放认证指纹。
6. 对无法证明正交的部分，系统必须保持保守，进行物理组合测试，而不是强行推导。

---

## 8. 这轮调研最大的产出

本轮调研最有价值的产出，不只是几份外部生态文档，而是把讨论从“泛泛而谈的测试策略”收敛到了 LLAR 自己的问题定义上。

具体来说，产出有三类：

1. 外部事实文档
   - Nix 全链路测试系统调研
   - Conan 测试系统深度调研

2. LLAR 内部设计文档
   - 基于动作图碰撞的测试系统全面设计方案
   - 当前实现状态说明

3. 一个更清楚的问题边界
   - 我们要解决的是“大规模矩阵如何在黑盒条件下保守缩减”，不是“证明所有组合永远安全”，也不是“依赖特定语言的 ABI 分析”。

---

## 9. 当前仍未闭合的问题

虽然设计已经明显收敛，但仍有几类问题没有完全闭合：

- 原子动作指纹的提取（Action Tracing）在跨平台环境下的工程落地细节。
- evaluator 目前还偏保守，分析结果需要更可审计的输出。
- 最终产物哈希叠加模型的严格界定，以及云端交付闭环还未完全落地。
- 对于极端耦合的包，如何避免“全岛爆炸”仍需进一步的工程化隔离策略。

这些问题已经不再是“方向不明确”的问题，而是“如何把当前方向做稳”的问题。

---

## 10. 总结

这轮调研的过程，本质上经历了多次认知收敛：

1. 先从外部生态确认：主流包管理器都不做全量穷举，也不把 pairwise 当主门禁。
2. 再从研究路线确认：pairwise、family-based、variational execution 各自有价值，但都不满足 LLAR 所需的工程确定性或黑盒边界。
3. 最后回到 LLAR 自身约束：多语言、黑盒、云端，使得通用 ABI 模型（如 `abidiff`）或源码文件比对不可行。
4. 最终收敛到当前方向：以黑盒可观察证据为基础，用单变量 probe、动作图指纹和碰撞岛屿来自动缩减必须执行的测试矩阵，并在 CI 阶段发放认证。

当前设计并不是从“理论最优”直接推出的，而是从一轮轮现实约束与极限反例（如隐式宏耦合）中逐步锤炼出来的结果，这使得它在工程上具备了极高的稳妥性与可落地性。
