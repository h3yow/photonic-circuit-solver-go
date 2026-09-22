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
	// LossUnit 为迁移归一化后的波导损耗单位，取 "db_per_m" 或 "db_per_cm"。
	// 1.0 模型缺省为 "db_per_m"；1.1 模型由 loss_unit 字段显式给出。
	LossUnit string
	Devices  []Device
	Connects []Connection
}

// Device 描述一个器件实例；参数含义由 Kind 决定。
type Device struct {
	ID     string
	Kind   string
	Params map[string]float64
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
