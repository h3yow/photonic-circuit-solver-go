# 容差设定

## 1. 传播求解交叉校验

`solver.Config`（缺省见 `DefaultConfig`）：

| 参数 | 缺省值 | 含义 |
|------|--------|------|
| `AbsTol` | `1e-10` | 两独立方法逐探测端复振幅差绝对上界 |
| `RelTol` | `1e-9` | 随振幅尺度缩放的相对上界 |
| `MaxIterations` | `10000` | Jacobi 迭代上限 |

交叉判据（逐探测端）：

```
|z_primary − z_secondary|  ≤  AbsTol + RelTol · |z_primary|
```

超出即报 `solver_divergence`，消息含两种方法各自的数值、差值与容差；
**绝不静默二选一**。`Solve` 可单独选择方法，`SolveCrossChecked` 强制两条
独立路径一致才返回。

## 2. 三条独立路径

- `linear`：组装全局线性方程组 `(I − S·P)x = s`（按端口角色直接列出
  固定行/连接行），部分主元高斯消元。
- `topological_sweep`：在 Kahn 拓扑序上对每个器件精确求值一次（DAG 下单遍
  即精确），与全局矩阵相互独立。
- `jacobi`：对入射波向量做定点迭代，残差按
  `‖Δx‖∞ ≤ AbsTol + RelTol·‖x‖₂` 判收敛；达上限不收敛报
  `solver_iteration_limit`。

对无源 DAG，Jacobi 几何收敛；三条路径在 MZI、环形腔等电路上结果一致
（见测试，复振幅差 < 1e-9，功率守恒误差 < 1e-9）。

## 3. 物理守恒校验

- 总输出功率 ≤ 总输入功率 + `1e-8`（数值松弛），否则 `solver_divergence`。
- 任何探测端产生非有限值 → `non_finite_input`。

## 4. 标定容差

`calibration.Config`：

| 参数 | 缺省值 | 含义 |
|------|--------|------|
| `PhaseTol` | `1e-8` rad | 成功时 `|arg(probe)−arg(ref) − target|` 上界 |
| `MaxIterations` | `200` | 二分/补偿迭代上限 |
| `MinAmplitude` | `1e-7` | probe/reference 最小振幅，低于视为暗点（相位无定义） |
| `MonotonicitySlack` | `1e-9` | 允许的微小数值非单调松弛 |

- 相位映射在 64 点网格上铺到连续分支并逐点核验单调性，违反报
  `calibration_not_converged`。
- 暗点（振幅低于 `MinAmplitude`）相位无定义 → `calibration_no_solution`。
- 二分达到 `PhaseTol` 才返回 offset，否则 `calibration_not_converged`，
  绝不返回未收敛/发散相位。
- 漂移补偿用数值灵敏度做 Newton 校正并**重新探测验证**残差达到
  `PhaseTol`；灵敏度近零报 `calibration_no_solution`，校正越界报
  `calibration_out_of_range`。
