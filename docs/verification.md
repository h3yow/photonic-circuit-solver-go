# 验证方法

## 1. 运行测试

```bash
go vet ./...
go test -race -count=1 ./...
```

竞态检测必须干净通过（共享缓存为不可变值；每次请求/每次标定探测都构建
私有快照与 solver，不共享可变状态）。

## 2. 测试矩阵

### 非法拓扑（错误须分类正确且可区分）
`topology/topology_test.go::TestCompile_TopologyErrorsAreDistinguishable`
- 引用不存在的器件 / 不存在的端口 → `missing_port`
- 端口悬空（未连接且未声明外部）→ `dangling_connection`
- 端口重复连接 / 外部端口又被连接 → `port_already_connected`
- 输入-输入相连 → `invalid_parameter`
- 多模原语（modes=2）→ `port_dimension_mismatch`
- 有向成环 / 自环 → `topology_cycle`
- 断言失败时打印输入 JSON 与实际/期望错误类别；错误消息点名具体端口。

### 传播一致性与物理性（`solver/solver_test.go`）
- 三条独立路径（linear / topological_sweep / jacobi）在 MZI 上逐探测端
  复振幅差 < 1e-9。
- 多种激励下总功率守恒误差 < 1e-9。
- 环形腔传输与闭式解析解吻合（< 1e-12）。
- 交叉校验对超容差差异报 `solver_divergence`（含两方法数值），容差内通过。
- 非有限、超功率、输出端口加激励、内部端口加激励、缺器件等分别归类。
- Jacobi 迭代上限 → `solver_iteration_limit`。

### 标定失败（`calibration/calibration_test.go`）
- 裸移相器在 [−π, π] 上目标 −3..2.9 全部收敛，残差 ≤ 1e-7 且可复现
  （重复运行 offset 完全相等）。
- 目标超出可达区间 → `calibration_out_of_range`。
- MZI 暗点附近相位无定义 → `calibration_no_solution`（或可达范围拒绝）。
- 标定目标器件缺失 → `missing_port`；目标器件非移相器 →
  `invalid_parameter`。
- 空激励、probe==reference、NaN 目标、反向区间 → 各自分类。
- 迭代上限被压缩时 → `calibration_not_converged`，不返回发散相位。
- 漂移补偿能把 +0.25 rad 漂移拉回目标（残差 ≤ 1e-7）；非有限观测被拒绝。

### 版本兼容（`model`、`engine` 测试）
- 1.0/1.1 可读且缺省一致；0.9/2.0/空版本 → `unsupported_version`。
- 未知字段不被静默丢弃 → `model_decode_error`。

### 并发与资源（`engine/engine_test.go`，`-race`）
- 64 路并发求解同一电路，结果字符串逐路一致（无数值漂移、无半更新）。
- 32 路并发同一标定共享 single-flight，返回同一记录；后续调用直接复用
  不可变记录，不重复标定。
- 并发 offset 覆盖求解不污染共享缓存（基线结果不被改写）。
- 批量超限：`reject` → `limit_exceeded` 且**不返回部分结果**；`degrade`
  分块返回全部结果。
- 电路规模超限 → `limit_exceeded`；未知 limit policy → `invalid_parameter`。

## 3. 手算复核（MZI，50:50）

单臂相移 φ、另一臂 0：`P_out1 = sin²(φ/2)`，`P_out2 = cos²(φ/2)`，
`P_out1 + P_out2 = 1`。环形腔在 φ_round=0 时
`t = (r−a)/(1−ar)`（实数），可直接对照测试中的解析断言。
