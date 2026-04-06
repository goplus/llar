# LLAR 矩阵降维优化方案：基于稳定路径的确定性差分

## 1. 现有方案的局限性：时间序列与锚点对齐的脆弱性

在现有的矩阵降维设计中（参考 `doc/llar-matrix-reduction-design.md` 6.2节），系统判断两个 Option 是否正交的核心方法是：在 Action Graph 中寻找 Baseline 与 Probe 完全相同的动作节点作为“精确锚点”，然后在锚点之间的 Gap 中分析差异。

这种基于“动作执行时间序列”的对齐机制存在两个致命的脆弱点：

1. **并发乱序 (Concurrency Jitter)**：在使用 `make -j` 或 `ninja` 等并发构建工具时，节点被记录到 trace 的顺序是随机的。物理上完全相同的两个图，因为 trace 记录顺序不同，可能导致 Gap 计算逻辑极其复杂甚至错乱。
2. **全局参数注入导致锚点全军覆没**：如果 Option A 的作用是在 CFLAGS 中增加一个 `-O3` 或者 `-g`，这会导致构建图中的**每一个编译节点**的 fingerprint 都发生变化。此时系统将找不到任何“完全一致”的锚点，触发“歧义必须保守回退”的安全机制，导致原本可以跳过的矩阵被强制真实执行。

**结论**：在黑盒观测中，动作的发生顺序（时间）和全局指纹是极度不稳定的，不适合作为对比差分图的坐标系。

---

## 2. 核心思想：以“稳定产物路径”为绝对坐标系

什么是绝对稳定的？在同一个代码库的构建中，**物理文件的路径（如 `main.o`, `lib/utils.a`）是绝对的契约。**

我们不需要强行去推导“Option A 里的第 3 个动作对应 Baseline 里的哪个动作”，我们只需要问一个确定性的问题：
> **“生成 `main.o` 的那个动作，相对 Baseline 发生改变了吗？”**

本方案将对比视角从 **“序列对比”** 翻转为 **“状态对比”**：
把 Action Graph 转化为一张哈希表：`Map[产物路径] -> 生成该产物的动作指纹 (Action Fingerprint)`。

---

## 3. 算法重构步骤

### 3.1 步骤一：Tooling 降噪（复用现有机制）
保留现有的 `evaluator.go` 中的降噪能力。通过过滤 `bash`, `make`, `cmake` 等纯调度工具，提取出真正产生物理读写和数据流传播的 Mainline Actions。

### 3.2 步骤二：构建产物生成表 (Path-to-Action Map)
对 Baseline (`O0`)、Option A (`OA`) 和 Option B (`OB`) 的 Mainline 节点进行遍历，以文件的写入路径（Writes）作为 Key，动作指纹作为 Value。

```text
Map_O0 = {
    "build/main.o": "gcc -c main.c",
    "build/utils.o": "gcc -c utils.c"
}

Map_OA = {
    "build/main.o": "gcc -c main.c -O3",
    "build/utils.o": "gcc -c utils.c -O3"
}

Map_OB = {
    "build/main.o": "gcc -c main.c",
    "build/utils.o": "gcc -c utils.c",
    "build/docs.pdf": "pandoc ..."
}
```
*注：如果一个节点产出多个文件，则这多个路径都映射到同一个动作指纹。如果文件是被一系列宏动作（比如中间生成了临时文件 `/tmp/1.s`）生成，则将瞬态路径隐藏，只保留最终持久化写入的工作空间路径。*

### 3.3 步骤三：确定性求取差分 ($\Delta$)
对比 Baseline 的 Map 和 Option 的 Map，不需要任何编辑距离（GED）或启发式猜想。直接通过 Map 遍历求差异：

定义 **$\Delta(Option)$ = Option 相对 Baseline，改变了哪些稳定路径的生成逻辑**（包括新增、删除或指纹修改）。

在上述例子中：
- `OA` 修改了 `main.o` 和 `utils.o` 的生成命令，因此：`Δ(A) = { "build/main.o", "build/utils.o" }`
- `OB` 新增了 `docs.pdf` 的生成，原有的 `main.o` 和 `utils.o` 命令指纹和 O0 完全一致被对消。因此：`Δ(B) = { "build/docs.pdf" }`

此时，我们获得了两个极度干净、100% 确定且不受乱序影响的差分集合 $\Delta(A)$ 和 $\Delta(B)$。

### 3.4 步骤四：双重碰撞判定

在拿到了 $\Delta(A)$ 和 $\Delta(B)$ 这两个“被篡改生成逻辑的文件集合”后，正交性判断化简为纯粹的集合论运算：

#### 1. 逻辑环境碰撞（写-写冲突）
```text
Collide_Logic = (Δ(A) ∩ Δ(B)) ≠ ∅
```
如果 A 和 B 都试图改变同一个文件（如 `main.o`）的生成逻辑（不管它们是怎么改的），立即判定逻辑碰撞。

#### 2. 物理传播碰撞（读-写冲突）
为了防止 A 的产物变成了 B 的输入，需要检查物理传播链路：
假设 $R_A$ 为 Option A 中所有 Mainline 节点的读路径集合，$R_B$ 同理。
```text
Collide_Physic = (Δ(A) ∩ R_B) ≠ ∅  OR  (Δ(B) ∩ R_A) ≠ ∅
```
如果 A 篡改了某个文件的生成逻辑，而 B 恰好读取了这个文件，则发生物理串联碰撞。

**放行条件**：
只要 `Collide_Logic` 和 `Collide_Physic` 皆为 false，则在构建图层面上，A 与 B 被**数学意义上 100% 证明为正交**，可以安全进入下一阶段（产物三方 Merge）。

---

## 4. 架构收益总结

1. **绝对确定性 (Zero Heuristics)**：彻底消灭了图匹配中的启发式“猜想”和“模糊归属”。路径不会骗人，哈希表寻址是 $O(1)$ 且绝对确定的。这完美契合了系统“保守证据链”的最高准则。
2. **完美免疫乱序 (Immune to Concurrency)**：以空间（文件路径）代替时间（节点出现顺序），从根本上无视了并发构建带来的 Trace 时序抖动。
3. **更强的降维放行率 (Higher Skip Rate)**：面对全局参数注入（如添加全局 `-g`），旧方案会因为丢失锚点而全部降级执行；新方案只会精准标出被 `-g` 影响的 `.o` 文件，只要 Option B 没有碰到这些 `.o`，依然可以完美发放正交认证，大幅提升测试矩阵的剪枝率。
