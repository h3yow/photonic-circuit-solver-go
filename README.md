# photonic-circuit-solver-go

本地光子电路仿真引擎：波导 / 分束器 / 移相器 / add-drop 谐振腔的
模型载入、拓扑校验、双途径传播求解、相位标定与漂移补偿，
以及物理量审计报告与跨版本等价核验。

## 快速开始

```go
eng, _ := solver.NewEngine()
circuit, err := eng.LoadModel(ctx, modelJSON)
res, err := eng.Solve(ctx, circuit, solver.SolveRequest{
    Batch:  []solver.Excitation{{Port: "in0", Amplitude: 1}},
    Method: solver.SolverCrossCheck, // 两条独立途径交叉校验（默认）
})

// 物理量审计：逐器件归一化量值 + 单位与取值来源
report, err := eng.Audit(ctx, circuit)

// 等价核验：两份模型文档（可跨版本、跨单位声明写法）
cmp, err := eng.CompareModels(ctx, oldJSON, newJSON, 0) // 0 = 默认容差 1e-9
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
  （电路级 `loss_unit`）与 `1.2`（器件级 `loss_unit`，未声明继承电路级）；
  迁移保持物理含义逐位不变；未知字段、未知单位、同一处重复声明严格拒绝，
  旧文档携带新字段报 `MODEL_MIGRATION`。
- **物理量审计**：逐器件列出归一化衰减/相位/透射量，标注单位与来源
  （电路级 / 器件级 / 版本缺省 / 推导）；算不出的器件标不可用并给原因，
  不用缺省值顶替、不悄悄跳过；失败时无半截报告。
- **等价核验**：两份文档归一化后逐器件对照；判等给出对照表与容差，
  判不等给出器件、物理量、两侧取值、差值与原因；版本组合语义不确定时
  整体拒绝。
- **并发安全**：`Engine` 只读、编译结果不可变、请求间零共享可变缓存，
  审计/对比/求解并发结论逐位一致。
- **上限可配**：器件 / 连接 / 批量 / 迭代次数超限即拒绝，无部分结果残留。

## 验证

```sh
go test ./... -count=1
go test -race ./... -count=1
```
