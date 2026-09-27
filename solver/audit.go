package solver

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ValueSource 记录审计量取值的来源。
type ValueSource string

const (
	// SourceDeviceParam 取值来自器件自己声明的参数。
	SourceDeviceParam ValueSource = "device_param"
	// SourceDefault 取值来自确定的缺省规则（编译期登记，见 applyParamDefaults）。
	SourceDefault ValueSource = "default"
	// SourceDerived 取值由其它量按确定公式推导。
	SourceDerived ValueSource = "derived"
)

// AuditedQuantity 是一个器件的单个归一化物理量审计项。
// Available 为 false 时 Value/Unit 无效且 Reason 给出原因——
// 绝不用 0、1 或任意缺省值顶替，也不静默跳过。
type AuditedQuantity struct {
	Name      string      `json:"name"`
	Available bool        `json:"available"`
	Value     float64     `json:"value,omitempty"`
	Unit      string      `json:"unit,omitempty"`
	Source    ValueSource `json:"source,omitempty"`
	// UnitSource 记录单位的声明来源（仅带单位的量有意义）。
	UnitSource UnitSource `json:"unit_source,omitempty"`
	// Reason 为不可用时的原因（缺失、冲突或版本语义不确定）。
	Reason string `json:"reason,omitempty"`
}

// DeviceAudit 是单个器件的审计结果。
type DeviceAudit struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Quantities []AuditedQuantity `json:"quantities"`
}

// AuditReport 是整份电路的物理量审计报告；既可程序化处理，
// 也可通过 String 直接阅读。
type AuditReport struct {
	CircuitID  string        `json:"circuit_id"`
	Version    ModelVersion  `json:"version"`
	Wavelength float64       `json:"wavelength_um"`
	Devices    []DeviceAudit `json:"devices"`
}

// String 输出人类可读的审计报告文本。
func (r *AuditReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "circuit %q (model version %s), wavelength %s um\n",
		r.CircuitID, r.Version, strconv.FormatFloat(r.Wavelength, 'g', -1, 64))
	for _, d := range r.Devices {
		fmt.Fprintf(&b, "device %s (%s):\n", d.ID, d.Kind)
		for _, q := range d.Quantities {
			if !q.Available {
				fmt.Fprintf(&b, "  %s = UNAVAILABLE (%s)\n", q.Name, q.Reason)
				continue
			}
			line := fmt.Sprintf("  %s = %s", q.Name, strconv.FormatFloat(q.Value, 'g', -1, 64))
			if q.Unit != "" {
				line += " " + q.Unit
			}
			line += " [source:" + string(q.Source)
			if q.UnitSource != "" {
				line += ", unit:" + string(q.UnitSource)
			}
			line += "]"
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// 量名常量，供调用方程序化检索。
const (
	QLoss                 = "loss"
	QAttenuation          = "attenuation"
	QPhase                = "phase"
	QTransmission         = "transmission_amplitude"
	QTheta                = "theta"
	QPowerTransmission    = "power_transmission"
	QPowerReflectance     = "power_reflectance"
	QPhi0                 = "phi0"
	QPhaseSlope           = "k"
	QPhaseCurvature       = "gamma"
	QSelfCoupling         = "r_self"
	QCrossCoupling        = "r_cross"
	QRoundtripAttenuation = "a"
	QRoundtripPhase       = "phi_roundtrip"
)

// 单位常量（审计报告口径）。
const (
	unitDBPerM         = lossDBPerM
	unitNepers         = "nepers"
	unitRad            = "rad"
	unitDimensionless  = "dimensionless"
	unitRadPerControl  = "rad_per_control"
	unitRadPerControl2 = "rad_per_control_sq"
)

func available(name, unit string, value float64, src ValueSource) AuditedQuantity {
	return AuditedQuantity{Name: name, Available: true, Value: value, Unit: unit, Source: src}
}

func unavailable(name, reason string) AuditedQuantity {
	return AuditedQuantity{Name: name, Available: false, Reason: reason}
}

// paramQuantity 取器件声明参数：缺失时按 hasDefault/def 走确定缺省，
// 无缺省规则则标不可用；非有限值同样标不可用，绝不顶替。
func paramQuantity(d *Device, name, unit string, hasDefault bool, def float64) AuditedQuantity {
	v, ok := d.Params[name]
	if !ok {
		if hasDefault {
			return available(name, unit, def, SourceDefault)
		}
		return unavailable(name, "missing required parameter '"+name+"'")
	}
	if !isFinite(v) {
		return unavailable(name, "parameter '"+name+"' is non-finite")
	}
	return available(name, unit, v, SourceDeviceParam)
}

// auditModel 对迁移后的模型逐器件生成审计项（纯函数，不改动输入）。
func auditModel(m *Model) *AuditReport {
	rep := &AuditReport{
		CircuitID:  m.Circuit.ID,
		Version:    m.Version,
		Wavelength: m.Circuit.Wavelength,
	}
	for i := range m.Circuit.Devices {
		d := &m.Circuit.Devices[i]
		rep.Devices = append(rep.Devices, auditDevice(d, m.Circuit.Wavelength))
	}
	return rep
}

func auditDevice(d *Device, wavelength float64) DeviceAudit {
	da := DeviceAudit{ID: d.ID, Kind: d.Kind}
	switch d.Kind {
	case KindWaveguide:
		da.Quantities = auditWaveguide(d, wavelength)
	case KindPhaseshifter:
		da.Quantities = []AuditedQuantity{
			paramQuantity(d, QPhi0, unitRad, true, 0),
			paramQuantity(d, QPhaseSlope, unitRadPerControl, true, 1),
			paramQuantity(d, QPhaseCurvature, unitRadPerControl2, true, 0),
		}
	case KindBeamsplitter:
		da.Quantities = auditBeamsplitter(d)
	case KindResonator:
		da.Quantities = []AuditedQuantity{
			paramQuantity(d, QSelfCoupling, unitDimensionless, false, 0),
			paramQuantity(d, QCrossCoupling, unitDimensionless, false, 0),
			paramQuantity(d, QRoundtripAttenuation, unitDimensionless, false, 0),
			paramQuantity(d, QRoundtripPhase, unitRad, true, 0),
		}
	default:
		da.Quantities = []AuditedQuantity{
			unavailable("device", "unknown device kind '"+d.Kind+"'"),
		}
	}
	return da
}

// auditWaveguide 审计波导：损耗归一化为 db_per_m 并溯源单位声明，
// 派生量（衰减/相位/透射）依赖 length、neff 与工作波长。
func auditWaveguide(d *Device, wavelength float64) []AuditedQuantity {
	// 损耗：值来源为器件参数或缺省 0；单位来源单独溯源。
	lossQ := paramQuantity(d, QLoss, unitDBPerM, true, 0)
	if lossQ.Available {
		lossQ.Value = lossToDBPerM(lossQ.Value, d.LossUnit)
		lossQ.UnitSource = d.LossUnitSource
	}

	lengthQ := paramQuantity(d, "length", "um", false, 0)
	neffQ := paramQuantity(d, "neff", unitDimensionless, false, 0)

	derived := func(name, unit string, compute func(length, neff float64) float64) AuditedQuantity {
		if !lengthQ.Available {
			return unavailable(name, "cannot derive: "+lengthQ.Reason)
		}
		if !neffQ.Available && name == QPhase {
			return unavailable(name, "cannot derive: "+neffQ.Reason)
		}
		if !lossQ.Available && name != QPhase {
			return unavailable(name, "cannot derive: "+lossQ.Reason)
		}
		v := compute(lengthQ.Value, neffQ.Value)
		if !isFinite(v) {
			return unavailable(name, "derived value is non-finite")
		}
		return available(name, unit, v, SourceDerived)
	}

	alphaPerUM := func(lossDBPerM float64) float64 {
		return (math.Ln10 / 10.0) * lossDBPerM / 1e6
	}
	att := derived(QAttenuation, unitNepers, func(length, _ float64) float64 {
		return alphaPerUM(lossQ.Value) * length
	})
	phase := derived(QPhase, unitRad, func(length, neff float64) float64 {
		return 2 * math.Pi * neff * length / wavelength
	})
	trans := derived(QTransmission, unitDimensionless, func(length, _ float64) float64 {
		return math.Exp(-alphaPerUM(lossQ.Value) * length)
	})
	return []AuditedQuantity{lossQ, lengthQ, neffQ, att, phase, trans}
}

// auditBeamsplitter 审计分束器：theta 与 reflectivity 冲突时相关量标不可用。
func auditBeamsplitter(d *Device) []AuditedQuantity {
	thetaQ := paramQuantity(d, QTheta, unitRad, true, math.Pi/4)
	_, hasTheta := d.Params["theta"]
	rQ := paramQuantity(d, "reflectivity", unitDimensionless, false, 0)
	_, hasR := d.Params["reflectivity"]

	if hasTheta && hasR {
		conflict := "conflicting parameters: both 'theta' and 'reflectivity' declared"
		return []AuditedQuantity{
			unavailable(QTheta, conflict),
			unavailable(QPowerTransmission, conflict),
			unavailable(QPowerReflectance, conflict),
		}
	}
	if !hasTheta && hasR {
		// theta 由反射率推导；反射率越界则无法确定 theta。
		if !rQ.Available {
			thetaQ = unavailable(QTheta, "cannot derive: "+rQ.Reason)
		} else if rQ.Value < 0 || rQ.Value > 1 {
			thetaQ = unavailable(QTheta,
				"cannot derive: reflectivity out of range [0,1]")
		} else {
			thetaQ = available(QTheta, unitRad, math.Asin(math.Sqrt(rQ.Value)), SourceDerived)
		}
	}
	if !thetaQ.Available {
		return []AuditedQuantity{
			thetaQ,
			unavailable(QPowerTransmission, "cannot derive: "+thetaQ.Reason),
			unavailable(QPowerReflectance, "cannot derive: "+thetaQ.Reason),
		}
	}
	s := math.Sin(thetaQ.Value)
	c := math.Cos(thetaQ.Value)
	return []AuditedQuantity{
		thetaQ,
		available(QPowerTransmission, unitDimensionless, c*c, SourceDerived),
		available(QPowerReflectance, unitDimensionless, s*s, SourceDerived),
	}
}
