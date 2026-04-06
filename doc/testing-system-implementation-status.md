# LLAR 测试系统实现状态说明

这份文档用于说明测试系统当前已经落地到什么程度，以及哪些问题仍未闭合。  
它与设计稿分离，避免将阶段性实现状态混入系统设计本身。

## 1. 已实现能力

当前版本已经具备：

- `onTest` 作为 Formula 的验证入口
- `llar test`
- `llar test --auto`
- `build-only` probe
- 命令级 trace 记录
- baseline 与单变量 probe
- evaluator 内部的最小动作图建模
- 基于碰撞连通分量的矩阵缩减

## 2. 当前行为

### 2.1 `llar test`

- 使用默认组合执行 `build + onTest`

### 2.2 `llar test --auto`

- 读取矩阵
- 生成 baseline 与单变量 probe
- 对 probe 组合执行 `build-only` 观察
- evaluator 计算需要真正测试的组合
- 对这些组合执行 `build + onTest`

## 3. 当前限制

当前版本仍存在以下限制：

- 自动模式尚未覆盖所有调用场景
- 设计上的 `--full` 语义尚未落地，普通模式还没有明确区分 default options 与全矩阵
- trace 的 lineage 模型还比较保守
- evaluator 不输出独立的分析报告
- build cache 与 test 语义尚未分离，`onTest` 仍然挂在 build 流程尾部
- 当前通过绕过 build cache 避免 `onTest` 被跳过，但这只是暂时实现，不是最终设计

## 4. 尚未落地的能力

以下能力仍未实现：

- artifact manifest
- 可审计分析输出
- 测试结果缓存
- 构建缓存与测试状态分离
- 更强的认证模型
- 云端交付闭环
