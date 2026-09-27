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

// UnitSource 记录一个物理量（或其单位）的取值来源，
// 供审计报告说明每个数值是电路整体声明的、器件自己声明的，
// 还是旧版本按确定规则补齐的。
type UnitSource string

const (
	// SourceCircuitDeclared 电路级 loss_unit 显式声明。
	SourceCircuitDeclared UnitSource = "circuit_declared"
	// SourceDeviceDeclared 器件级 loss_unit 显式声明（1.2 起）。
	SourceDeviceDeclared UnitSource = "device_declared"
	// SourceVersionDefault 旧版本未声明，按确定迁移规则补齐。
	SourceVersionDefault UnitSource = "version_default"
	// SourceDeviceParam 数值由器件 params 显式给出。
	SourceDeviceParam UnitSource = "device_param"
	// SourceDefaultRule 数值缺失，按器件类型的确定缺省规则补齐。
	SourceDefaultRule UnitSource = "default_rule"
	// SourceDerived 数值由其他量推导（如归一化、解析散射）。
	SourceDerived UnitSource = "derived"
)

// Model 是本地电路模型的内存表示（版本解析与迁移后得到）。
type Model struct {
	// Version 为迁移后的内部统一版本（等价于当前版本语义）。
	Version ModelVersion
	// DeclaredVersion 为文档原始声明的版本，供审计报告说明来源。
	DeclaredVersion ModelVersion
	Circuit         Circuit
}

// Circuit 描述电路拓扑与全局参数。
type Circuit struct {
	ID         string
	Wavelength float64 // 工作波长（微米），必须为正有限值
	// LossUnit 为迁移归一化后的波导损耗单位，取 "db_per_m" 或 "db_per_cm"。
	// 1.0 模型缺省为 "db_per_m"；1.1 起由 loss_unit 字段显式给出。
	LossUnit string
	// LossUnitSource 记录电路级损耗单位的来源（显式声明或版本缺省）。
	LossUnitSource UnitSource
	Devices        []Device
	Connects       []Connection
}

// Device 描述一个器件实例；参数含义由 Kind 决定。
type Device struct {
	ID     string
	Kind   string
	Params map[string]float64
	// LossUnit 为该器件解析后的损耗单位（仅 waveguide 有物理意义）：
	// 1.2 起可由器件级 loss_unit 单独声明，否则继承电路级取值。
	LossUnit string
	// LossUnitSource 记录该器件损耗单位的来源。
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
