package solver

// ModelVersion 标识电路模型格式版本，如 "1.0"、"1.1"。
type ModelVersion string

// 器件类型常量。
const (
	KindWaveguide    = "waveguide"
	KindBeamsplitter = "beamsplitter"
	KindPhaseshifter = "phaseshifter"
	KindResonator    = "resonator"
)

// Model 是本地电路模型的内存表示（版本解析与迁移后得到）。
type Model struct {
	Version ModelVersion
	Circuit Circuit
}

// Circuit 描述电路拓扑与全局参数。
type Circuit struct {
	ID         string
	Wavelength float64 // 工作波长（微米），必须为正有限值
	// LossUnit 为迁移归一化后的电路级波导损耗单位，取 "db_per_m" 或 "db_per_cm"。
	// 1.0 模型缺省为 "db_per_m"；1.1 起由 loss_unit 字段显式给出；
	// 1.2 起器件可单独覆盖（见 Device.LossUnit）。
	LossUnit string
	Devices  []Device
	Connects []Connection
}

// UnitSource 记录一个物理量单位的声明来源（审计报告需要精确溯源）。
type UnitSource string

const (
	// UnitSourceDevice 单位由器件自己声明（1.2 起器件级 loss_unit）。
	UnitSourceDevice UnitSource = "device"
	// UnitSourceCircuit 单位由电路整体声明（circuit.loss_unit）。
	UnitSourceCircuit UnitSource = "circuit"
	// UnitSourceVersionDefault 单位由版本语义按确定规则补齐（未声明时缺省 db_per_m）。
	UnitSourceVersionDefault UnitSource = "version_default"
)

// Device 描述一个器件实例；参数含义由 Kind 决定。
type Device struct {
	ID     string
	Kind   string
	Params map[string]float64
	// LossUnit 为该器件归一化后的有效损耗单位（仅波导有意义，其余器件为空）。
	// 取值优先级：器件级声明 > 电路级声明 > 版本缺省。
	LossUnit string
	// LossUnitSource 记录 LossUnit 的声明来源。
	LossUnitSource UnitSource
}

// Connection 描述从一个端点到另一个端点的单向波导连接。
type Connection struct {
	From Endpoint
	To   Endpoint
}

// Endpoint 为器件端口（Device/Port 非空）或外部端口（External 非空）。
type Endpoint struct {
	Device   string
	Port     string
	External string
}

// IsExternal 报告该端点是否为电路外部端口。
func (ep Endpoint) IsExternal() bool { return ep.External != "" }
