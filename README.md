# 本地光子电路仿真引擎 (photonic-circuit-solver-go)

一个纯 Go、无外部依赖的本地光子电路仿真引擎。电路由**波导、分束器、移相器、
全通环形谐振腔**构成；引擎在统一的光场/相位约定下，提供传播求解、相位标定/漂移
补偿、模型版本兼容与并发安全的缓存与去重。

## 包结构

| 包 | 职责 |
|----|------|
| `errors` | 可区分的错误分类（`Kind`），所有拒绝都带明确原因 |
| `model` | 器件/参数/拓扑的版本化模型，严格 JSON 解码、缺省语义、物理边界校验 |
| `topology` | 拓扑编译：缺端口、维度不匹配、悬空、重复占用、成环分别拒绝 |
| `solver` | 三条相互独立的传播求解路径与容差交叉校验 |
| `calibration` | 单调、可复现、收敛的相位标定与漂移补偿 |
| `engine` | 门面：不可变缓存、并发 single-flight 去重、规模/批量/迭代上限 |

## 快速开始

```go
eng, _ := engine.New(engine.DefaultConfig())
key, err := eng.Load(modelBytes)           // 解码 + 校验 + 编译 + 缓存
res, err := eng.SolveOne(key, engine.SolveRequest{
    Method:     solver.MethodLinear,
    CrossCheck: solver.MethodJacobi,        // 独立第二条路径交叉校验
    Request: solver.Request{
        Excitations: []solver.Excitation{{DeviceID: "b1", Port: "in1", Amplitude: 1}},
        Probes:      []solver.Probe{{DeviceID: "b2", Port: "out1"}},
    },
})
```

## 文档

- [光场与相位约定](docs/conventions.md)
- [容差设定](docs/tolerances.md)
- [模型版本支持范围与演进规则](docs/model-versioning.md)
- [验证方法](docs/verification.md)

## 错误分类（可程序化区分）

`errors.KindOf(err)` 返回下列类别之一，调用方据此区分拒绝原因：

`model_decode_error`、`unsupported_version`、`unknown_field`、
`unknown_device_type`、`missing_port`、`port_dimension_mismatch`、
`dangling_connection`、`port_already_connected`、`topology_cycle`、
`invalid_parameter`、`non_finite_input`、`out_of_physical_range`、
`solver_divergence`、`solver_iteration_limit`、`calibration_no_solution`、
`calibration_out_of_range`、`calibration_not_converged`、`limit_exceeded`。
