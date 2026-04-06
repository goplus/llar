# LLAR Trace SSA 设计方案：面向汇合的路径版本化数据流模型

## 1. 目标

本文档描述一种用于重构 LLAR Stage 2 的 **SSA-inspired Trace 数据流模型**。

核心目标不是把构建图改造成编译器意义上的严格 SSA，而是把当前基于

- `seedWrites`
- `needPaths`
- `slicePaths`

的路径集合启发式，收敛成一个更正规的**路径版本化数据流分析**。

这套方案的核心抽象只有三个：

1. 每次写入形成一个新的**路径版本**
2. 下游读取消费某个路径版本
3. 多路数据流在图上发生**汇合（join）**

一旦这三个对象成立，Stage 2 就可以借用大量 SSA / 数据流分析思想：

- reaching definitions
- def-use / use-def chains
- forward / backward slicing
- join frontier
- RAW / WAW hazard analysis

---

## 2. 这不是“严格 SSA”

必须先明确边界：

LLAR 当前可观测到的是黑盒 trace 的**执行节点摘要**，而不是编译器 IR。

当前稳定输入主要是：

- `Argv`
- `Env`
- `Cwd`
- `Inputs`
- `Changes`

见 [trace.go](/Users/haolan/project/llar/internal/trace/trace.go)。

因此本文档定义的 Trace SSA：

- **不是** 编译器里的 strict SSA
- **不是** syscall 级别的精确版本链
- **不是** 基于程序 CFG/basic block/dominance frontier 的标准 phi 放置

它是一个更轻的模型：

> **在现有 action graph 之上，对 canonical path 做 action-level 的版本化与数据流分析。**

也就是说：

- 一个执行节点对某路径的**最终写入**形成一个新版本
- 一个执行节点对某路径的读取，绑定到一个保守可判定的上游版本
- 图上的“汇合”承担类似 phi/join 的分析角色，但不要求严格遵循经典 SSA 语法

---

## 3. 核心对象

### 3.1 执行节点 `E`

执行节点就是 trace 中的一条原始执行记录。

它是**不透明节点**，不依赖动作语义识别。

也就是说，这里不区分：

- compile
- configure
- link
- install

节点只携带观测属性：

- `argv`
- `cwd`
- `env`
- `inputs`
- `changes`

本文档后续所有分析都建立在“执行节点是不透明的”这个前提上。

### 3.2 路径版本 `V(path, n)`

对任意 canonical path `p`，每一次写入都会产生一个新版本：

- `p@0`：基线起始版本，表示构建开始前外部可见的输入状态
- `p@1`
- `p@2`
- ...

这里的版本递增单位是：

> **执行节点级最终写入**

不是 syscall 级写入。

### 3.3 汇合点 `J`

构建图不是程序 CFG，但它依然存在大量**数据流汇合**。

本文档统一把这类现象称为 **join / 汇合**。

有两类汇合：

1. **多输入汇合**
   - 一个执行节点同时消费多个上游版本
   - 例如多个输入共同产生一个新输出

2. **版本选择/竞争汇合**
   - 同一路径的多个候选版本在某个分析边界上汇合
   - 后续需要判断哪个版本支配 downstream use

这里不强行使用经典 phi 术语，但**join 在分析中承担与 phi/join 类似的角色**。

---

## 4. 基本数据流规则

### 4.1 定义（Def）

若执行节点 `E_i` 写入路径 `p`，则产生一个新的版本：

`Def(E_i, p) = p@i`

更准确地说，是 `p` 的下一个可用版本。

### 4.2 使用（Use）

若执行节点 `E_j` 读取路径 `p`，则它消费的是某个可达版本：

`Use(E_j, p) -> p@k`

这里的 `k` 不是编译器 SSA 那种绝对精确唯一值，而是：

- 能精确判定时，绑定到唯一版本
- 不能精确判定时，绑定到保守汇合态

### 4.3 传播

数据流传播不再是“路径集合 BFS”，而是：

`changed def -> reaching use -> downstream def`

也就是：

1. 某个变化定义版本出现
2. 找出消费该版本的 use
3. 找出这些 use 所在执行节点产生的新定义
4. 继续向下传播

---

## 5. Stage 2 在 Trace SSA 上的重新定义

### 5.1 `M(A)`：变更执行区

对于 option `A`，`M(A)` 是相对 baseline 发生变化的执行节点集合。

变化来源包括：

- 执行节点内部属性变化
  例如 `argv/cwd/env` 变化
- 新执行节点出现
- 关键输入版本变化

### 5.2 `SeedDef(A)`：变化定义

`M(A)` 产生的、相对 baseline 真正变化的路径版本集合。

它替代当前较粗的 `seedWrites` 概念。

### 5.3 `Need(A)`：外部前提版本

`M(A)` 及其传播闭包消费的、但并非由 `M(A)` 自己产生的那些版本。

它表达的是：

> 这条变化流要成立，依赖了哪些外部版本输入

这比当前“路径前提集合”更接近真正的数据流含义。

### 5.4 `Flow(A)`：传播闭包

从 `SeedDef(A)` 出发，沿 def-use 链传播所能到达的全部受影响版本。

它替代当前的 `slicePaths`。

### 5.5 `JoinFrontier(A)`：汇合前沿

`A` 的变化流第一次与以下对象汇合的位置：

- 外部未变化流
- 另一 option 的变化流
- 允许的 merge/replay surface

这个前沿非常重要，因为它决定：

- 哪里是无害汇合
- 哪里是危险汇合
- 哪里应当视为 Stage 2 的分析边界

---

## 6. 碰撞的重新定义

当前 Stage 2 的碰撞仍主要靠路径集合交叉：

- `seedWrites` overlap
- `slicePaths` vs `needPaths`
- `slicePaths` shared overlap

在 Trace SSA 下，碰撞应改写成更明确的数据流冒险：

### 6.1 读后写污染（RAW Hazard）

若 `A` 的变化版本会在不允许的汇合前，遮蔽 `B` 原本依赖的外部版本，则发生硬碰撞。

直观地说：

- `B` 原本要消费 `p@k`
- 但与 `A` 组合后，`B` 被迫看到 `p@m`
- 且 `p@m` 是由 `A` 的变化流引入的新版本

这就是 build-graph 上的 RAW hazard。

### 6.2 写写竞争（WAW Hazard）

如果 `A` 和 `B` 都试图为同一路径产生新的不兼容版本，且这种竞争无法在允许汇合面上被吸收，则发生硬碰撞。

### 6.3 允许的汇合

若两条变化流只在允许的 surface 上汇合，例如：

- direct merge surface
- root replay 可吸收的 replay-root 汇合

则不视为 Stage 2 硬碰撞。

---

## 7. 这套模型能借用哪些 SSA / 数据流算法

最值得借用的不是“SSA 语法”，而是这些分析算法：

### 7.1 Reaching Definitions

回答：

> 某个 use 实际看到的是哪个定义版本

这是整个模型最核心的能力。

### 7.2 Def-Use / Use-Def Chains

回答：

> 某个变化定义具体影响了哪些 use
> 哪些 use 又产生了哪些 downstream definitions

### 7.3 Forward / Backward Slice

回答：

> 一个变化定义会向前影响到哪里
> 一个碰撞点/输出点向后依赖了哪些外部版本

### 7.4 Join Frontier

回答：

> 某条变化流第一次与其他流汇合的边界在哪里

### 7.5 Hazard Analysis

回答：

> 两条变化流是否在不允许的汇合前发生 RAW / WAW 冲突

---

## 8. 为什么这比当前方案更强

当前 Stage 2 本质上已经在做半数据流分析，但问题是：

- 传播单位还是路径集合
- 汇合没有被显式建模
- `Need` 和 `FS` 的语义还不够统一
- 很多边界只能靠启发式修补

Trace SSA 的提升在于：

1. **路径不再是单一模糊节点，而是版本链**
2. **读取不再只是“读过这个路径”，而是读过这个路径的某个版本**
3. **波及区不再只是 BFS，而是 def-use 传播**
4. **碰撞不再只是路径重叠，而是危险汇合/版本遮蔽**

---

## 9. 这套模型不解决什么

边界必须写清楚：

1. 不解决 `A+B-only` 幽灵路径
2. 不解决 Stage 3 的 object/member merge
3. 不直接证明运行时语义正确性
4. 不替代 tooling / probe / mainline / delivery 的图角色分类
5. 不要求动作语义识别

也就是说：

> Trace SSA 只负责把 Stage 2 的结构干涉判定正规化为数据流分析问题。

---

## 10. 与当前实现的关系

这套模型不是推翻现有实现，而是为现有 Stage 2 提供一个更正规、可逐步迁移的解释框架。

当前代码中的：

- `seedWrites`
- `needPaths`
- `slicePaths`
- `classifyMutationRoots`
- `propagateForwardSlice`

都可以理解为这套数据流模型的粗粒度近似。

未来若推进 Trace SSA，优先方向不是重写整个 evaluator，而是：

1. 保留现有 action graph 与图角色分类
2. 在其上叠一层 path-version / def-use overlay
3. 用该 overlay 重写 Stage 2 的传播与碰撞判定

---

## 11. 最终定义

一句话总结：

> **LLAR Trace SSA 是一种面向汇合的路径版本化数据流模型。**
> 它把黑盒执行节点的路径写入版本化，把构建图重写为“定义版本、使用关系、汇合边界”组成的数据流图，并在这个图上做传播、前提与碰撞分析。

它不是编译器级 strict SSA，但足够让 Stage 2 从经验启发式，收敛成更系统的数据流分析框架。
