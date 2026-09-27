package solver

import (
	"math"
	"sort"
	"strconv"
)

// portDir 标识器件端口的传播方向。
type portDir int

const (
	dirIn portDir = iota + 1
	dirOut
)

// portRef 用器件序号与端口名定位一个端口。
type portRef struct {
	dev  int
	port string
}

// compiledDevice 是校验后的器件：有序输入/输出端口与数值参数。
type compiledDevice struct {
	idx      int
	id       string
	kind     string
	inPorts  []string
	outPorts []string
	params   map[string]float64
}

// compiledConn 是校验后的连接；*External 为空表示对应侧为器件端口。
type compiledConn struct {
	fromDev      int // -1 表示外部输入
	fromPort     string
	fromExternal string
	toDev        int // -1 表示外部输出
	toPort       string
	toExternal   string
}

// CompiledCircuit 是模型通过全部拓扑/参数校验后的不可变编译结果。
// 它不持有任何跨请求可变缓存：求解所需的临时场变量均在请求内分配。
type CompiledCircuit struct {
	model  *Model
	limits Limits

	devices    []compiledDevice
	deviceByID map[string]int

	// 拓扑连接与波变量编号。
	conns        []compiledConn
	internalOf   map[portRef]int    // 器件输出端口 -> 内部波变量序号
	incomingEdge map[portRef]int    // 器件输入端口 <- 内部波变量序号
	inputEdges   map[portRef]string // 器件输入端口 <- 外部输入名
	outputEdges  map[portRef]string // 器件输出端口 -> 外部输出名

	externalInputs  []string // 稳定顺序（首次出现序）
	externalOutputs []string
	inputUseCount   map[string]int // 外部输入被连接占用次数（合法值为 1）
	outputUseCount  map[string]int // 外部输出被连接占用次数（合法值为 1）

	// order 为器件的拓扑序（Kahn），供拓扑求解器使用。
	order []int

	// edgeSrc/edgeDst 为内部波变量 e 的源输出端口与目标输入端口。
	edgeSrc []portRef
	edgeDst []portRef

	// testTopologicalScale 仅供测试：对拓扑求解器的每个输出乘以该因子，
	// 用于确定性地制造两途径超差以验证分歧报错路径。生产路径不设置（零值即按 1 处理）。
	testTopologicalScale complex128
}

// Model 返回编译所依据的模型（内部版本语义）。
func (c *CompiledCircuit) Model() *Model { return c.model }

// ExternalInputs 返回已声明的外部输入端口（稳定顺序）。
func (c *CompiledCircuit) ExternalInputs() []string {
	out := make([]string, len(c.externalInputs))
	copy(out, c.externalInputs)
	return out
}

// ExternalOutputs 返回已声明的外部输出端口（稳定顺序）。
func (c *CompiledCircuit) ExternalOutputs() []string {
	out := make([]string, len(c.externalOutputs))
	copy(out, c.externalOutputs)
	return out
}

// 端口表：每种器件的有序输入/输出端口。
var portTable = map[string]struct{ in, out []string }{
	KindWaveguide:    {in: []string{"in"}, out: []string{"out"}},
	KindPhaseshifter: {in: []string{"in"}, out: []string{"out"}},
	KindBeamsplitter: {in: []string{"a", "b"}, out: []string{"c", "d"}},
	KindResonator:    {in: []string{"in", "add"}, out: []string{"through", "drop"}},
}

// compile 对模型执行标识、参数、端口、维度、悬空与成环校验，
// 生成不可变编译结果。任何非法情形都以分类错误拒绝，不做默认值兜底。
func compile(m *Model, limits Limits) (*CompiledCircuit, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, newError(ErrInvalidParameter, "nil model")
	}
	c := &CompiledCircuit{
		model:          m,
		limits:         limits,
		deviceByID:     map[string]int{},
		internalOf:     map[portRef]int{},
		incomingEdge:   map[portRef]int{},
		inputEdges:     map[portRef]string{},
		outputEdges:    map[portRef]string{},
		inputUseCount:  map[string]int{},
		outputUseCount: map[string]int{},
	}

	if len(m.Circuit.Devices) > limits.MaxDevices {
		return nil, newError(ErrLimitExceeded, "device count %d exceeds limit %d",
			len(m.Circuit.Devices), limits.MaxDevices)
	}
	if len(m.Circuit.Connects) > limits.MaxConnections {
		return nil, newError(ErrLimitExceeded, "connection count %d exceeds limit %d",
			len(m.Circuit.Connects), limits.MaxConnections)
	}

	if err := validateCircuitParams(&m.Circuit); err != nil {
		return nil, err
	}
	if err := c.buildDevices(m); err != nil {
		return nil, err
	}
	if err := c.buildConnections(m); err != nil {
		return nil, err
	}
	if err := c.checkFullConnection(); err != nil {
		return nil, err
	}
	if err := c.topologicalSort(); err != nil {
		return nil, err
	}
	return c, nil
}

func isFinite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func validateCircuitParams(ci *Circuit) error {
	if !isFinite(ci.Wavelength) || ci.Wavelength <= 0 {
		return newError(ErrInvalidParameter, "circuit.wavelength must be a finite positive number (micrometers)").
			withDetail("wavelength", ci.Wavelength)
	}
	seen := map[string]struct{}{}
	for _, d := range ci.Devices {
		if d.ID == "" {
			return newError(ErrInvalidParameter, "device id must not be empty")
		}
		if _, dup := seen[d.ID]; dup {
			return newError(ErrDuplicateID, "duplicated device id %q", d.ID).
				withDetail("device", d.ID)
		}
		seen[d.ID] = struct{}{}
		if err := validateDeviceParams(d, ci.LossUnit); err != nil {
			return err
		}
	}
	return nil
}

func requireParam(d Device, name string) (float64, error) {
	v, ok := d.Params[name]
	if !ok {
		return 0, newError(ErrInvalidParameter, "device %q (%s) missing required parameter %q",
			d.ID, d.Kind, name).
			withDetail("device", d.ID).withDetail("parameter", name)
	}
	if !isFinite(v) {
		return 0, newError(ErrNonFiniteInput, "parameter %q of device %q must be finite", name, d.ID).
			withDetail("device", d.ID).withDetail("parameter", name)
	}
	return v, nil
}

func optionalParam(d Device, name string, def float64) (float64, error) {
	v, ok := d.Params[name]
	if !ok {
		return def, nil
	}
	if !isFinite(v) {
		return 0, newError(ErrNonFiniteInput, "parameter %q of device %q must be finite", name, d.ID).
			withDetail("device", d.ID).withDetail("parameter", name)
	}
	return v, nil
}

func validateDeviceParams(d Device, lossUnit string) error {
	checkRange := func(name string, v, lo, hi float64, loInc, hiInc bool) error {
		okLo := v > lo || loInc && v == lo
		okHi := v < hi || hiInc && v == hi
		if !okLo || !okHi {
			return newError(ErrInvalidParameter,
				"parameter %q of device %q out of physical range: %v not in %v",
				name, d.ID, v, rangeLabel(lo, hi, loInc, hiInc)).
				withDetail("device", d.ID).withDetail("parameter", name).
				withDetail("value", v)
		}
		return nil
	}
	switch d.Kind {
	case KindWaveguide:
		length, err := requireParam(d, "length")
		if err != nil {
			return err
		}
		if err := checkRange("length", length, 0, math.Inf(1), true, false); err != nil {
			return err
		}
		neff, err := requireParam(d, "neff")
		if err != nil {
			return err
		}
		if err := checkRange("neff", neff, 0, math.Inf(1), false, false); err != nil {
			return err
		}
		loss, err := optionalParam(d, "loss", 0)
		if err != nil {
			return err
		}
		if err := checkRange("loss", loss, 0, math.Inf(1), true, false); err != nil {
			return err
		}
		_ = lossUnit // 单位在建模期已归一化字段，仅作文档性引用
	case KindBeamsplitter:
		_, hasTheta := d.Params["theta"]
		_, hasR := d.Params["reflectivity"]
		if hasTheta && hasR {
			return newError(ErrInvalidParameter,
				"device %q must not specify both theta and reflectivity (ambiguous definition)", d.ID).
				withDetail("device", d.ID)
		}
		if hasTheta {
			theta := d.Params["theta"]
			if !isFinite(theta) {
				return newError(ErrNonFiniteInput, "theta of device %q must be finite", d.ID).
					withDetail("device", d.ID)
			}
		}
		if hasR {
			r := d.Params["reflectivity"]
			if !isFinite(r) {
				return newError(ErrNonFiniteInput, "reflectivity of device %q must be finite", d.ID).
					withDetail("device", d.ID)
			}
			if err := checkRange("reflectivity", r, 0, 1, true, true); err != nil {
				return err
			}
		}
	case KindPhaseshifter:
		for _, p := range []string{"u0", "k", "gamma", "umax", "phi0"} {
			v, ok := d.Params[p]
			if !ok {
				continue
			}
			if !isFinite(v) {
				return newError(ErrNonFiniteInput, "parameter %q of device %q must be finite", p, d.ID).
					withDetail("device", d.ID).withDetail("parameter", p)
			}
			if p == "umax" && v <= 0 {
				return newError(ErrInvalidParameter, "umax of device %q must be strictly positive", d.ID).
					withDetail("device", d.ID).withDetail("value", v)
			}
		}
	case KindResonator:
		r1, err := requireParam(d, "r_self")
		if err != nil {
			return err
		}
		if err := checkRange("r_self", r1, 0, 1, false, false); err != nil {
			return err
		}
		r2, err := requireParam(d, "r_cross")
		if err != nil {
			return err
		}
		if err := checkRange("r_cross", r2, 0, 1, false, false); err != nil {
			return err
		}
		a, err := requireParam(d, "a")
		if err != nil {
			return err
		}
		if err := checkRange("a", a, 0, 1, false, true); err != nil {
			return err
		}
		phi, err := optionalParam(d, "phi_roundtrip", 0)
		if err != nil {
			return err
		}
		// 拒绝接近奇点（分母过小将导致非有限/非物理放大）。
		denom := complex(1, 0) - complex(r1*r2*a, 0)*complex(math.Cos(phi), math.Sin(phi))
		if minMag := 1e-12; abs(denom) < minMag {
			return newError(ErrInvalidParameter,
				"resonator %q is too close to its singular pole (|1-r_self*r_cross*a*e^{iφ}|=%g < %g)",
				d.ID, abs(denom), minMag).
				withDetail("device", d.ID).
				withDetail("denominator_magnitude", abs(denom))
		}
	default:
		// kind 已在建模期校验，此处不可达
		return newError(ErrInvalidParameter, "device %q has unknown kind %q", d.ID, d.Kind).
			withDetail("device", d.ID).withDetail("kind", d.Kind)
	}
	return nil
}

func rangeLabel(lo, hi float64, loInc, hiInc bool) string {
	l, h := "(", ")"
	if loInc {
		l = "["
	}
	if hiInc {
		h = "]"
	}
	los := formatNum(lo)
	his := formatNum(hi)
	return l + los + "," + his + h
}

func formatNum(x float64) string {
	if math.IsInf(x, 1) {
		return "+Inf"
	}
	if math.IsInf(x, -1) {
		return "-Inf"
	}
	return strconv.FormatFloat(x, 'g', -1, 64)
}

func abs(z complex128) float64 { return math.Hypot(real(z), imag(z)) }

func (c *CompiledCircuit) buildDevices(m *Model) error {
	for i, d := range m.Circuit.Devices {
		tab := portTable[d.Kind]
		// 复制参数并登记确定的缺省语义（未显式给出时），
		// 缺省只在编译期发生一次，求解期不再做隐式回退。
		params := map[string]float64{}
		for k, v := range d.Params {
			params[k] = v
		}
		applyParamDefaults(d.Kind, params)
		// 损耗单位归一化：内部统一按 dB/m 存储与求解（见 device.go）。
		// 器件级单位（1.2）优先，未声明的继承电路级单位；
		// 归一化只在编译期发生一次，求解期不再做隐式换算。
		if d.Kind == KindWaveguide {
			params["loss"] = lossToDBPerM(params["loss"], d.LossUnit)
		}
		cd := compiledDevice{
			idx:      i,
			id:       d.ID,
			kind:     d.Kind,
			inPorts:  append([]string(nil), tab.in...),
			outPorts: append([]string(nil), tab.out...),
			params:   params,
		}
		c.devices = append(c.devices, cd)
		c.deviceByID[d.ID] = i
	}
	return nil
}

// lossToDBPerM 把声明单位下的损耗值换算为内部统一的 dB/m。
// 空单位（程序化构造的 Model）按 db_per_m 处理，不做换算。
func lossToDBPerM(v float64, unit string) float64 {
	if unit == lossDBPerCm {
		return v * 100 // 1 dB/cm = 100 dB/m
	}
	return v
}

// applyParamDefaults 为各器件类型登记稳定缺省值。
func applyParamDefaults(kind string, p map[string]float64) {
	set := func(k string, v float64) {
		if _, ok := p[k]; !ok {
			p[k] = v
		}
	}
	switch kind {
	case KindWaveguide:
		set("loss", 0)
	case KindBeamsplitter:
		// theta 与 reflectivity 同时缺省时为 50:50（theta=π/4）。
		if _, t := p["theta"]; !t {
			if _, r := p["reflectivity"]; !r {
				p["theta"] = math.Pi / 4
			}
		}
	case KindPhaseshifter:
		set("u0", 0)
		set("k", 1)
		set("gamma", 0)
		set("phi0", 0)
	case KindResonator:
		set("phi_roundtrip", 0)
	}
}

// lookupPort 校验端口存在性并返回其方向。
func (c *CompiledCircuit) lookupPort(devID, port string) (int, portDir, bool) {
	idx, ok := c.deviceByID[devID]
	if !ok {
		return 0, 0, false
	}
	for _, p := range c.devices[idx].inPorts {
		if p == port {
			return idx, dirIn, true
		}
	}
	for _, p := range c.devices[idx].outPorts {
		if p == port {
			return idx, dirOut, true
		}
	}
	return idx, 0, false
}

func (c *CompiledCircuit) buildConnections(m *Model) error {
	inSeen := map[string]struct{}{}
	outSeen := map[string]struct{}{}
	nEdges := 0
	addInput := func(name string) {
		if _, ok := inSeen[name]; !ok {
			inSeen[name] = struct{}{}
			c.externalInputs = append(c.externalInputs, name)
		}
	}
	addOutput := func(name string) {
		if _, ok := outSeen[name]; !ok {
			outSeen[name] = struct{}{}
			c.externalOutputs = append(c.externalOutputs, name)
		}
	}

	for i, cn := range m.Circuit.Connects {
		cc := compiledConn{fromDev: -1, toDev: -1}

		// ---- from 侧：必须是外部输入或器件输出端口 ----
		if cn.From.IsExternal() {
			cc.fromExternal = cn.From.External
		} else {
			idx, dir, exists := c.lookupPort(cn.From.Device, cn.From.Port)
			if !exists {
				if _, devExists := c.deviceByID[cn.From.Device]; !devExists {
					return newError(ErrPortMissing, "connection %d from-side references unknown device %q",
						i, cn.From.Device).
						withDetail("connection", i).
						withDetail("device", cn.From.Device).
						withDetail("port", cn.From.Port)
				}
				return newError(ErrPortMissing, "connection %d from-side references unknown port %q on device %q",
					i, cn.From.Port, cn.From.Device).
					withDetail("connection", i).
					withDetail("device", cn.From.Device).
					withDetail("port", cn.From.Port)
			}
			if dir != dirOut {
				return newError(ErrPortDimensionMismatch,
					"connection %d from-side attaches to input port %s.%s (only output ports may emit)",
					i, cn.From.Device, cn.From.Port).
					withDetail("connection", i).
					withDetail("endpoint", endpointLabel(cn.From))
			}
			cc.fromDev, cc.fromPort = idx, cn.From.Port
		}

		// ---- to 侧：必须是外部输出或器件输入端口 ----
		if cn.To.IsExternal() {
			cc.toExternal = cn.To.External
		} else {
			idx, dir, exists := c.lookupPort(cn.To.Device, cn.To.Port)
			if !exists {
				if _, devExists := c.deviceByID[cn.To.Device]; !devExists {
					return newError(ErrPortMissing, "connection %d to-side references unknown device %q",
						i, cn.To.Device).
						withDetail("connection", i).
						withDetail("device", cn.To.Device).
						withDetail("port", cn.To.Port)
				}
				return newError(ErrPortMissing, "connection %d to-side references unknown port %q on device %q",
					i, cn.To.Port, cn.To.Device).
					withDetail("connection", i).
					withDetail("device", cn.To.Device).
					withDetail("port", cn.To.Port)
			}
			if dir != dirIn {
				return newError(ErrPortDimensionMismatch,
					"connection %d to-side attaches to output port %s.%s (only input ports may receive)",
					i, cn.To.Device, cn.To.Port).
					withDetail("connection", i).
					withDetail("endpoint", endpointLabel(cn.To))
			}
			cc.toDev, cc.toPort = idx, cn.To.Port
		}

		// 外部输入只能出现在 from 侧，外部输出只能出现在 to 侧。
		if cc.fromExternal != "" && cc.toExternal != "" {
			return newError(ErrPortDimensionMismatch,
				"connection %d links two external endpoints (%s -> %s)", i,
				cc.fromExternal, cc.toExternal).
				withDetail("connection", i)
		}

		// 端口占用登记：同一物理端口被多条连接占用 => 维度不匹配（单模端口不可扇出/扇入）。
		if cc.fromDev >= 0 {
			ref := portRef{cc.fromDev, cc.fromPort}
			if _, busy := c.internalOf[ref]; busy {
				return portBusyError(i, cn.From, "from")
			}
			if _, busy := c.outputEdges[ref]; busy {
				return portBusyError(i, cn.From, "from")
			}
		}
		if cc.toDev >= 0 {
			ref := portRef{cc.toDev, cc.toPort}
			if _, busy := c.incomingEdge[ref]; busy {
				return portBusyError(i, cn.To, "to")
			}
			if _, busy := c.inputEdges[ref]; busy {
				return portBusyError(i, cn.To, "to")
			}
		}

		edgeIdx := -1
		switch {
		case cc.fromDev >= 0 && cc.toDev >= 0:
			edgeIdx = nEdges // 内部波变量独立编号
			nEdges++
		case cc.fromExternal != "" && cc.toDev >= 0:
			if c.inputUseCount[cc.fromExternal] >= 1 {
				return externalBusyError(i, cc.fromExternal, "input")
			}
			c.inputUseCount[cc.fromExternal]++
			c.inputEdges[portRef{cc.toDev, cc.toPort}] = cc.fromExternal
			addInput(cc.fromExternal)
		case cc.fromDev >= 0 && cc.toExternal != "":
			if c.outputUseCount[cc.toExternal] >= 1 {
				return externalBusyError(i, cc.toExternal, "output")
			}
			c.outputUseCount[cc.toExternal]++
			c.outputEdges[portRef{cc.fromDev, cc.fromPort}] = cc.toExternal
			addOutput(cc.toExternal)
		}

		if cc.fromDev >= 0 && cc.toDev >= 0 {
			c.internalOf[portRef{cc.fromDev, cc.fromPort}] = edgeIdx
			c.incomingEdge[portRef{cc.toDev, cc.toPort}] = edgeIdx
			c.edgeSrc = append(c.edgeSrc, portRef{cc.fromDev, cc.fromPort})
			c.edgeDst = append(c.edgeDst, portRef{cc.toDev, cc.toPort})
		}
		c.conns = append(c.conns, cc)
	}
	return nil
}

func portBusyError(connIdx int, ep Endpoint, side string) error {
	return newError(ErrPortDimensionMismatch,
		"connection %d %s-side port %s is already occupied (single-mode ports cannot fan-out/fan-in)",
		connIdx, side, endpointLabel(ep)).
		withDetail("connection", connIdx).
		withDetail("endpoint", endpointLabel(ep))
}

func externalBusyError(connIdx int, name, kind string) error {
	return newError(ErrPortDimensionMismatch,
		"connection %d reuses external %s port %q (external ports cannot fan-out/fan-in)",
		connIdx, kind, name).
		withDetail("connection", connIdx).
		withDetail("external", name)
}

// checkFullConnection 检查每个器件端口都恰好接入一条连接；
// 未接入的器件端口属于悬空连接。
func (c *CompiledCircuit) checkFullConnection() error {
	for _, d := range c.devices {
		for _, p := range d.inPorts {
			ref := portRef{d.idx, p}
			if _, ok := c.incomingEdge[ref]; !ok {
				if _, ok2 := c.inputEdges[ref]; !ok2 {
					return newError(ErrDanglingConnection,
						"input port %s.%s is not connected to anything", d.id, p).
						withDetail("device", d.id).withDetail("port", p)
				}
			}
		}
		for _, p := range d.outPorts {
			ref := portRef{d.idx, p}
			if _, ok := c.internalOf[ref]; !ok {
				if _, ok2 := c.outputEdges[ref]; !ok2 {
					return newError(ErrDanglingConnection,
						"output port %s.%s is not connected to anything", d.id, p).
						withDetail("device", d.id).withDetail("port", p)
				}
			}
		}
	}
	// 外部端口名不得同时作为输入与输出出现。
	inSet := map[string]struct{}{}
	for _, n := range c.externalInputs {
		inSet[n] = struct{}{}
	}
	for _, n := range c.externalOutputs {
		if _, clash := inSet[n]; clash {
			return newError(ErrPortDimensionMismatch,
				"external port %q is declared both as input and output", n).
				withDetail("external", n)
		}
	}
	return nil
}

// topologicalSort 用 Kahn 算法对器件图排序；存在有向环则明确拒绝，
// 并给出环节点作为判定依据。
func (c *CompiledCircuit) topologicalSort() error {
	n := len(c.devices)
	indeg := make([]int, n)
	adj := make([][]int, n)
	for _, cc := range c.conns {
		if cc.fromDev >= 0 && cc.toDev >= 0 {
			adj[cc.fromDev] = append(adj[cc.fromDev], cc.toDev)
			indeg[cc.toDev]++
		}
	}
	// 稳定序：按器件序号入队。
	var queue []int
	for i := 0; i < n; i++ {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	c.order = c.order[:0]
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		c.order = append(c.order, v)
		nexts := append([]int(nil), adj[v]...)
		sort.Ints(nexts)
		for _, w := range nexts {
			indeg[w]--
			if indeg[w] == 0 {
				queue = append(queue, w)
			}
		}
	}
	if len(c.order) != n {
		var cyclic []string
		for i := 0; i < n; i++ {
			if indeg[i] > 0 {
				cyclic = append(cyclic, c.devices[i].id)
			}
		}
		sort.Strings(cyclic)
		return newError(ErrTopologyCycle,
			"device connection graph contains a directed cycle involving %v", cyclic).
			withDetail("devices", cyclic)
	}
	return nil
}
