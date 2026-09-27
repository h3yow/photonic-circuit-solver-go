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
// 差多少、为什么判为不等。Numeric 为 false 时 A/B/Diff 无效
// （结构性差异，如器件缺失、类型不同、连接不同），原因在 Reason 中。
type Mismatch struct {
	DeviceID string  `json:"device_id,omitempty"`
	Quantity string  `json:"quantity,omitempty"`
	Numeric  bool    `json:"numeric"`
	A        float64 `json:"a,omitempty"`
	B        float64 `json:"b,omitempty"`
	Diff     float64 `json:"diff,omitempty"`
	Reason   string  `json:"reason"`
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
				strconv.FormatFloat(m.A, 'g', -1, 64),
				strconv.FormatFloat(m.B, 'g', -1, 64),
				strconv.FormatFloat(m.Diff, 'g', -1, 64),
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
func compareModels(a, b *Model, tol float64) *EquivalenceReport {
	rep := &EquivalenceReport{Equal: true, Tolerance: tol}
	fail := func(m Mismatch) {
		rep.Equal = false
		rep.Mismatches = append(rep.Mismatches, m)
	}

	// 电路级：工作波长。
	if !withinTol(a.Circuit.Wavelength, b.Circuit.Wavelength, tol) {
		fail(Mismatch{
			Quantity: "wavelength",
			Numeric:  true,
			A:        a.Circuit.Wavelength,
			B:        b.Circuit.Wavelength,
			Diff:     abs64(a.Circuit.Wavelength - b.Circuit.Wavelength),
			Reason:   "circuit wavelength differs beyond tolerance",
		})
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
					fail(Mismatch{
						DeviceID: id,
						Quantity: name,
						Numeric:  true,
						A:        va,
						B:        vb,
						Diff:     abs64(va - vb),
						Reason: "normalized values differ beyond tolerance " +
							strconv.FormatFloat(tol, 'g', -1, 64) +
							" (unit " + paramUnit(name) + ")",
					})
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
	return rep
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
