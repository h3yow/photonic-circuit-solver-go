package solver

import (
	"math"
	"math/cmplx"
)

// 物理量审计报告：对已载入（或仅解析）的电路逐个器件列出归一化后的
// 衰减、相位、传播/透射等量，并标注每个数值的单位与取值来源。
// 报告类型均带 JSON 标签，既可人读也可被程序继续处理。

// 审计中使用的单位写法。
const (
	unitMicrometer = "um"
	unitRadian     = "rad"
	unitUnitless   = "unitless"
	unitDBPerM     = lossDBPerM
)

// Quantity 是一个归一化后的物理量及其来源说明。
type Quantity struct {
	Name   string     `json:"name"`
	Value  float64    `json:"value"`
	Unit   string     `json:"unit"`
	Source UnitSource `json:"source"`
	// UnitSource 仅对带有单位声明的量（波导损耗）有意义：
	// 说明单位是电路整体声明的、器件自己声明的，还是版本缺省补齐的。
	UnitSource UnitSource `json:"unit_source,omitempty"`
}

// DeviceAudit 是单个器件的审计条目。
// Available 为 false 时 Quantities 为空，Reason 给出不可用原因；
// 不可用条目不会被悄悄跳过，也不会用 0/1 等缺省值顶替。
type DeviceAudit struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Available  bool       `json:"available"`
	Reason     string     `json:"reason,omitempty"`
	Quantities []Quantity `json:"quantities,omitempty"`
}

// AuditReport 是整个电路的物理量审计报告。
type AuditReport struct {
	CircuitID             string        `json:"circuit_id"`
	ModelVersion          ModelVersion  `json:"model_version"`
	DeclaredVersion       ModelVersion  `json:"declared_version"`
	Wavelength            Quantity      `json:"wavelength"`
	CircuitLossUnit       string        `json:"circuit_loss_unit"`
	CircuitLossUnitSource UnitSource    `json:"circuit_loss_unit_source"`
	Devices               []DeviceAudit `json:"devices"`
}

// auditModel 从（已迁移的）模型构建审计报告。
// 每个器件独立判定可用性：参数缺失、冲突或非法时该器件标为不可用并给出原因，
// 不影响其他器件的审计；文档级问题（版本、迁移、未知字段）在进入本函数前已被拒绝。
func auditModel(m *Model) *AuditReport {
	rep := &AuditReport{
		CircuitID:             m.Circuit.ID,
		ModelVersion:          m.Version,
		DeclaredVersion:       m.DeclaredVersion,
		CircuitLossUnit:       m.Circuit.LossUnit,
		CircuitLossUnitSource: m.Circuit.LossUnitSource,
		Wavelength: Quantity{
			Name:   "wavelength",
			Value:  m.Circuit.Wavelength,
			Unit:   unitMicrometer,
			Source: SourceCircuitDeclared,
		},
	}
	for _, d := range m.Circuit.Devices {
		rep.Devices = append(rep.Devices, auditDevice(m, d))
	}
	return rep
}

// auditDevice 审计单个器件；任何参数问题都使该器件整体不可用并记录原因。
func auditDevice(m *Model, d Device) DeviceAudit {
	da := DeviceAudit{ID: d.ID, Kind: d.Kind}
	if err := validateDeviceParams(d, m.Circuit.LossUnit); err != nil {
		da.Available = false
		da.Reason = err.Error()
		return da
	}
	da.Available = true
	switch d.Kind {
	case KindWaveguide:
		da.Quantities = auditWaveguide(m, d)
	case KindPhaseshifter:
		da.Quantities = auditPhaseshifter(d)
	case KindBeamsplitter:
		da.Quantities = auditBeamsplitter(d)
	case KindResonator:
		da.Quantities = auditResonator(d)
	}
	return da
}

// paramSource 报告参数的取值来源：器件显式声明或确定缺省规则补齐。
func paramSource(d Device, name string) UnitSource {
	if _, ok := d.Params[name]; ok {
		return SourceDeviceParam
	}
	return SourceDefaultRule
}

// auditWaveguide 列出波导的归一化损耗、衰减幅值与传播相位。
func auditWaveguide(m *Model, d Device) []Quantity {
	length := d.Params["length"]
	neff := d.Params["neff"]
	rawLoss, hasLoss := d.Params["loss"]
	lossSrc := SourceDeviceParam
	if !hasLoss {
		rawLoss = 0
		lossSrc = SourceDefaultRule
	}
	// 归一化到内部统一单位 dB/m（与 compile 中的换算规则一致）。
	lossDBM := lossToDBPerM(rawLoss, d.LossUnit)
	alphaPerUM := (math.Ln10 / 10.0) * (lossDBM / 1e6)
	attenuation := math.Exp(-alphaPerUM * length)
	phase := 2 * math.Pi * neff * length / m.Circuit.Wavelength
	return []Quantity{
		{Name: "length", Value: length, Unit: unitMicrometer, Source: paramSource(d, "length")},
		{Name: "neff", Value: neff, Unit: unitUnitless, Source: paramSource(d, "neff")},
		{Name: "loss", Value: lossDBM, Unit: unitDBPerM, Source: lossSrc, UnitSource: d.LossUnitSource},
		{Name: "attenuation", Value: attenuation, Unit: unitUnitless, Source: SourceDerived},
		{Name: "phase", Value: phase, Unit: unitRadian, Source: SourceDerived},
	}
}

// auditPhaseshifter 列出移相器的静态相位与控制模型系数。
func auditPhaseshifter(d Device) []Quantity {
	get := func(name, unit string, def float64) Quantity {
		v, ok := d.Params[name]
		if !ok {
			v = def
		}
		return Quantity{Name: name, Value: v, Unit: unit, Source: paramSource(d, name)}
	}
	return []Quantity{
		get("phi0", unitRadian, 0),
		get("u0", unitUnitless, 0),
		get("k", unitUnitless, 1),
		get("gamma", unitUnitless, 0),
	}
}

// auditBeamsplitter 列出分束器的归一化角与反射率（二者互为推导）。
func auditBeamsplitter(d Device) []Quantity {
	theta, hasTheta := d.Params["theta"]
	r, hasR := d.Params["reflectivity"]
	var thetaSrc, rSrc UnitSource
	switch {
	case hasTheta:
		thetaSrc, rSrc = SourceDeviceParam, SourceDerived
		r = math.Sin(theta) * math.Sin(theta)
	case hasR:
		thetaSrc, rSrc = SourceDerived, SourceDeviceParam
		theta = math.Asin(math.Sqrt(r))
	default:
		theta, r = math.Pi/4, 0.5
		thetaSrc, rSrc = SourceDefaultRule, SourceDerived
	}
	return []Quantity{
		{Name: "theta", Value: theta, Unit: unitRadian, Source: thetaSrc},
		{Name: "reflectivity", Value: r, Unit: unitUnitless, Source: rSrc},
	}
}

// auditResonator 列出谐振腔参数与解析透射幅值。
func auditResonator(d Device) []Quantity {
	r1 := d.Params["r_self"]
	r2 := d.Params["r_cross"]
	a := d.Params["a"]
	phi := d.Params["phi_roundtrip"] // 缺省 0（确定缺省规则）
	ejp := cmplx.Exp(complex(0, phi))
	k12 := math.Sqrt((1 - r1*r1) * (1 - r2*r2))
	denom := complex(1, 0) - complex(r1*r2*a, 0)*ejp
	sTh := (complex(r1, 0) - complex(r2*a, 0)*ejp) / denom
	sDr := complex(-k12*a, 0) / denom
	return []Quantity{
		{Name: "r_self", Value: r1, Unit: unitUnitless, Source: paramSource(d, "r_self")},
		{Name: "r_cross", Value: r2, Unit: unitUnitless, Source: paramSource(d, "r_cross")},
		{Name: "a", Value: a, Unit: unitUnitless, Source: paramSource(d, "a")},
		{Name: "phi_roundtrip", Value: phi, Unit: unitRadian, Source: paramSource(d, "phi_roundtrip")},
		{Name: "through_amplitude", Value: cmplx.Abs(sTh), Unit: unitUnitless, Source: SourceDerived},
		{Name: "drop_amplitude", Value: cmplx.Abs(sDr), Unit: unitUnitless, Source: SourceDerived},
	}
}
