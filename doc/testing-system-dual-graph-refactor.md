# LLAR 自动测试矩阵缩减：设计说明与实现主线

## 1. 这套系统到底在干什么

`llar test --auto` 不是想“聪明地猜哪些组合大概不用跑”。

它做的是一件更具体的事：

- 先观察构建过程里真实发生的文件传播和关键动作变化
- 再据此判断哪些 option 彼此有碰撞
- 最后只对碰撞组做真实组合，对正交组做推断展开

所以这套系统的目标不是形式化证明，而是：

- 用尽量保守、可落地的证据
- 把明显没必要跑的组合裁掉

这也是为什么它同时输出两样东西：

- `reduced combos`
- `trusted / untrusted`

前者告诉你“建议跑哪些组合”，后者告诉你“这次建议值不值得信”。

## 2. 用一个例子先建立直觉

假设一个模块有两个 option：

- `tls`
- `docs`

baseline 构建时，系统观察到：

- `core.c -> core.o`
- `core.o -> libfoo.a`
- `libfoo.a -> $INSTALL/lib/libfoo.a`

打开 `tls` 的 probe 后，系统观察到：

- 新增 `tls.c -> tls.o`
- `tls.o` 被并入 `libfoo.a`

打开 `docs` 的 probe 后，系统观察到：

- 新增文档生成动作
- 新增 `$INSTALL/share/doc/...`

如果 `tls` 和 `docs` 的变化没有交汇：

- 没有共享传播路径
- 没有共享关键 `compile / link / archive` 动作槽位

那么系统就会把它们视为正交。

这意味着：

- `tls`
- `docs`
- `tls+docs`

不需要都真实执行；后者可以由前两个单变量 probe 的正交性推断出来。

但如果另一个 option `http2` 也改了 `libfoo.a` 这条链，或者也改了同一个 `compile` 槽位，那它和 `tls` 就会被判成碰撞，组合不能省。

## 3. 系统依赖什么证据

当前实现只依赖构建期间可观察到的黑盒事实，不依赖源码语义。

它关心的是：

- 启动了什么命令
- 命令在什么目录执行
- 读了哪些路径
- 写了哪些路径
- 哪些路径被 rename / unlink / symlink

这些事实来自 `strace`，进入系统后的基本单位叫 `Record`。

可以把一个 `Record` 理解成：

- “某次构建动作片段所对应的一份原始行为记录”

它还不是 evaluator 最终使用的稳定动作，但它是后续所有分析的原材料。

### 当前明确不做的事

为了避免误解，先把边界说清楚。

当前版本明确不做：

- AST / IR / 编译器语义分析
- 环境变量的完整因果分析
- point-in-time 输入快照
- 最终安装树的全面产物对比
- 依赖模块的全链路 trace

因此，这套系统最准确的定位是：

- 基于构建行为证据的矩阵缩减器

而不是：

- 程序语义等价证明器

## 4. 从 trace 到结论的 7 个步骤

理解当前实现，最好的方式不是从模块文件开始，而是顺着一条数据流往下看。

### 步骤 1：采集 trace，得到 `Record`

系统只抓少量关键 syscall，例如：

- `execve`
- `chdir`
- `open/openat/openat2/creat`
- `rename/renameat/renameat2`
- `unlink/unlinkat`
- `mkdir/mkdirat`
- `symlink/symlinkat`
- `clone/fork/vfork`

这里有几个实现细节值得保留：

1. 只接受成功 syscall  
失败探测不记入真实依赖，避免把 `TryCompile` 里的失败尝试误判成传播事实。

2. `open` 类路径优先用 `strace -yy` 返回的 `fd</resolved/path>`  
这样通过 symlink 打开的文件，更容易归到真实对象路径，而不是停留在字面 alias 上。

3. parser 会产出 `ParseDiagnostics`  
例如：
- `MissingPIDLines`
- `InvalidCalls`
- `ResumedMismatches`
- `UnrecognizedLines`

这些诊断会直接参与后面的 `trusted` 判定。

### 步骤 2：归一化路径和 token

trace 原始路径不能直接用于动作身份，因为它们往往带有太多偶然噪声，例如：

- 临时目录前缀
- 本次 probe 的 build/install 根
- 某些构建器生成的随机目录名

所以系统有两层归一化。

#### `normalizePath`

它做的是最基础的词法清洗，例如：

- 统一路径分隔符
- 折叠根级别 tmp 路径

但它现在不会再做过度的全局替换，比如：

- 不会把 `/port/8080/config` 里的 `8080` 当成 PID
- 不会把 `libfoo.so.1.2.345` 的版本号当成噪声

#### `normalizeScopeToken`

这是更重要的一层。它会把：

- `SourceRoot`
- `BuildRoot`
- `InstallRoot`

替换成：

- `$SRC`
- `$BUILD`
- `$INSTALL`

这样：

- `/tmp/run-a/src/foo.c`
- `/tmp/run-b/src/foo.c`

会被看成同一个逻辑路径。

另外，当前还保留了一层**收窄后的 build-noise 归一化**，但只在 `$BUILD` 下对明确模式生效，例如：

- `TryCompile-12345`
- `cmTC_a1b2c3`
- `foo.tmp.1234`

而像：

- `TryCompile-doc`
- `cmTC_tls`

这种携带语义的信息不会被吞掉。

### 步骤 3：把 `Record` 归并成稳定动作

原始 `Record` 很碎，不能直接用于碰撞分析。

系统会先识别动作类型，例如：

- `configure`
- `copy`
- `install`
- `archive`
- `link`
- `compile`
- `generic`

这里最重要的不是分类名字，而是分类后的后果：

- `compile/link/archive` 这些关键动作会获得更强的身份和差分能力
- `generic` 则是保守回退，说明系统没把握给它更强语义

#### compile pipeline 合并

现实里的一个逻辑 compile 往往不是一条记录，而是一串进程：

- `cc -> cc1 -> as`
- `c++ -> cc1plus -> as`

当前 evaluator 会把这串流水线先合成一个逻辑 `compile` 动作。

它分两步做：

1. 先按 `PID / ParentPID` 做非相邻归并  
为了处理并行构建下的交错 trace。

2. 再保留相邻窗口回退  
为了兼容没有稳定进程关系信息的简单样本。

合并后的效果是：

- 这条 compile 流水线的 `reads` 取并集
- `writes` 取并集
- 最后只留下一个稳定动作节点

如果没有这一步，像 `.s`、`.tmp` 这样的中间临时文件会把传播链切碎。

### 步骤 4：给动作建立两套身份

这是当前实现里一个很关键、也很容易被忽略的设计。

系统不会只给动作一个身份，而是给两套：

- `fingerprint`
- `actionKey`

#### `fingerprint`

`fingerprint` 是完整身份，用来回答：

- “这两个动作是否可以看成同一个具体动作实例”

它会吃进：

- 归一化后的 `argv`
- `cwd`
- `reads`
- `writes`

所以它很细。

例如：

- `gcc -DUSE_TLS -c core.c -o core.o`
- `gcc -DUSE_HTTP2 -c core.c -o core.o`

即使输入输出路径一样，只要参数不同，`fingerprint` 就不同。

#### `actionKey`

`actionKey` 是更粗的语义槽位，用来回答：

- “这两个动作是不是在改同一个逻辑位置”

它主要用于：

- `compile`
- `link`
- `archive`

可以把它理解成：

- “同一个 `core.c -> core.o` 槽位”
- 而不是“完全相同的一次编译”

为什么要有两套身份？

因为只用 `fingerprint` 会太细，很多“同一槽位但参数不同”的动作根本对不上；只用 `actionKey` 又会太粗，丢掉具体差异。

### 步骤 5：先分清两类节点：动作节点 vs 路径节点

这里是最容易读混的地方。

当前图里其实同时存在两类东西：

- **动作节点**
- **路径节点**

它们不是一个概念，也承担不同职责。

#### 动作节点是什么

动作节点表示：

- “谁做了一次事”

例如：

- `cc -c foo.c -o foo.o`
- `ar rcs libfoo.a foo.o`
- `cp libfoo.a $INSTALL/lib/libfoo.a`
- `perl util/dofile.pl -> generated.c`

动作节点关心的是：

- `argv`
- `cwd`
- 读了哪些路径
- 写了哪些路径
- 它属于什么 `kind`

所以动作节点回答的问题是：

- 这一步到底是什么类型的行为
- 是 `compile`、`link`、`archive`
- 还是 `copy`、`install`
- 还是系统暂时看不懂的 `generic`

前面提到的：

- `fingerprint`
- `actionKey`

都属于动作节点的身份。

#### 路径节点是什么

路径节点表示：

- “某个文件路径在图里处于什么位置”

例如：

- `generated.c`
- `foo.o`
- `libfoo.a`
- `$INSTALL/lib/libfoo.a`

路径节点关心的是：

- 谁写了它
- 谁读了它
- 它最后属于什么角色

所以路径节点回答的问题是：

- 这个文件有没有进入业务传播链
- 它只是构建控制面噪声，还是会影响最终交付物

前面提到的：

- `propagating`
- `delivery`
- `tooling`
- `unknown`

都属于路径节点的角色。

#### 它们之间的关系

动作和路径不是并列两套无关的数据，它们通过读写关系连在一起：

```text
动作 --writes--> 路径 --read by--> 动作
```

例如：

```text
perl gen.pl --writes--> generated.c --read by--> cc -c
cc -c --writes--> generated.o --read by--> ar
ar --writes--> libfoo.a --read by--> cp
cp --writes--> $INSTALL/lib/libfoo.a
```

在这个例子里：

- `perl gen.pl`、`cc -c`、`ar`、`cp` 是动作节点
- `generated.c`、`generated.o`、`libfoo.a`、`$INSTALL/lib/libfoo.a` 是路径节点

#### 它们在算法中的作用分别是什么

动作节点主要负责：

- 建立动作身份
- 做 `baseline vs probe` 的动作匹配
- 判断某一步是关键语义动作，还是保守回退的 `generic`
- 提供 `paramTouches` 这类“同一动作槽位但参数变化”的证据

路径节点主要负责：

- 建立 writer -> reader 的传播边
- 判断某个变化有没有沿着文件链继续传播
- 把路径分成 `tooling / propagating / delivery / unknown`
- 为后面的碰撞检测提供路径交汇证据

可以把它粗略理解成：

- 动作节点解决“**发生了什么行为**”
- 路径节点解决“**影响沿着哪些文件传播**”

#### 为什么只看其中一类都不够

只看动作，不看路径，会丢掉传播关系。

例如你知道有一条 `compile` 动作变了，但不知道它写出的 `foo.o` 后面有没有进入 `archive` 或 `link`，那你仍然不知道这个变化是不是业务相关。

只看路径，不看动作，也不够。

例如你知道 `generated.c` 是 `propagating` 路径，只能说明：

- 这个文件进入了业务传播链

但这并不能回答：

- 写出 `generated.c` 的那一步到底是 `configure` 噪声、真实 `codegen`，还是完全看不懂的黑盒 `generic`

这也是为什么后续如果要引入 `kindCodegen`，它会是一个**动作节点标签**，而不是新的路径角色。

换句话说：

- `propagating` 说的是“这条路径重要”
- `kindCodegen` 说的是“这个动作不是普通黑盒，它是在生成后续编译要吃的输入”

### 步骤 5：建立文件传播图和路径角色

这是系统真正开始理解“影响链”的地方。

#### 文件传播图

规则非常直接：

- 如果动作 A 最后写了路径 `X`
- 后续动作 B 读了路径 `X`
- 就建立 `A -> B` 的传播边

当前实现是一个 **last-writer 图**：

- 每次 read 只连到最近一次 writer

这张图回答的是：

- 某个变化有没有沿着文件链传播到后续关键动作

#### 路径角色

系统不会把所有路径一视同仁，而是给路径分角色：

- `propagating`
- `delivery`
- `tooling`
- `unknown`

这些术语很重要：

- `propagating`
  - 真正承载业务传播的路径
  - 例如生成头、对象文件、归档文件
- `delivery`
  - 最终安装树中的交付路径
- `tooling`
  - 构建器内部控制面、探测链、自举链
- `unknown`
  - 证据不足，系统不敢强判

这个分类决定后面哪些路径会被拿来当碰撞证据。

### 步骤 6：对 baseline 和 probe 做差分，得到 `optionProfile`

对于每个单变量 probe，系统会拿 probe 图和 baseline 图做差分，最终产出一个 `optionProfile`。

`optionProfile` 可以理解成：

- “这个 option variant 在当前 require 组合下留下的行为签名”

它不是一句“变了”或“没变”，而是一组集合，主要包括：

- `propagatingReads`
- `propagatingWrites`
- `unknownReads`
- `unknownWrites`
- `deliveryWrites`
- `toolingReads`
- `toolingWrites`
- `paramTouches`

#### 为什么 `optionProfile` 需要这么多集合

因为系统最终要回答的不是：

- “这个 option 有没有变化”

而是：

- “这个 option 的变化落在哪里”
- “这些变化是不是和另一个 option 交汇”

#### `paramTouches` 是干什么的

`paramTouches` 是当前实现里非常关键的一层守卫。

它专门处理这种情况：

- 路径集合没变
- 但同一个关键动作槽位的参数变了

例如：

- baseline: `gcc -c core.c -o core.o`
- probe: `gcc -DUSE_TLS -c core.c -o core.o`

如果只看文件图，两边都还是：

- 读 `core.c`
- 写 `core.o`

看起来像“没什么变化”。

但实际上编译语义已经变了。

这时系统就会用共享 `actionKey` 上的 `fingerprint` 差异，把这个变化记录进 `paramTouches`。

#### refine 是什么

对 `configure/tooling` 这类动作，系统还会尝试做一层 `refine`：

- 在同一个 `actionKey` 组里，尽量把 baseline 和 probe 中“应该是一对”的动作配起来

但当前只在 **`1:1` 可证实时** 才做 refine。

如果出现 `N:M`，系统不会做贪婪拉链硬配，而是保守地退回 unmatched 路径。

代价是 profile 变粗；好处是错配风险小很多。

### 步骤 7：根据 `optionProfile` 建碰撞图并做缩减

最后，系统会检查两个 `optionProfile` 是否碰撞。

当前最主要的碰撞信号有两类：

1. 路径交汇  
例如两边都写了同一个 `propagating` 路径，或者都触碰了同一传播链。

2. `paramTouches` 交汇  
例如两边都改了同一个 `compile / link / archive` 槽位。

一旦两个 option key 碰撞，它们之间就会加一条边。

系统随后会：

1. 对碰撞图求连通分量
2. 只在每个碰撞分量内部做真实组合
3. 把真正正交的维度作为全局乘子扩回去

这就是矩阵缩减的核心算法。

## 5. `zeroDiff` 为什么是一个关键细节

`zeroDiff` 不是“某个 variant 没变化”，而是：

- 某个 option key 的所有已观察 variant 都是空差分

只有这种情况下，这个 key 才会被当成真正的正交乘子。

这样设计是为了避免一个非常危险的误判：

- 如果某个 key 有多个值，其中一个值空差分，另一个值有真实变化
- 那就绝不能因为“有一个空 diff”就把整个 key 踢出碰撞图

这也是为什么当前实现里，`zeroDiff` 只在**整组 variant 全空**时才成立。

## 6. `trusted` 到底在保护什么

`trusted` 不是“系统确信自己绝对正确”。

它表达的是：

- 这次 trace 和图结构足够干净，缩减结论可以作为正常信号使用

当前它由两层共同决定。

### 第一层：trace 是否可信

如果 parser diagnostics 出现这些问题，probe 会被降成 `untrusted`：

- `MissingPIDLines`
- `InvalidCalls`
- `ResumedMismatches`
- `UnrecognizedLines`

原因很简单：

- 输入证据已经脏了

### 第二层：图是否可信

即使 trace 解析成功，图本身也可能不可靠。

当前两类场景会降 `trusted`：

1. `unknown` 路径触达 business 子图
2. business 子图里出现了不可忽略的 `generic` 动作

这表示：

- 系统看到了变化
- 但没有足够语义去安全解释这些变化

这时继续做缩减容易过度自信，所以要明确降级。

## 7. 当前实现已经覆盖了什么

这套系统已经能稳定覆盖一批非常实际的场景：

- 主模块 `OnBuild` 内的大部分文件传播碰撞
- `compile / link / archive` 这类稳定动作的参数级守卫
- build root / install root / tmp root 变化引起的大量伪失配
- 并行构建下常见的 compile pipeline 交错 trace

也就是说，它已经不是一个只会跑 toy case 的概念验证。

## 8. 当前还没解决什么

这部分同样重要，因为它决定了这套系统的结论该如何使用。

### 1. 还不是 point-in-time digest

`compile` 生成输入的 digest 当前仍然是构建结束后的文件系统快照，而不是动作读取当时的快照。

因此，如果某个生成输入在构建中被后续步骤覆盖或删除：

- digest 可能偏离编译动作真正看到的内容

### 2. 只 trace 主模块

当前自动 probe 主要观察主模块 `OnBuild`。

如果 option 的关键影响主要体现在依赖模块构建中，这条链路不一定会被完整看到。

### 3. 还没有最终产物对比层

系统当前主要依赖：

- 文件传播图
- `paramTouches`

它还没有一层完整的最终交付物证据，例如：

- `OutputDir` manifest
- `.a/.so` 的语义级对比

所以它现在更像：

- 构建过程推断器

而不是：

- 最终产物验证器

### 4. symlink 还不是完整别名系统

`open` 类访问已经会尽量优先采用 `-yy` 返回的真实 fd 路径，但这仍然不是一张完整的 symlink alias 图。

某些命名空间操作里：

- 同一物理对象仍可能被表示成不同路径

### 5. `refine` 仍然只做保守的 `1:1`

当前 `configure/tooling` refine 不处理 `N:M`。

这不是遗漏，而是刻意保守：

- 不强行配对，减少错配
- 代价是 profile 粒度更粗

## 9. 应该怎样正确使用这套系统

最好的理解方式不是：

- “它已经完整理解了构建系统”

而是：

- 它已经能比较稳定地理解构建中的文件传播
- 能额外盯住少数关键动作的参数变化
- 并在自己看不清时主动降级为 `untrusted`

因此，当前最合理的使用方式是：

- 把它当成一个保守的自动缩减器
- `trusted=true` 时把结果当正常信号使用
- `trusted=false` 时把结果当提示，而不是保证

## 10. 如果你只记住三个名词

### `fingerprint`

动作的完整身份。  
回答的是：这两个动作是不是同一个具体实例。

### `actionKey`

动作的粗粒度槽位。  
回答的是：这两个动作是不是在改同一个逻辑位置。

### `optionProfile`

某个 option variant 的行为签名。  
回答的是：这个 option 到底把变化落到了哪里。

这三个概念合在一起，基本就是当前实现的主骨架：

- `fingerprint` 负责精确对齐
- `actionKey` 负责粗粒度守卫
- `optionProfile` 负责碰撞判定

## 11. 代码映射（附录）

如果已经理解上面的设计，再回头看代码会更容易。

### `internal/trace`

负责：

- 挂 `strace`
- 解析 syscall
- 产出 `Record` 和 `ParseDiagnostics`

### `internal/build`

负责：

- 在主模块构建期间包 trace
- 把 trace、scope、digest、diagnostics 打包成 `build.Result`

### `internal/evaluator`

负责：

- 归一化 `Record`
- 识别动作
- 合并 compile pipeline
- 建传播图
- 给路径分角色
- 生成 `optionProfile`
- 判断碰撞并缩减矩阵

### `cmd/llar/internal/test.go`

负责：

- `llar test --auto` 的入口
- 调 evaluator
- 输出 reduced combos 和 `trusted / untrusted`
