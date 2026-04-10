# LLAR TraceSSA + Evaluator 重构方案设计

## 1. 目的

这份文档定义的是 **LLAR Stage 2 的重构方案**。

目标不是修补当前 `internal/evaluator` 里的旧实现，而是给出一套新的、可以直接照着实现的设计：

- 只保留两个模块：
  - `tracessa`
  - `evaluator`
- 讲清楚每个模块负责什么
- 讲清楚每个模块内部具体怎么做
- 讲清楚从输入到输出的完整流程

这份文档的读者包括：

- 人类工程师
- 未来参与实现的 AI

因此文档必须满足两个要求：

1. 不依赖旧代码上下文也能读懂
2. 读完之后可以直接开始拆文件和实现

---

## 2. 设计目标

Stage 2 要解决的问题只有一个：

> **给定 baseline 和单项 option 的构建观测，提取这个 option 的结构影响摘要，用于后续组合正交判定。**

这里的“结构影响摘要”指的是：

- 直接起火点写了什么
- 这些变化向下波及到了哪里
- 这些变化依赖了哪些前提状态
- 当前证据是否足以给出 sound 结论

最终需要的输出是 `ImpactProfile`，供 `evaluator` 上层做碰撞判定。

---

## 3. 非目标

这次重构不负责：

- Stage 3 的产物合并
- Stage 4 的组合测试
- 源码语义分析
- ABI 推断
- 针对特定语言的编译语义识别

这些能力不属于 Stage 2。

---

## 4. 总体架构

重构后只保留两个模块：

```text
internal/trace/ssa    -> tracessa
internal/evaluator    -> evaluator
```

依赖方向固定为：

```text
evaluator -> tracessa
```

`tracessa` 不能依赖 `evaluator`。

---

## 5. 两个模块各自负责什么

## 5.1 `tracessa` 模块

`tracessa` 是 **纯分析引擎**。

它负责把 baseline/probe 的黑盒 trace 观测转换成 `ImpactProfile`。

它内部完成完整的五阶段流水线：

1. 观测归一化
2. Path-SSA 建图
3. 角色投影
4. Wavefront 差分
5. Impact 提取

`tracessa` 不负责：

- 组合矩阵决策
- 多个 option 两两比较
- 合并产物
- 测试执行

它的责任边界很简单：

> **输入一对 baseline/probe 观测，输出一个单项 option 的 `ImpactProfile`。**

## 5.2 `evaluator` 模块

`evaluator` 是 **Stage 2 的编排器和判定器**。

它负责：

- 从 `ProbeResult` 组装出 `tracessa` 需要的输入
- 调用 `tracessa` 得到每个 option 的 `ImpactProfile`
- 用两个 `ImpactProfile` 做碰撞判定
- 把 Stage 2 结果交给上层

`evaluator` 不再负责：

- 自己维护一套独立的路径图
- 自己做角色分类
- 自己做节点配对式差分
- 自己重新定义 `Need / SeedW / FS`

`evaluator` 的原则是：

> **只消费 `tracessa` 的结果，不重复实现 `tracessa` 的内部逻辑。**

---

## 6. `tracessa` 模块设计

## 6.1 `tracessa` 的输入

`tracessa` 处理的是一个 baseline/probe 对。

建议定义输入对象：

```text
AnalysisInput
  Base:
    Records
    Events
    Scope
    InputDigests
  Probe:
    Records
    Events
    Scope
    InputDigests
```

这里不要求 baseline/probe 的采样方式一致：

- 可以都是 `Records`
- 可以都是 `Events`
- 也可以是 `Events + fallback Records`

但进入分析前都必须先归一化成统一观测。

## 6.2 `tracessa` 的输出

建议定义输出对象：

```text
AnalysisResult
  Profile: ImpactProfile
  Debug:
    BaseGraph
    ProbeGraph
    BaseRoles
    ProbeRoles
    Wavefront
```

其中对 `evaluator` 真正必需的是：

- `Profile`

调试字段只用于调试和测试，不作为上层业务逻辑的依赖。

## 6.3 `tracessa` 的核心数据结构

下面这些结构是整个引擎的稳定 IR。

### 6.3.1 `NormalizedExecNode`

表示归一化后的黑盒执行节点。

字段建议包含：

- `PID`
- `ParentPID`
- `Argv`
- `Cwd`
- `Env`
- `Reads`
- `ReadMisses`
- `Writes`
- `Deletes`

约束：

- 所有路径都已经处于统一 canonical scope
- `Env` 必须是去噪、排序后的稳定环境视图，不能把样本私有噪音变量直接原样带入
- `ReadMisses` 必须保留失败 `open/stat/access` 一类负面依赖事实；不能在归一化时静默丢失
- 不携带 compile/configure/install 等动作语义

### 6.3.2 `PathState`

表示路径的一个版本状态。

字段建议包含：

- `Path`
- `Version`
- `Writer`
- `Tombstone`
- `Missing`

语义：

- 同一路径每次定义产生一个新版本
- 删除产生墓碑版本
- 失败读取或查找未命中可以绑定到负面状态 `P(path, n, missing=true)`
- `Tombstone` 和 `Missing` 不是一回事：
  - `Tombstone` 表示图内某个动作显式删除了该路径
  - `Missing` 表示该动作观测到了“这里不存在可读对象”的负面前提
- `Path` 是统一的分析 key，不限于真实文件系统路径；保留字命名空间如 `$ENV/CFLAGS` 也属于合法状态路径

### 6.3.3 `ReadBinding`

表示一次读取绑定到哪些 reaching-def。

字段建议包含：

- `Path`
- `Defs`

约束：

- `Defs` 可能为空以外不能被偷偷折叠
- 若存在多个不可比较定义，必须显式保留多个 `Defs`

### 6.3.4 `Graph`

表示 Path-SSA 图。

图上只有两种对象：

- 执行节点
- 路径状态

建议结构至少包含：

- `Actions`
- `ActionReads`
- `ActionWrites`
- `InitialDefs`
- `DefsByPath`
- `ReadersByDef`
- `ParentAction`
- `Out` / `In` 或任何足以恢复偏序的边信息

### 6.3.5 `RoleProjection`

表示投影到 SSA 图上的角色视图。

建议结构包含：

- `ActionClass`
- `DefClass`
- `ActionNoise`
- `DefNoise`
- `ActionDeliveryOnly`

这个结构是**附加视图**，不是 Graph 的一部分。

### 6.3.6 `WavefrontResult`

表示第四阶段的分类结果。

建议结构包含：

- `ProbeClass`
- `DivergedDefs`
- `MutationRoots`
- `FlowActions`
- `UnchangedActions`
- `JoinSet`
- `ReadAmbiguous`

### 6.3.7 `ImpactProfile`

这是 `tracessa` 的正式输出。

建议字段：

- `SeedW`
- `Need`
- `FS`
- `JoinSet`
- `SeedStates`
- `NeedStates`
- `FlowStates`
- `Ambiguous`

含义固定：

- `SeedW`：直接起火点写出的 diverged 路径
- `Need`：发散闭包之外、且被发散动作真实依赖的前提路径
- `FS`：所有 diverged 状态对应路径的总和
- `JoinSet`：所有同时消费“闭包内发散状态”和“闭包外稳定前提”的动作集合

---

## 7. `tracessa` 的五阶段流程

下面是 `tracessa` 内部必须实现的完整流程。

## 7.1 第一阶段：观测归一化

### 作用

把原始 trace 观测清洗成可比较的黑盒执行节点。

### 输入

- `Records`
- `Events`
- `Scope`
- `InputDigests`

### 输出

- `[]NormalizedExecNode`

### 具体要做什么

#### 1. 路径归一化

把路径映射到统一 canonical 空间：

- `/tmp/build-abc/lib.a` -> `$BUILD/lib.a`
- `/tmp/install-xyz/bin/foo` -> `$INSTALL/bin/foo`

路径归一化必须稳定，不能因为不同样本的临时根不同而改变身份。

#### 2. `argv / cwd / env` 去噪

去掉不影响结构判定的噪音：

- 绝对临时根
- 随机后缀
- 时间戳
- 样本私有工作目录前缀

但不能把有语义的环境变量直接抹掉。

像下面这类会改变构建行为的环境输入必须保留下来并规范化：

- `CFLAGS`
- `CPPFLAGS`
- `LDFLAGS`
- `PKG_CONFIG_PATH`
- `LD_LIBRARY_PATH`
- 任何被当前动作真实继承并可能改变产物的环境项

#### 3. 事件折叠

如果底层输入是 syscall 事件序列，则要折叠成执行节点级摘要：

- 一条 exec 对应一个黑盒节点
- exec 时继承的环境快照归入 `Env`
- 读事件归入 `Reads`
- 失败 `open/stat/access` 事件归入 `ReadMisses`
- 写事件归入 `Writes`
- 删除事件归入 `Deletes`

#### 4. 进程树关联

保留：

- `PID`
- `ParentPID`

并恢复稳定的父子关系和基础先后事实。

### 这一步绝对不能做什么

- 不得判断“这是 configure / compile / install”
- 不得给路径打 mainline/tooling/probe 标签
- 不得提前推导 `Need / SeedW / FS`

### 为什么必须这样

因为这一层的任务只是“把事实洗干净”。  
一旦在这里混入语义判断，后面所有阶段都会被污染。

## 7.2 第二阶段：构建 Path-SSA

### 作用

把归一化后的黑盒命令列表升级成路径状态版本图。

### 输入

- `[]NormalizedExecNode`

### 输出

- `Graph`

### 图中有哪些对象

只有两类对象：

- 执行节点 `E`
- 路径状态 `P(path, n)`

边只有两类：

- `P -> E`
- `E -> P`

### 具体怎么建

#### 1. 初始状态

每个被读取但尚未在图内定义过的路径，都隐式拥有一个基线状态：

```text
P(path, 0)
```

它表示构建开始前外部世界的状态。

环境变量也按同样方式进入图，但使用保留字路径空间：

```text
P($ENV/NAME, 0)
```

也就是说，`NormalizedExecNode.Env` 在建图时必须展开成该动作的一组只读输入状态。
这样环境变化才能进入数据流，也才能参与 ready 判定、等价检查和 root 判定。

#### 2. 写入建新版本

当某个节点写路径 `p` 时，产生一个新版本：

```text
P(p, k+1)
```

#### 3. 删除建 tombstone

当某个节点删除路径 `p` 时，产生：

```text
P(p, k+1, tombstone=true)
```

删除必须进入数据流，不能靠“路径不存在”这种隐式含义表示。

#### 4. 读取绑定 reaching-def

当节点读取路径 `p` 时，绑定到在当前保守因果偏序下对它可达的 reaching-def 集合。

读取有四种情况：

1. 唯一前置定义
2. 没有图内定义，回退到 `P(path, 0)`
3. 若底层明确观测到失败读取或查找未命中，则绑定到负面状态，例如 `P(path, 0, missing=true)`
4. 多个不可比较定义，形成 `ambiguous read`

这里必须区分三件事：

- `P(path, 0)`：表示“外部世界里存在一个初始状态，但当前图内没有更晚定义”
- `P(path, k, tombstone=true)`：表示“图内某个动作显式删除了它”
- `P(path, 0, missing=true)`：表示“该动作真实探测过这个路径，并观测到这里不存在可读对象”

如果 trace 已经记录了失败探测，就不能把第 3 种情况偷换成第 2 种情况。
否则后续另一个 option 新建同一路径时，SSA 图上会丢掉这条真实的负面依赖。

### 偏序怎么定义

这里只允许使用当前明确可观测的证据：

- 同一 pid 的稳定顺序
- `pid / parent` 关系
- 已观测读写依赖

不允许假设一个虚假的全局线性时间轴。

### 这一步不能做什么

- 不得写入动作语义
- 不得区分主线或探针
- 不得基于目录语义猜测隐式依赖

## 7.3 第三阶段：角色投影

### 作用

把不应参与主线碰撞判定的控制面噪音从 SSA 图上隔离出去。

### 输入

- `Graph`

### 输出

- `RoleProjection`

### 角色投影的原则

角色是**图上的投影结果**，不是图本体。

也就是说：

- `Graph` 永远保持纯 Path-SSA
- 角色信息只存在于 `RoleProjection`

### 角色投影具体怎么做

#### 1. Hard sinks

角色投影的主判断不应是“路径长得像什么”，而应是：

> **这个状态最终有没有进入真实产物链。**

但这里不能直接把 sink 定义成“被非噪音动作继续消费的产物”，因为第三阶段本身正是在判定谁是噪音、谁不是噪音。

因此第三阶段必须先从**与角色无关的硬事实**出发，建立一组不会自举循环的锚点，也就是 **hard sinks**。

原则：

- 任何最终逃到 `$INSTALL` 或等价交付面的状态，都属于 hard sink
- 出现在 output manifest 中的状态，属于 hard sink
- evaluator 显式给出的外部产物根，属于 hard sink

这一步的目标不是“先把所有路径分类完”，而是先定义：

> **哪些东西代表真实产物链的终点。**

#### 2. Tooling / Probe 种子

再从动作图的可观测行为中识别控制面种子，例如：

- 长出大批 control-plane 写入、但这些写入无法进入 hard sinks 的动作
- 只生成被子进程继续执行、却不进入真实产物链的 produced-exec 动作
- 位于明显的自检 / 自举 / 临时子图入口处的动作
- 已知 delivery plane 外、但其全部写出最终都只流向工具子图的动作

对当前 Path-SSA 实现，推荐把这里进一步落成两层图证据：

1. `seed actions`
   - configure/tool bootstrap action 只能作为弱 hint
   - 真正进入种子集的，还必须满足至少一种图关系证据：
     - 子进程链由已知 tooling action 派生
     - `execPath` 来自 tooling writer
     - 写出只被局部工具子图继续消费/执行
2. `workspace roots`
   - 不再用 `TryCompile` / `cmTC` / 特定目录名直接判 probe
   - 而是从 action 的 `cwd/read/write` 关系中，推断一组局部 tooling workspace roots
   - 这些 roots 只是 island membership 的证据，不是路径角色本身

这里必须区分 3 个层次，不能混用：

- `hint`
  - 例如 `KindConfigure`、configure/tool bootstrap 父子链、明显的局部自举入口
  - 它们只能把 action 提名为候选，不能直接把 action/path 判成 tooling/probe
- `evidence`
  - 例如 `execPath` 来自 tooling writer、写出只被局部子图继续消费/执行、`cwd/read/write` 收敛到同一局部 workspace
  - 这些证据用来支持 island 候选，但仍不是最终角色
- `membership`
  - 最终的 tooling/probe island membership 必须由后续不动点解出
  - 不能因为 action 恰好位于某个 workspace root 下，或路径名看起来像 probe artifact，就直接判定 membership

也就是说，`workspace root` 只是“这个 action/def 可能属于局部 tooling island”的结构证据；
真正的 island membership，必须等到 Step 3 的联合不动点之后才能成立。

这里的识别仍然只是投影起点，不是给 Graph 节点改类型。

这一步的种子识别也应尽量来自图上的行为证据，而不是工具链名字或路径外观。
如果某个候选种子没有足够图证据支撑，就不应硬判成 tooling/probe，而应保留给后续 `Ambiguous` / mainline-visible 处理。

#### 3. SSA 逃逸分析

这里不能把 Step 2 理解成“从 tooling 种子做一次无约束的前向闭包，然后凡是到不了 hard sinks 的都切掉”。

那样在 `hard sinks = ∅` 的裸构建里会直接出错。例如：

```text
A1(configure) -> config.cache
A1(configure) -> config.h
A2(cc)        -> 读 main.c, config.h -> 写 app
```

如果把 A1 当 tooling seed，并从 A1 做无约束前向传播，那么 `config.h -> A2 -> app` 整条链都会被 seed 感染；由于此时没有 hard sinks，整片图都会被误判成 `NoEscape`，真实主线被整块吞掉。

因此这里必须做的是：

> **definite-tooling-only 的 must-analysis，而不是 tooling 污染的 may-analysis。**

也就是说，第三阶段只能剔除那些**被证明一定只停留在 tooling/probe 子图内部**的状态；凡是存在主线逃逸可能的状态，都不能在这一步切掉。

这里的不动点对象不能只是一组 path，也不能只是一组 action，而应当是当前 Path-SSA 图上的二部节点：

- `PathState`
- `Action`

换句话说，Stage 3 需要解的是一个 **联合 island membership**：

- 某个 `Action` 只有在其 `exec/read/write` 都仍满足 tooling-only 条件时，才可以留在候选 island 内
- 某个 `PathState` 只有在其 writer 和全部消费者都仍留在候选 island 内时，才可以留在候选 island 内
- 一旦 `Action` 或 `PathState` 任一侧越过边界，对侧也必须在后续迭代中同步退出

因此，generic wrapper / `gmake` / `cc` / `ld` 这类动作是否属于 probe island，不能靠“动作名看起来像工具”来判断，
也不能靠旧 action-graph 的目录规则直接提升；必须通过它在 `def -> action -> def` 图上的联合 membership 来证明。

从这些种子出发时，判断规则应当是：

- 只有当某个 `PathState` 的所有消费者都仍然停留在 tooling/probe 候选子图内部时，它才有资格留在 `NoEscape` 候选集中
- 只要某个 `PathState` 被一个混合消费者读取，这个状态就必须立即退出 `NoEscape` 候选集
- 这里的“混合消费者”指：某个 action 同时读取了 seed-reachable control-plane def 和 seed 外的其他输入（例如源码、主线编译输入、未证实为 tooling 的 def）
- 到达 hard sinks 当然也是逃逸，但不是唯一逃逸条件

因此，真正的 `NoEscape` 不是“从 seed 能走到哪里”，而是：

> **从 seed 出发、且始终不跨越 mixed-consumer frontier 的那部分最大不动点。**

在上面的例子里：

- `config.cache` 若只在 configure/probe 子图内部使用，可以保留在 `NoEscape`
- `config.h` 一旦被 `A2(cc)` 读取，而 `A2` 同时还读取了 `main.c`，它就已经跨越了 mixed-consumer frontier，不能再算 tooling-only
- `A2` 和 `app` 因此会保留在残余图中，留给 derived sinks 和 backward slice 处理

#### 3.1 这里说的“稀疏逃逸分析”是什么意思

这里借鉴的是传统 SSA / points-to 分析里的 sparse escape 思路，但对象不是“堆对象是否逃出函数”，而是“某个 `PathState` 是否逃出 tooling/probe 子图”。

传统 sparse escape 的核心不是按每个 basic block 维护 dense 的 IN/OUT 集合，而是：

1. 先建立 SSA 值或对象节点
2. 再建立真实的 use-def / points-to 边
3. 最后在这张稀疏图上用 worklist 跑到不动点

只有沿真实依赖边传播，才叫 sparse。

映射到这里时，对应关系应当是：

- 传统分析里的“对象节点 / allocation site” -> 这里的 `PathState = P(path, version)`
- 传统分析里的“值流边” -> 这里的 `def -> action read`
- 传统分析里的“对象可达边 / store-load 传播” -> 这里的 `action -> written defs`
- 传统分析里的“逃逸汇点” -> 这里的 hard sinks

因此，这里的稀疏逃逸分析本质上就是一个带 frontier 的稀疏不动点：

1. 以 configure / probe / tooling seeds 写出的 `PathState` 作为初始候选
2. 以这些 `PathState` 可达的 `Action` 作为 action 侧初始候选，但它们仍需经过后续删减
3. 沿 `def -> action -> def` 的真实依赖链传播，但只允许穿过仍满足 tooling-only 条件的 action
4. 若某个候选 `Action` 的 `exec/read/write` 越过局部 island 边界，则它记为 `EscapeResidual`
5. 若某个候选 `PathState` 被 mixed consumer 读取，则它记为 `EscapeResidual`
6. 若某个候选 `PathState` 到达 hard sinks，则它记为 `EscapeHardSink`
7. 只有那些既不到达 hard sinks、也不跨越 mixed-consumer frontier 的 action/def 候选，才留在 `NoEscape`
8. 最终切掉的只是 `NoEscape` 子图，而不是整片 seed 前向闭包

在实现上，这里的 `mixed-consumer frontier` 不应再由“路径外观看起来像 probe artifact”来豁免。
如果某条状态需要特殊对待，必须通过它所属的 `tooling workspace root / probe island membership` 来证明，而不是通过 `looks*` 函数直接打补丁。

同样，`KindConfigure` 在实现里最多只能作为初始 hint：

- 它可以帮助把某些 action 放入初始候选
- 但不能单独决定某个 action/def 一定属于 tooling/probe
- 如果图上没有额外 relation evidence 支撑，这些候选必须在不动点中自然淘汰，而不是被硬保留

这一步的关键不是路径名长得像不像 sidecar，而是状态沿真实 SSA 依赖链最终流到了哪里。

例如：

- `_build/CMakeFiles/pkgRedirects` 若只在 configure/probe 子图内部闭环，且到不了任何 hard sink，则它属于 `NoEscape`
- `_build/cmake_install.cmake` 若会流到 install/control-plane，则它不属于 `NoEscape`

所以，Stage 3 的第一层推荐实现形态应当是一个小格上的稀疏不动点：

- `NoEscape`
- `EscapeResidual`
- `EscapeHardSink`

#### 4. Derived sinks

仅靠 hard sinks 还不够，因为很多裸构建根本没有：

- `$INSTALL`
- output manifest
- evaluator 提供的外部产物根

例如一个纯 `make` 构建可能只有：

```text
cc -> build/main.o
ld -> build/app
```

这时如果没有第二层 sink，自举会卡死：既没有 hard sink，也还没算出 mainline closure。

因此，在只剔除**已证明为 `NoEscape`** 的 tooling/probe islands 之后，还必须从**残余图**中推断一组 **derived sinks**。

这组 sinks 的定义不能再依赖“谁是 non-noise”，而应来自残余图本身的结构事实。

推荐做法：

1. 在剔除 `NoEscape` 后的残余图上计算 condensation DAG
2. 找出其中的 terminal components
3. 从 terminal components 中选出真实物化前沿，作为 derived sinks
4. 这些 derived sinks 代表“trace 结束时仍存活、且未被证明属于 tooling/probe 的终端产物前沿”

例如：

- `build/app` 是裸构建里的 derived sink
- `build/config.cache` 若已在前一层被证明属于 `NoEscape` tooling island，则它不参与 derived sink 候选
- `config.h` 若跨越了 mixed-consumer frontier，则它不会在前一层被切掉，而会继续留在残余图里参与主线闭包推断

#### 5. 主线反向闭包推断

最后从：

- `hard sinks`
- `derived sinks`

一起出发，在 Path-SSA 图上做一层 **backward slice / backward reachability**：

- `sink path -> writer action`
- `writer action -> read defs / initial defs`
- `read def -> producer action`
- 再从 `producer action -> written defs` 继续向上回溯

直到到达不动点。

得到的闭包不是“最终产物集合”，而是：

> **所有会真实影响最终产物链的 mainline-relevant 状态和动作集合。**

这一步的结果就是主判断本身，不允许再回退到 `looks*` 风格的路径启发式。

例如：

- `_build/trace_options.h` 若被真实 compile 读取，并进一步影响 `libtracecore.a` / install 输出，则它属于主线反向闭包
- `_build/CMakeFiles/pkgRedirects` 若只在 configure/probe 子图内部循环，且无法反向连接到任何 hard sink 或 derived sink，则它不属于主线闭包

也就是说，第三阶段推荐采用的主判断应当是：

> **从真实产物反推主线闭包。**

这和传统 SSA / PDG 分析里的 `backward slice from observable outputs` 是同一类思路，只是这里的节点不是局部变量，而是 `PathState` 和黑盒 `ExecNode`。

#### 6. 剩余路径的残差分类

经过“hard sinks + `NoEscape` tooling/probe islands + derived sinks + 主线反向闭包推断”之后，仍然可能剩下一小部分既不明显属于主线，也不明显属于噪音的残差状态。

这些状态可以再按以下顺序处理：

1. 若它们完全停留在 install/copy/delivery 平面，则归入 delivery-side
2. 若它们完全停留在 configure/probe/tooling 子图，则归入 tooling/probe-side
3. 若证据不足以做 sound 分类，则保留为 `Ambiguous`，或保守提升为 mainline-visible

这里不允许再引入路径外观 fallback。

也就是说：

- 不能因为路径名“看起来像 sidecar”就把它压成 tooling/probe
- 不能因为路径名“看起来像产物”就把它抬成 mainline
- 不能重新退化成工具链 catalog，比如硬编码 `TryCompile` / `cmTC` / 特定构建系统文件名列表

#### 7. 这一阶段的设计原则

角色投影的目标不是把每条路径都“猜成某个名字类目”，而是把 Path-SSA 图切成三层：

- 可反向连到真实产物 sink 的 mainline closure
- 明确不逃逸、且不跨越 mixed-consumer frontier 的 tooling/probe islands
- 只停留在交付面的 delivery-side states

推荐顺序应当是：

1. infer `hard sinks`
2. infer `seed actions` 与 `workspace roots`
3. 在 `PathState + Action` 二部图上跑 sparse must-analysis，得到 `NoEscape` tooling/probe islands
4. 从残余图 infer `derived sinks`
5. backward slice 得到 mainline closure
6. delivery-plane 残差隔离
7. 无法证明的残差保留为 `Ambiguous` 或保守视作 mainline-visible

这意味着 `looks*` 一类函数不应出现在 Stage 3 的正式设计里；一旦还需要它们，说明图推断本身还不够强。

当前实现若需要判断“某条状态是否属于 probe workspace”，也必须先从图关系推断 `workspace roots / island membership`，再据此分类；不能直接按路径名或目录名做角色决策。

### 结果是什么

最后得到：

- 哪些 action 是 noise
- 哪些 state 是 noise
- 哪些 action 是 delivery-only
- 哪些 action/state 仍属于主线

### 这一步不能做什么

- 不得修改 Graph 里的读取绑定
- 不得删除节点
- 不得根据角色回写版本结构

## 7.4 第四阶段：Wavefront Diff

### 作用

在 baseline 和 probe 两张 role-aware SSA 图上推进差分波面，把 probe 侧动作分成：

- `MutationRoot`
- `Flow`
- `Unchanged`

### 输入

- baseline `Graph`
- probe `Graph`
- baseline `RoleProjection`
- probe `RoleProjection`
- 路径 digest 证据

### 输出

- `WavefrontResult`

### 具体怎么做

#### 1. 定义内在签名

每个动作提取内在签名：

```text
Hash(normalized argv + normalized cwd + normalized env footprint + read path set + write path set)
```

这里故意不带版本号，因为签名描述的是行为形状，不是世界线位置。

环境变量不能只停留在归一化文本里而不进入签名。
否则像 `CFLAGS=-O0` 与 `CFLAGS=-O3` 这类直接改变动作行为的差异，会在波面入口被错误压平。

#### 2. 找 ready 起点

从那些输入都绑定到等价 `P(path, 0)` 的动作开始推进。

如果一个动作只读到等价前提，它就有资格进入 baseline ready pool / probe ready pool。

#### 3. 噪音拦截

若动作或状态已经被第三阶段标成噪音：

- 它不进入主线差分
- 它写出的状态不进入主线下游

#### 4. Flow 判定

如果 probe 动作的任一可见输入已经是 diverged，则：

- 该动作一定是 `Flow`
- 它写出的所有可见状态都变为 diverged

#### 5. MutationRoot 判定

只有当 probe 动作的全部可见输入都仍是 equivalent 时，才允许做 root 判定。

做法是：

1. 去 baseline ready pool 中查找相同内在签名的候选
2. 在整个候选集合内继续做等价检查
3. 若存在唯一等价候选，则该 probe 动作为 `Unchanged`
4. 若不存在等价候选，则该 probe 动作为 `MutationRoot`
5. 若候选集合本身无法唯一决断，则记为 `Ambiguous`

### 这里必须特别强调

不能复用旧实现里那种：

> “同签名 bucket 里拿第一个候选就定生死”

这种做法会把候选顺序错误地当成起火证据。

正确做法必须是：

> **bucket 内完整检查，再做分类。**

## 7.5 第五阶段：Impact 提取

### 作用

把第四阶段的动作分类压成 `ImpactProfile`。

### 输入

- probe `Graph`
- probe `RoleProjection`
- `WavefrontResult`
- baseline 对齐结果

### 输出

- `ImpactProfile`

### 具体怎么提取

#### 1. `SeedW`

由 `MutationRoot` 写出的、且实际被标为 diverged 的物理路径。

#### 2. `FS`

所有 diverged 状态对应路径的总和，包括：

- `SeedW`
- `Flow` 写出的路径
- 删除产生的 tombstone 路径

#### 3. `Need`

`Need` 只表示 **发散动作实际依赖、且不由当前发散闭包自己产出的前提状态**。

这里的“前提状态”是指：

- 不由当前发散闭包内部定义
- 但被 `MutationRoot` 或 `Flow` 动作真实读取
- 并且能够影响这些动作输出的状态

`Need` 不是所有下游输入的全集，但也绝不能为了压制伪碰撞而删掉真实前提。

正确规则是：

1. 遍历所有 `MutationRoot` 和 `Flow` 动作的读取
2. 只保留不由当前发散闭包产出的前提状态；它既可能是图外 `P(path, 0)`，也可能是图内但稳定的中间状态
3. 只要某个前提状态被发散动作实际读取，它就必须进入 `Need`
4. 即使 baseline 对应动作原本也读取了这个前提状态，也不能因此把它从 `Need` 中删除
5. baseline 对齐只能用于识别观测噪音、delivery 污染和无关 ambient 依赖，不能用来删除真实前提
6. `delivery-only` 或纯安装复制动作的读取不进入 `Need`
7. 若某个读取命中的状态已经由同一发散闭包内部产出，则它不进入 `Need`；这类状态已经属于闭包内部传播，应由 `FS` 描述

### 实现要求

为了避免把真实 RAW 冲突漏掉，`Need` 的实现必须按下面的顺序做：

1. 第四阶段先完整算出 `MutationRoot`、`Flow` 和整条 diverged 闭包
2. 第五阶段再统一遍历所有 `MutationRoot` 和 `Flow` 动作的读取
3. 对每个读取命中的状态，先判断它是否已经在当前 diverged 闭包里
4. 若该状态已经属于当前闭包内部传播，则跳过，不进入 `Need`
5. 若该状态不属于当前闭包，但这个动作真实读取了它，则把它加入 `Need`
6. 只有角色噪音、`delivery-only` 读取、观测歧义这几类情况允许阻止加入 `Need`

实现时必须明确禁止两类旧错误：

1. 不能在 root 阶段提前把读取直接塞进 `Need`
   因为此时还不知道哪些读取其实会在后续 flow 闭包里变成内部传播，提取得太早会把边界画错
2. 不能因为 baseline 对应动作本来也读取了这个前提，就把它从 `Need` 删除
   这会把“baseline 原本就读、但组合时仍会被另一边 option 污染”的真实前提静默漏掉

### 例子 1：图内稳定中间件不能漏

基线：

```text
compile a.c -> a.o
link a.o -> app
```

两个 option：

- `X` 修改 `compile`
- `Y` 修改 `link`

此时：

- `X` 会写出新的 `a.o`
- `Y` 虽然只改了 `link`，但 `link` 仍然真实读取 `a.o`

实现上如果把 `Need(Y)` 限成“只收图外前提”，就会把 `a.o` 漏掉，最后错误判成 `X` 与 `Y` 正交。

正确做法是：

- `link` 读取了 `a.o`
- `a.o` 不是 `Y` 自己这条发散闭包产出的状态
- 所以 `a.o` 必须进入 `Need(Y)`

这样 `X` 写出的 `a.o` 才会在 Stage 2 的 RAW 判定里撞上 `Y` 的前提。

### 例子 2：baseline 本来也读过，仍然不能删

基线：

```text
gen flags.in -> flags.txt
compile main.c common.h flags.txt -> main.o
link main.o -> app
```

两个 option：

- `A` 修改 `gen`，导致 `flags.txt` 改变
- `B` 修改 `common.h`

在 `A` 的单项分析里：

- `compile` 会因为 `flags.txt` 被污染而变成 `Flow`
- 这个 `Flow compile` 同时仍然读取 `common.h`

如果实现里用了“baseline 对应动作本来也读 `common.h`，所以不用记进 `Need`”这条过滤，系统就会把 `common.h` 错删掉，最终误判 `A` 与 `B` 正交。

正确做法是：

- 只要 `Flow compile` 真实读取了 `common.h`
- 且 `common.h` 不是 `A` 自己的发散闭包内部产物
- 那么 `common.h` 就必须进入 `Need(A)`

这样另一边 option `B` 对 `common.h` 的改动才能在 RAW 判定里被看见。

### 为什么要这么做

因为 `Need` 最容易被错误实现成第一种形式：

> “所有受影响动作的所有输入”

那会直接制造大量伪碰撞。

第二种同样错误的形式是：

> “只保留 probe 相对 baseline 新出现的外部依赖”

这会把那些 baseline 本来就读取、但在组合时仍然会被另一边 option 修改的真实前提错误删掉，进而漏掉真实 RAW 冲突。

#### 4. `JoinSet`

`JoinSet` 表示 **所有同时消费“闭包内发散状态”和“闭包外稳定前提”的动作集合**。

它不是碰撞判定字段，而是给后续 replay / merge 规划提供 join 信息。

先定义：

- `ReachState`：从 `MutationRoot` 写出的 diverged 状态出发，在 probe 图上沿 `def -> use -> def` 可达的全部状态
- `ReachExec`：在同一传播闭包中可达的全部动作
- `StablePrereq`：不属于当前 `ReachState`，但属于当前分析域且可见的稳定前提状态
- `MixedExec`：所有同时满足以下条件的动作：
  1. 该动作属于 `ReachExec`
  2. 它至少读取一个来自 `ReachState` 的输入
  3. 它还读取了至少一个来自 `StablePrereq` 的输入

则：

> `JoinSet = MixedExec`

更直白地说：

- 只要一个动作同时消费了“已经发散的输入”和“闭包外仍稳定的前提”，它就属于 `JoinSet`
- 后续是否只取最靠前的一层，是 `JoinSet` 之上的派生操作，不是 `JoinSet` 自身定义的一部分

这里的 `StablePrereq` 包括两类来源：

- 图外初始状态，例如 `P(path, 0)`
- 图内但稳定、且不由当前发散闭包产出的中间状态，例如未变化的 `b.o`

因此，像下面这种 fan-in：

```text
a.o (diverged) \
                -> link -> bin
b.o (stable)   /
```

链接动作必须进入 `JoinSet`，因为它同时读取了发散输入和稳定前提。

提取规则固定：

1. 只在 `MutationRoot + Flow` 诱导出的传播闭包里找 join
2. 纯噪音动作、`delivery-only` 动作不进入 `JoinSet`
3. 只要一个动作满足 mixed 条件，就进入 `JoinSet`
4. 若 Stage 3 需要最小 replay 起点，再从 `JoinSet` 派生 `min(JoinSet)`
5. 证据不足以判断某个动作是否属于 `JoinSet` 时，直接标记 `Ambiguous`

---

## 8. `evaluator` 模块设计

## 8.1 `evaluator` 的作用

`evaluator` 不再自己分析 trace。

它只做四件事：

1. 组织 baseline 和 singleton 的输入
2. 调用 `tracessa`
3. 比较两个 `ImpactProfile`
4. 把 Stage 2 的结论交给上层

## 8.2 `evaluator` 的输入

仍然使用现有上层采样得到的 `ProbeResult` 或等价对象。

每个 `ProbeResult` 至少提供：

- `Records`
- `Events`
- `Scope`
- `InputDigests`

## 8.3 `evaluator` 的输出

对单项 option：

- 返回 `ImpactProfile`

对两项 option 的正交判定：

- 返回 `OrthogonalityResult`

建议 `OrthogonalityResult` 至少包含：

- `Orthogonal`
- `Hazards`
- `LeftProfile`
- `RightProfile`
- `Ambiguous`

## 8.4 `evaluator` 具体流程

### 单项分析流程

1. 取 baseline `ProbeResult`
2. 取 option `X` 的 singleton `ProbeResult`
3. 组装成 `tracessa` 的 `AnalysisInput`
4. 调用 `tracessa.AnalyzeImpact`
5. 得到 `ImpactProfile(X)`

### 双项碰撞流程

1. 分别得到 `ImpactProfile(A)` 和 `ImpactProfile(B)`
2. 判定 `SeedW(A) ∩ SeedW(B)`
3. 判定 `FS(A) ∩ Need(B)`
4. 判定 `FS(B) ∩ Need(A)`
5. 判定 `FS(A) ∩ FS(B)`
6. 记录所有 hazard
7. 若任一侧 `Ambiguous=true`，直接判定不能跳过真实组合构建

四类判定的语义必须固定：

#### `SeedW(A) ∩ SeedW(B)`

这是直接写写冲突。  
两边都在定义同一路径，属于最直接的 WAW。

#### `FS(A) ∩ Need(B)`

这是从 A 指向 B 的 RAW。  
表示 A 的发散结果会污染 B 的真实前提状态。

#### `FS(B) ∩ Need(A)`

这是从 B 指向 A 的 RAW。  
表示 B 的发散结果会污染 A 的真实前提状态。

#### `FS(A) ∩ FS(B)`

这是汇聚型共享产物冲突检查，不能省略。

它捕获的是：

- 两边沿不同路径传播
- 最终在某个 fan-in 节点或共享输出路径上汇聚
- 从而共同改写同一个中间产物或最终产物

如果没有这一步，凡是依赖图内内部状态汇聚到同一输出的场景，都会被错误地当成正交。

### `FS ∩ FS` 命中的处理规则

`FS(A) ∩ FS(B)` 非空后，不能直接忽略，也不能简单一刀切。

正确规则是：

1. 若交集路径不在任何显式允许的 replay / merge surface 上，则直接判为冲突
2. 若交集路径位于显式允许的 replay surface 上，则记录为 `join hazard`，交由 Stage 3 判断是否能通过 replay 吸收
3. 只有当 Stage 3 明确声明该交汇可被吸收时，才允许继续放行

也就是说：

> `FS ∩ FS` 至少必须进入 hazard 集，绝不能因为它不属于 `Need` 就被静默忽略。

### `JoinSet` 的用途

`JoinSet` 不参与 Stage 2 的正交矩阵判定。

它只用于后续 Stage 3：

1. 从 `JoinSet` 派生 `min(JoinSet)` 作为 replay roots
2. 限制 replay 只从真正需要重新混合的那一层开始
3. 避免因为缺少边界信息而把整段下游主线全部重放

换句话说：

- `Need / FS / SeedW` 回答的是“会不会撞”
- `JoinSet` 回答的是“哪里发生了发散流与稳定前提的 join”
- `min(JoinSet)` 回答的是“如果要吸收这次交汇，最小该从哪里开始重放”

## 8.5 `evaluator` 绝对不能再做什么

以下逻辑必须从 `evaluator` 中彻底删除：

- 自己构图
- 自己做角色分类
- 自己实现 wavefront
- 自己从 action graph 直接推导 `Need / SeedW / FS`
- 自己维护第二套“看起来差不多”的 Stage 2 启发式

`evaluator` 一旦保留这些逻辑，重构就会再次退化成旧结构。

---

## 9. 端到端完整流程

下面是重构后的完整执行流程。

## 9.1 单项 option 分析

```text
baseline ProbeResult
probe ProbeResult
    -> evaluator 组装 AnalysisInput
    -> tracessa 阶段 1：观测归一化
    -> tracessa 阶段 2：建 Path-SSA
    -> tracessa 阶段 3：角色投影
    -> tracessa 阶段 4：Wavefront 差分
    -> tracessa 阶段 5：Impact 提取
    -> 返回 ImpactProfile
```

## 9.2 两个 option 的正交判定

```text
ImpactProfile(A)
ImpactProfile(B)
    -> evaluator 判定 Seed-WAW
    -> evaluator 判定 Flow-Need RAW
    -> evaluator 判定 FS-FS 汇聚冲突
    -> evaluator 记录是否 Ambiguous
    -> 输出 Stage 2 正交结论
```

## 9.3 Stage 3 合成入口

```text
ImpactProfile(A)
ImpactProfile(B)
    -> synthesis / replay planner 读取 JoinSet
    -> 派生 min(JoinSet) 作为 replay 起点
    -> 若 min(JoinSet) 过宽或 hazard 不可吸收，则回退真实组合构建
```

---

## 10. 文件组织建议

虽然只保留两个模块，但每个模块内部仍然要按文件职责拆开，避免再次长成一坨。

## 10.1 `internal/trace/ssa`

建议文件划分：

- `normalize.go`
  观测归一化
- `graph.go`
  Graph / PathState / ReadBinding 等核心结构
- `build.go`
  Path-SSA 建图
- `roles.go`
  角色投影
- `wavefront.go`
  Wavefront 差分
- `impact.go`
  Impact 提取
- `analyze.go`
  对外总入口

这些文件仍属于同一个模块：`tracessa`。

## 10.2 `internal/evaluator`

建议文件划分：

- `evaluator.go`
  调用 `tracessa` 的总流程
- `impact_compare.go`
  两个 `ImpactProfile` 的碰撞判定
- `debug.go`
  调试输出

如果现有文件名更适合复用，可以保留文件名，但职责要按上面收敛。

---

## 11. 重构实施顺序

为了避免旧代码继续影响新实现，这次重构应按下面顺序推进。

## 11.1 第一步：先冻结接口和测试

先补足或保留这些测试：

- 真实 case 回归
- `Need` 提取回归
- duplicate signature 匹配回归
- tombstone 传播回归
- probe/tooling island 隔离回归
- ambiguous read 回归

这一步的目的不是保留旧逻辑，而是保留**外部行为要求**。

## 11.2 第二步：在 `tracessa` 内平行实现新引擎

先不切换 `evaluator`，只把新的五阶段引擎写出来并跑通测试。

## 11.3 第三步：让 `evaluator` 改为只消费 `tracessa`

一旦 `tracessa` 稳定，就把 `evaluator` 改造成纯编排器。

## 11.4 第四步：删除旧 Stage 2 逻辑

当且仅当：

- 新测试通过
- 回归样本通过
- `evaluator` 已不再依赖旧内部逻辑

才删除旧的 Stage 2 代码。

删除不是目标，删除只是为了确保后续实现不再被旧结构反向污染。

---

## 12. 风险和边界

## 12.1 目录枚举

当前核心模型不显式表达目录集合语义。

因此：

- `readdir`
- `getdents`
- `glob`

一类依赖若没有明确观测证据，必须保守回退。

## 12.2 失败探测

若底层稳定记录了失败 `open/stat/access`，则建图时必须显式生成负面状态并绑定到对应读取。

例如：

```text
probe read A.h -> miss
fallback read /usr/include/A.h -> success
```

这里第一步必须落成类似：

```text
P($BUILD/include/A.h, 0, missing=true)
```

否则如果另一个 option 恰好新建了这个 `A.h`，Stage 2 就看不见“原本这里不存在、但某个动作真实探测过它”这条依赖，后续可能漏掉 RAW。

若底层没有稳定记录失败探测，则系统不能假装已经恢复了这类负面前提。
这种情况下必须：

- 要么显式降级，不对 negative dependency 作精确判定
- 要么上抛 `Ambiguous`

## 12.3 歧义

以下情况必须上抛 `Ambiguous`：

- 多 reaching-def
- baseline ready pool 候选无法唯一对齐
- 结论依赖未观测能力

## 12.4 性能

新的 SSA 引擎一定会比旧的路径集合逻辑更重。  
这是可以接受的，因为目标首先是结构正确性和可维护性。

性能优化只能在不破坏模块边界的前提下进行。

---

## 13. 一句话总结

这次重构的本质不是“整理旧 `evaluator`”，而是：

> **让 `tracessa` 成为唯一的 Stage 2 分析引擎，让 `evaluator` 只做编排和判定。**

这样整个 Stage 2 才会重新变得可理解、可维护、可验证。 
