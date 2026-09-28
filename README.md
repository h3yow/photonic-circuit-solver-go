# photonic-circuit-solver-go

本地光子电路仿真引擎：波导 / 分束器 / 移相器 / add-drop 谐振腔的
模型载入、拓扑校验、双途径传播求解、相位标定与漂移补偿，
以及模型体检（物理量审计与跨版本等价核验）。

## 快速开始

```go
eng, _ := solver.NewEngine()
circuit, err := eng.LoadModel(ctx, modelJSON)
res, err := eng.Solve(ctx, circuit, solver.SolveRequest{
    Batch:  []solver.Excitation{{Port: "in0", Amplitude: 1}},
    Method: solver.SolverCrossCheck, // 两条独立途径交叉校验（默认）
})
```

示例模型见 `examples/mzi.json`，完整约定见 [`docs/overview.md`](docs/overview.md)。

## 设计要点

- **非法拓扑不靠默认值兜底**：端口缺失 / 维度不匹配 / 悬空 / 有向成环 /
  重复 ID 各有独立错误码（`PORT_MISSING`、`PORT_DIMENSION_MISMATCH`、
  `DANGLING_CONNECTION`、`TOPOLOGY_CYCLE` …）。
- **两种独立求解途径**：拓扑顺序传播 vs 全局波变量高斯消元；超差明确报
  `SOLVER_DIVERGENCE`，携带输入与双方结果，不静默取其一。
- **物理边界强制**：非有限 / 越界激励与控制量拒绝，不截断不归一化。
- **标定收敛可复现**：中心化二次最小二乘 + 单调可逆校验 + 确定性二分反演；
  无解 / 超范围 / 不收敛分类拒绝。
- **版本兼容确定**：支持 `1.0`（缺省 `loss_unit=db_per_m` 迁移）、`1.1`
  （电路级 `loss_unit`）与 `1.2`（器件级 `loss_unit` 覆盖电路级）；未知字段
  严格拒绝，旧文档携带新字段报 `MODEL_MIGRATION`，重复键等冲突声明报
  `CONFLICTING_DECLARATION`。
- **物理量审计**：`Audit`/`AuditDocument` 逐器件给出归一化物理量、单位与
  取值来源（器件声明 / 电路声明 / 版本缺省）；算不出的量明确标不可用并给
  原因，不用缺省值顶替，失败不返回半截报告。
- **跨版本等价核验**：`CompareModels` 对两份文档逐器件归一化对照（容差
  `Limits.EquivalenceTolerance`，默认 1e-9）；不等时逐项指出器件、物理量、
  双值、差值与原因，报告始终可直接 JSON 序列化（证据字段不缺项、无 NaN/
  Inf），且与文档传入顺序无关（正序反序逐位一致）。物理范围外取值
  （如分束器 `reflectivity=30` 这种按百分比误填）与 theta/reflectivity
  双重声明等语义不可确定的输入整体拒绝（错误点名器件与物理量、报告为
  nil），不硬凑半截报告；核验为纯函数，不改动已载入电路。
- **并发安全**：`Engine` 只读、编译结果不可变、请求间零共享可变缓存。
- **上限可配**：器件 / 连接 / 批量 / 迭代次数超限即拒绝，无部分结果残留。

## 验证

```sh
go test ./... -count=1
go test -race ./... -count=1
```
