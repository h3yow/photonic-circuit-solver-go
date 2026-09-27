package solver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// 支持的模型版本范围。
const (
	versionOneDotZero   ModelVersion = "1.0"
	versionOneDotOne    ModelVersion = "1.1"
	versionOneDotTwo    ModelVersion = "1.2"
	currentModelVersion              = versionOneDotTwo
)

// lossUnit 取值常量。
const (
	lossDBPerM  = "db_per_m"
	lossDBPerCm = "db_per_cm"
)

// wireDevice 是模型 JSON 的器件表示；参数以原始 JSON 保留，
// 以便逐字段严格解码并拒绝未知参数，而不是静默丢弃。
type wireDevice struct {
	ID     string                     `json:"id"`
	Kind   string                     `json:"kind"`
	Params map[string]json.RawMessage `json:"params,omitempty"`
	// LossUnit 为 1.2 引入的器件级损耗单位声明；更早版本携带即拒绝。
	LossUnit *string `json:"loss_unit,omitempty"`
}

// wireEndpoint 要求三选一：{"external": "in0"} 或 {"device":"d1","port":"in"}。
type wireEndpoint struct {
	Device   string `json:"device,omitempty"`
	Port     string `json:"port,omitempty"`
	External string `json:"external,omitempty"`
}

type wireConnection struct {
	From wireEndpoint `json:"from"`
	To   wireEndpoint `json:"to"`
}

type wireCircuit struct {
	ID         string           `json:"id"`
	Wavelength *float64         `json:"wavelength,omitempty"`
	LossUnit   *string          `json:"loss_unit,omitempty"`
	Devices    []wireDevice     `json:"devices"`
	Connects   []wireConnection `json:"connects"`
}

type wireModel struct {
	Version string      `json:"version"`
	Circuit wireCircuit `json:"circuit"`
}

// 各器件类型允许的参数集合。新增字段必须在此显式登记并给出缺省语义。
var allowedParams = map[string]map[string]struct{}{
	KindWaveguide:    {"length": {}, "loss": {}, "neff": {}},
	KindBeamsplitter: {"theta": {}, "reflectivity": {}},
	KindPhaseshifter: {"u0": {}, "k": {}, "gamma": {}, "umax": {}, "phi0": {}},
	KindResonator:    {"r_self": {}, "r_cross": {}, "a": {}, "phi_roundtrip": {}},
}

// parseModel 从 JSON 字节严格解析模型：
//   - 未知顶级/嵌套字段 => ErrUnknownField；
//   - 版本不在支持范围 => ErrVersionUnsupported；
//   - 旧版本携带新版本字段（演进无法确定）=> ErrModelMigration；
//   - 旧版本缺失字段按确定缺省补齐（见 migrate）。
func parseModel(data []byte, limits Limits) (*Model, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}

	if err := rejectDuplicateKeys(data); err != nil {
		return nil, err
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	// DisallowUnknownFields 对任意层级（含 params 内层手动解码）生效。
	dec.DisallowUnknownFields()
	var wm wireModel
	if err := dec.Decode(&wm); err != nil {
		return nil, classifyJSONError(err)
	}
	if dec.More() {
		return nil, newError(ErrUnknownField, "trailing data after model document")
	}

	v := ModelVersion(wm.Version)
	switch v {
	case versionOneDotZero, versionOneDotOne, versionOneDotTwo:
	default:
		return nil, newError(ErrVersionUnsupported, "model version %q is outside supported range [%s,%s]",
			wm.Version, versionOneDotZero, currentModelVersion).
			withDetail("version", wm.Version).
			withDetail("supported", []string{string(versionOneDotZero), string(versionOneDotOne), string(currentModelVersion)})
	}

	if err := validateMigration(v, wm); err != nil {
		return nil, err
	}
	m, err := migrate(v, wm)
	if err != nil {
		return nil, err
	}

	if len(m.Circuit.Devices) > limits.MaxDevices {
		return nil, newError(ErrLimitExceeded, "device count %d exceeds limit %d",
			len(m.Circuit.Devices), limits.MaxDevices).
			withDetail("count", len(m.Circuit.Devices)).
			withDetail("limit", limits.MaxDevices)
	}
	if len(m.Circuit.Connects) > limits.MaxConnections {
		return nil, newError(ErrLimitExceeded, "connection count %d exceeds limit %d",
			len(m.Circuit.Connects), limits.MaxConnections).
			withDetail("count", len(m.Circuit.Connects)).
			withDetail("limit", limits.MaxConnections)
	}
	return m, nil
}

// validateMigration 拒绝跨版本的语义不确定演进：
//   - 旧版（1.0）模型不得携带仅在 1.1 才有定义的 circuit.loss_unit；
//   - 1.0/1.1 模型不得携带仅在 1.2 才有定义的器件级 loss_unit。
func validateMigration(v ModelVersion, wm wireModel) error {
	wc := wm.Circuit
	if v == versionOneDotZero && wc.LossUnit != nil {
		return newError(ErrModelMigration,
			"model declares version 1.0 but carries 1.1-only field 'loss_unit': semantics cannot be inferred").
			withDetail("field", "circuit.loss_unit").
			withDetail("declared_version", "1.0").
			withDetail("introduced_in", "1.1")
	}
	if v == versionOneDotZero || v == versionOneDotOne {
		for _, wd := range wc.Devices {
			if wd.LossUnit != nil {
				return newError(ErrModelMigration,
					"model declares version %s but device %q carries 1.2-only field 'loss_unit': semantics cannot be inferred",
					v, wd.ID).
					withDetail("field", "device.loss_unit").
					withDetail("device", wd.ID).
					withDetail("declared_version", string(v)).
					withDetail("introduced_in", "1.2")
			}
		}
	}
	return nil
}

// migrate 将任意受支持版本的线表示迁移为内部统一 Model（等价于当前版本语义）。
func migrate(v ModelVersion, wm wireModel) (*Model, error) {
	wc := wm.Circuit

	m := &Model{Version: currentModelVersion, DeclaredVersion: v}
	m.Circuit.ID = wc.ID

	// 缺省语义 1：wavelength 在 1.0/1.1/1.2 均为必填，缺失即拒绝，不做默认值兜底。
	if wc.Wavelength == nil {
		return nil, newError(ErrInvalidParameter, "circuit.wavelength is required").
			withDetail("field", "circuit.wavelength")
	}
	m.Circuit.Wavelength = *wc.Wavelength

	// 缺省语义 2：loss_unit 仅 1.1 起可显式给出；1.0 模型缺省为 db_per_m。
	switch {
	case wc.LossUnit != nil:
		unit, err := checkLossUnit(*wc.LossUnit)
		if err != nil {
			return nil, err
		}
		m.Circuit.LossUnit = unit
		m.Circuit.LossUnitSource = SourceCircuitDeclared
	default:
		m.Circuit.LossUnit = lossDBPerM
		m.Circuit.LossUnitSource = SourceVersionDefault
	}

	for _, wd := range wc.Devices {
		d := Device{ID: wd.ID, Kind: wd.Kind, Params: map[string]float64{}}
		allowed, knownKind := allowedParams[wd.Kind]
		if !knownKind {
			return nil, newError(ErrInvalidParameter, "device %q has unknown kind %q", wd.ID, wd.Kind).
				withDetail("device", wd.ID).withDetail("kind", wd.Kind)
		}
		// 器件级损耗单位（1.2）：缺省继承电路级取值及其来源。
		d.LossUnit = m.Circuit.LossUnit
		d.LossUnitSource = m.Circuit.LossUnitSource
		if wd.LossUnit != nil {
			if wd.Kind != KindWaveguide {
				return nil, newError(ErrInvalidParameter,
					"device %q (%s) declares loss_unit but kind %q has no loss quantity: conflicting declarations",
					wd.ID, wd.Kind, wd.Kind).
					withDetail("device", wd.ID).
					withDetail("kind", wd.Kind).
					withDetail("field", "device.loss_unit")
			}
			unit, err := checkLossUnit(*wd.LossUnit)
			if err != nil {
				return nil, err
			}
			d.LossUnit = unit
			d.LossUnitSource = SourceDeviceDeclared
		}
		for name, raw := range wd.Params {
			if _, ok := allowed[name]; !ok {
				return nil, newError(ErrUnknownField, "device %q (%s) has unknown parameter %q",
					wd.ID, wd.Kind, name).
					withDetail("device", wd.ID).
					withDetail("kind", wd.Kind).
					withDetail("parameter", name)
			}
			var val float64
			if err := json.Unmarshal(raw, &val); err != nil {
				return nil, newError(ErrInvalidParameter, "parameter %s of device %q must be a number",
					name, wd.ID).
					withDetail("device", wd.ID).withDetail("parameter", name)
			}
			d.Params[name] = val
		}
		m.Circuit.Devices = append(m.Circuit.Devices, d)
	}

	for _, wcn := range wc.Connects {
		cn, err := convertConnection(wcn)
		if err != nil {
			return nil, err
		}
		m.Circuit.Connects = append(m.Circuit.Connects, cn)
	}

	_ = v // 各受支持版本迁移目标一致；版本参数用于上面的演进校验与 DeclaredVersion
	return m, nil
}

// checkLossUnit 校验损耗单位写法；未知写法一律拒绝，不做猜测。
func checkLossUnit(u string) (string, error) {
	switch u {
	case lossDBPerM, lossDBPerCm:
		return u, nil
	default:
		return "", newError(ErrInvalidParameter, "unknown loss_unit %q", u).
			withDetail("loss_unit", u)
	}
}

// rejectDuplicateKeys 在严格解码前扫描整个文档，
// 拒绝同一对象内出现重复键（同一处相互冲突的声明）。
// encoding/json 对重复键默认“后者覆盖前者”，会静默丢失冲突证据，因此显式拒绝。
func rejectDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	// frame 记录一个对象/数组的解析状态；对象帧用 expectKey 区分键与值。
	type frame struct {
		isObj     bool
		expectKey bool
		seen      map[string]struct{}
	}
	var stack []frame
	// valueConsumed 在标量值被消费后恢复父对象帧的 expectKey。
	valueConsumed := func() {
		if len(stack) > 0 && stack[len(stack)-1].isObj {
			stack[len(stack)-1].expectKey = true
		}
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return classifyJSONError(err)
		}
		// 对象帧期待键时，token 必为键字符串或 '}'。
		if len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.isObj && top.expectKey {
				if key, ok := tok.(string); ok {
					if _, dup := top.seen[key]; dup {
						return newError(ErrInvalidParameter,
							"duplicate key %q in the same object: conflicting declarations cannot be resolved", key).
							withDetail("key", key)
					}
					top.seen[key] = struct{}{}
					top.expectKey = false
					continue
				}
			}
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{':
				stack = append(stack, frame{isObj: true, expectKey: true, seen: map[string]struct{}{}})
			case '[':
				stack = append(stack, frame{})
			case '}', ']':
				stack = stack[:len(stack)-1]
				valueConsumed()
			}
			continue
		}
		valueConsumed()
	}
}

func convertConnection(wc wireConnection) (Connection, error) {
	from, err := convertEndpoint("from", wc.From)
	if err != nil {
		return Connection{}, err
	}
	to, err := convertEndpoint("to", wc.To)
	if err != nil {
		return Connection{}, err
	}
	if from == to {
		return Connection{}, newError(ErrDanglingConnection, "connection from an endpoint to itself is invalid").
			withDetail("endpoint", endpointLabel(from))
	}
	return Connection{From: from, To: to}, nil
}

func convertEndpoint(side string, we wireEndpoint) (Endpoint, error) {
	hasExt := we.External != ""
	hasDev := we.Device != "" || we.Port != ""
	if hasExt == hasDev {
		return Endpoint{}, newError(ErrInvalidParameter,
			"connection %s endpoint must be exactly one of {external} or {device,port}", side).
			withDetail("side", side)
	}
	if hasDev {
		if we.Device == "" || we.Port == "" {
			return Endpoint{}, newError(ErrPortMissing,
				"device endpoint requires both device and port on %s side", side).
				withDetail("device", we.Device).withDetail("port", we.Port)
		}
	}
	return Endpoint{Device: we.Device, Port: we.Port, External: we.External}, nil
}

func endpointLabel(ep Endpoint) string {
	if ep.IsExternal() {
		return "external:" + ep.External
	}
	return fmt.Sprintf("%s.%s", ep.Device, ep.Port)
}

// classifyJSONError 将 JSON 层错误归类到稳定错误码。
func classifyJSONError(err error) error {
	if ute, ok := err.(*json.UnmarshalTypeError); ok {
		return wrapError(ErrInvalidParameter, err,
			"field %s has wrong JSON type", ute.Field)
	}
	if se, ok := err.(*json.SyntaxError); ok {
		return wrapError(ErrInvalidParameter, err,
			"invalid JSON syntax at offset %d", se.Offset)
	}
	// encoding/json 对未知字段的报错形如：json: unknown field "xxx"
	if bytes.Contains([]byte(err.Error()), []byte("unknown field")) {
		return wrapError(ErrUnknownField, err, "strict decoding rejected document")
	}
	return wrapError(ErrInvalidParameter, err, "failed to decode model")
}
