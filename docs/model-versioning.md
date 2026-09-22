# 模型版本支持范围与演进规则

## 1. 支持范围

- 当前支持 **`1.0`** 与 **`1.1`**（`model.MinVersion`..`model.MaxVersion`）。
- 超出范围（更旧或更新）一律 `unsupported_version`，不尝试猜测语义。
- 解码使用 `DisallowUnknownFields`：任何未知字段（顶层或器件内）报
  `model_decode_error` 且消息指明未知字段，**不静默丢弃**。

## 2. 缺省语义（确定且跨版本稳定）

| 器件 | 缺失字段 | 稳定缺省 |
|------|----------|----------|
| 任意 | `modes` | `1`（单模） |
| waveguide | `phase` | `0` |
| beam_splitter | `theta` | `π/4`（50:50） |
| phase_shifter | `bias`, `offset` | `0` |
| phase_shifter | `gain` | `1`（指针字段；显式给 0 与缺失可区分，0/负按越界拒绝） |
| ring_cavity | `r`,`a`,`phi_round` | **无缺省，必填**（谐振物理不用默认值兜底） |

缺省值在 1.0 与 1.1 下完全相同（测试 `TestDefaults_StableAcrossVersions`
锁定），参数演进不会改变既有字段的缺省含义。

## 3. 演进规则

1. **新增字段**：在新版本中引入，旧版本文件读取时取上表的稳定缺省；新字段
   必须带确定缺省，否则该版本不被声明为兼容。
2. **缺失字段**：语义以本表为准，永不回退到“器件相关的隐式默认”。
3. **语义变更**：任何改变既有字段数值含义的改动都需要提升次版本号并扩展
   版本窗口；窗口外继续硬拒绝。
4. **未知字段/未知器件类型/未知参数演进**：分别以
   `model_decode_error` / `unknown_device_type` 明确报错，结论不会不确定。
5. 参数物理边界（θ 范围、|r|、a、gain 正值、有限性）在 `Defaults()` 阶段
   统一校验，错误分类为 `non_finite_input` 或 `out_of_physical_range`。

## 4. 模型示例

见 `examples/`。最小模型：

```json
{
  "version": "1.1",
  "devices": [
    {"id": "wg", "type": "waveguide", "params": {"phase": 0.5}}
  ],
  "connections": [],
  "external_ports": [
    {"device": "wg", "port": "in"},
    {"device": "wg", "port": "out"}
  ]
}
```

连接规则：每条 `connection` 必须把一个**输出角色端口**连到一个**输入角色
端口**；未连接又未在 `external_ports` 声明的端口按悬空拒绝。
