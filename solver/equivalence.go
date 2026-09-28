package solver

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// QuantityComparison 是同一器件同一物理量在两份文档中的归一化对照。
type QuantityComparison struct {
	Name            string  `json:"name"`
	Unit            string  `json:"unit,omitempty"`
	A               float64 `json:"a"`
	B               float64 `json:"b"`
	Diff            float64 `json:"diff"`
	WithinTolerance bool    `json:"within_tolerance"`
}

// DeviceComparison 是同一器件（按 ID 匹配）的逐量对照。
type DeviceComparison struct {
	DeviceID   string               `json:"device_id"`
	Kind       string               `json:"kind"`
	Quantities []QuantityComparison `json:"quantities"`
}

// Mismatch 描述一处不等价判定依据：哪个器件、哪个量、两边各是什么、
// 差多少、为什么判为不等。Numeric 为 false 时 A/B/Diff 在 JSON 中显式为
// null（结构性差异，如器件缺失、类型不同、连接不同），原因在 Reason 中。
// Numeric 为 true 时 A/B/Diff 必定逐项给出（即便差值为 0 也不缺项），
// 且全部为有限值，可直接 JSON 序列化交给下游。
type Mismatch struct {
	DeviceID string   `json:"device_id,omitempty"`
	Quantity string   `json:"quantity,omitempty"`
	Numeric  bool     `json:"numeric"`
	A        *float64 `json:"a"`
	B        *float64 `json:"b"`
	Diff     *float64 `json:"diff"`
	Reason   string   `json:"reason"`
}

// EquivalenceReport 是跨版本/跨声明等价核验结果。
// Equal 为 true 时 Devices 给出逐器件归一化对照与所用容差；
// 为 false 时 Mismatches 给出全部判定依据。
type EquivalenceReport struct {
	Equal      bool               `json:"equal"`
	Tolerance  float64            `json:"tolerance"`
	Devices    []DeviceComparison `json:"devices"`
	Mismatches []Mismatch         `json:"mismatches,omitempty"`
}

// String 输出人类可读的核验结论。
func (r *EquivalenceReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "equal=%v (tolerance %s)\n", r.Equal,
		strconv.FormatFloat(r.Tolerance, 'g', -1, 64))
	for _, d := range r.Devices {
		fmt.Fprintf(&b, "device %s (%s):\n", d.DeviceID, d.Kind)
		for _, q := range d.Quantities {
			fmt.Fprintf(&b, "  %s: A=%s B=%s diff=%s within_tol=%v\n",
				q.Name,
				strconv.FormatFloat(q.A, 'g', -1, 64),
				strconv.FormatFloat(q.B, 'g', -1, 64),
				strconv.FormatFloat(q.Diff, 'g', -1, 64),
				q.WithinTolerance)
		}
	}
	for _, m := range r.Mismatches {
		if m.Numeric {
			fmt.Fprintf(&b, "MISMATCH device=%q quantity=%q A=%s B=%s diff=%s: %s\n",
				m.DeviceID, m.Quantity,
				strconv.FormatFloat(*m.A, 'g', -1, 64),
				strconv.FormatFloat(*m.B, 'g', -1, 64),
				strconv.FormatFloat(*m.Diff, 'g', -1, 64),
				m.Reason)
		} else {
			fmt.Fprintf(&b, "MISMATCH device=%q quantity=%q: %s\n",
				m.DeviceID, m.Quantity, m.Reason)
		}
	}
	return b.String()
}

// paramUnit 给出比较口径下各参数的归一化单位（损耗已统一为 db_per_m）。
func paramUnit(name string) string {
	switch name {
	case "length":
		return "um"
	case "loss":
		return unitDBPerM
	case "theta", "phi0", "phi_roundtrip":
		return unitRad
	case "k":
		return unitRadPerControl
	case "gamma":
		return unitRadPerControl2
	default:
		return unitDimensionless
	}
}

// effectiveParams 返回器件用于等价比较的归一化参数：
// 登记确定缺省（与编译期同一套规则），波导损耗统一折算为 db_per_m，
// 分束器 reflectivity 统一折算为 theta（与求解器语义一致，
// 使 theta 与 reflectivity 两种写法可按物理含义比较）。
// 调用方必须已通过 validateDeviceParams：theta 与 reflectivity 不会同时出现，
// reflectivity 必在 [0,1] 内，因此 Asin(Sqrt(r)) 一定是有限值。
func effectiveParams(d *Device) map[string]float64 {
	p := map[string]float64{}
	for k, v := range d.Params {
		p[k] = v
	}
	applyParamDefaults(d.Kind, p)
	if d.Kind == KindWaveguide {
		p["loss"] = lossToDBPerM(p["loss"], d.LossUnit)
	}
	if d.Kind == KindBeamsplitter {
		if r, ok := p["reflectivity"]; ok {
			p["theta"] = math.Asin(math.Sqrt(r))
			delete(p, "reflectivity")
		}
	}
	return p
}

// compareModels 对两个迁移后的模型做等价核验（纯函数，不改动输入）。
// 判定式与求解交叉校验一致：|a-b| <= tol*max(1,|a|,|b|)。
// 进入报告的任何数值若不是有限值（含差值相减溢出），返回错误而不是产出
// 无法序列化、语义不可判定的报告；调用方保证返回错误时不交出报告。
func compareModels(a, b *Model, tol float64) (*EquivalenceReport, error) {
	if !isFinite(tol) || tol <= 0 {
		return nil, newError(ErrInvalidParameter, "equivalence tolerance must be a finite positive number")
	}
	rep := &EquivalenceReport{Equal: true, Tolerance: tol}
	fail := func(m Mismatch) {
		rep.Equal = false
		rep.Mismatches = append(rep.Mismatches, m)
	}

	// circuit 级与器件级数值型判等依据都走同一构造：
	// A/B/Diff 逐项齐全（指针永不为 nil），保证序列化不缺项。
	numericMismatch := func(deviceID, quantity string, a, b float64, reason string) Mismatch {
		diff := abs64(a - b)
		return Mismatch{
			DeviceID: deviceID,
			Quantity: quantity,
			Numeric:  true,
			A:        &a,
			B:        &b,
			Diff:     &diff,
			Reason:   reason,
		}
	}

	// finitePair 保证一对归一化取值及其差值都可序列化；
	// 任一非有限则等价语义无法判定，整体拒绝。
	finitePair := func(deviceID, quantity string, a, b float64) error {
		diff := a - b
		if !isFinite(a) || !isFinite(b) || !isFinite(diff) {
			return newError(ErrNonFiniteInput,
				"equivalence undecidable: normalized %q of device %q produced a non-finite comparison value",
				quantity, deviceID).
				withDetail("device", deviceID).
				withDetail("parameter", quantity).
				withDetail("a", a).
				withDetail("b", b)
		}
		return nil
	}

	// 电路级：工作波长。
	if !withinTol(a.Circuit.Wavelength, b.Circuit.Wavelength, tol) {
		if err := finitePair("", "wavelength", a.Circuit.Wavelength, b.Circuit.Wavelength); err != nil {
			return nil, err
		}
		fail(numericMismatch("", "wavelength",
			a.Circuit.Wavelength, b.Circuit.Wavelength,
			"circuit wavelength differs beyond tolerance"))
	}

	byID := func(m *Model) map[string]*Device {
		out := map[string]*Device{}
		for i := range m.Circuit.Devices {
			d := &m.Circuit.Devices[i]
			out[d.ID] = d
		}
		return out
	}
	da, db := byID(a), byID(b)

	ids := map[string]struct{}{}
	for id := range da {
		ids[id] = struct{}{}
	}
	for id := range db {
		ids[id] = struct{}{}
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)

	for _, id := range sorted {
		x, inA := da[id]
		y, inB := db[id]
		switch {
		case !inA:
			fail(Mismatch{DeviceID: id, Reason: "device present only in document B"})
			continue
		case !inB:
			fail(Mismatch{DeviceID: id, Reason: "device present only in document A"})
			continue
		}
		if x.Kind != y.Kind {
			fail(Mismatch{
				DeviceID: id,
				Quantity: "kind",
				Reason:   "device kind differs: " + x.Kind + " vs " + y.Kind,
			})
			continue
		}
		dc := DeviceComparison{DeviceID: id, Kind: x.Kind}
		pa, pb := effectiveParams(x), effectiveParams(y)
		keys := map[string]struct{}{}
		for k := range pa {
			keys[k] = struct{}{}
		}
		for k := range pb {
			keys[k] = struct{}{}
		}
		names := make([]string, 0, len(keys))
		for k := range keys {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			va, oka := pa[name]
			vb, okb := pb[name]
			switch {
			case !oka:
				fail(Mismatch{DeviceID: id, Quantity: name,
					Reason: "parameter present only in document B"})
			case !okb:
				fail(Mismatch{DeviceID: id, Quantity: name,
					Reason: "parameter present only in document A"})
			default:
				if err := finitePair(id, name, va, vb); err != nil {
					return nil, err
				}
				ok := withinTol(va, vb, tol)
				dc.Quantities = append(dc.Quantities, QuantityComparison{
					Name:            name,
					Unit:            paramUnit(name),
					A:               va,
					B:               vb,
					Diff:            abs64(va - vb),
					WithinTolerance: ok,
				})
				if !ok {
					fail(numericMismatch(id, name, va, vb,
						"normalized values differ beyond tolerance "+
							strconv.FormatFloat(tol, 'g', -1, 64)+
							" (unit "+paramUnit(name)+")"))
				}
			}
		}
		rep.Devices = append(rep.Devices, dc)
	}

	// 连接：作为无序多重集比较（连接顺序不影响物理含义）。
	ca := canonicalConnections(a)
	cb := canonicalConnections(b)
	if len(ca) != len(cb) {
		fail(Mismatch{Quantity: "connections",
			Reason: "connection count differs: " + itoa(len(ca)) + " vs " + itoa(len(cb))})
	} else {
		for i := range ca {
			if ca[i] != cb[i] {
				fail(Mismatch{Quantity: "connections",
					Reason: "connection sets differ, first divergence (sorted): " + ca[i] + " vs " + cb[i]})
				break
			}
		}
	}
	return rep, nil
}

// canonicalConnections 把连接表规范化为排序后的字符串多重集。
func canonicalConnections(m *Model) []string {
	out := make([]string, 0, len(m.Circuit.Connects))
	for _, cn := range m.Circuit.Connects {
		out = append(out, endpointLabel(cn.From)+"->"+endpointLabel(cn.To))
	}
	sort.Strings(out)
	return out
}

func abs64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
