package solver

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// QuantityComparison 是同一器件同一物理量在两份文档中的归一化对照。
// 为使对照与文档传入顺序无关，A=min(两边归一化值)，B=max(两边归一化值)。
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
// 差多少、为什么判为不等。Numeric 为 false 时 A/B/Diff 固定为 0 且无意义
// （结构性差异，如器件缺失、类型不同、连接不同），原因在 Reason 中。
// Numeric 为 true 时 A/B/Diff 必定出现且为有限值（不带 omitempty，
// 避免 0 差值等合法证据在 JSON 中缺项）。为使结论与文档传入顺序无关，
// A/B 按归一化数值规范化：A=min(两边), B=max(两边)。
type Mismatch struct {
	DeviceID string  `json:"device_id,omitempty"`
	Quantity string  `json:"quantity,omitempty"`
	Numeric  bool    `json:"numeric"`
	A        float64 `json:"a"`
	B        float64 `json:"b"`
	Diff     float64 `json:"diff"`
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
// 调用方必须先用 validateModelForEquivalence 拒绝取值非法、声明冲突或
// 归一化后无法确定（非有限）的文档；本函数只处理两边都具有确定物理语义
// 的情形，因此报告中的每个数值都保证是有限值、可直接 JSON 序列化。
// 结论与文档传入顺序无关：所有数值对照按 normalizedPair 规范化，
// 所有结构性原因使用不含 A/B 方位的对称表述（见各处 Reason）。
func compareModels(a, b *Model, tol float64) (*EquivalenceReport, error) {
	rep := &EquivalenceReport{Equal: true, Tolerance: tol}
	fail := func(m Mismatch) {
		rep.Equal = false
		rep.Mismatches = append(rep.Mismatches, m)
	}

	// 电路级：工作波长。
	if !withinTol(a.Circuit.Wavelength, b.Circuit.Wavelength, tol) {
		lo, hi, d := normalizedPair(a.Circuit.Wavelength, b.Circuit.Wavelength)
		fail(Mismatch{
			Quantity: "wavelength",
			Numeric:  true,
			A:        lo,
			B:        hi,
			Diff:     d,
			Reason: "circuit wavelength differs beyond tolerance " +
				strconv.FormatFloat(tol, 'g', -1, 64) + " (unit um)",
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
		case !inA || !inB:
			// 对称表述：不指出在哪一份文档，调换入参后文本不变。
			fail(Mismatch{DeviceID: id,
				Reason: "device is present in only one of the two documents"})
			continue
		}
		if x.Kind != y.Kind {
			k1, k2 := x.Kind, y.Kind
			if k2 < k1 {
				k1, k2 = k2, k1
			}
			fail(Mismatch{
				DeviceID: id,
				Quantity: "kind",
				Reason:   "device kind differs between documents: " + k1 + " vs " + k2,
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
			case !oka || !okb:
				fail(Mismatch{DeviceID: id, Quantity: name,
					Reason: "parameter is present in only one of the two documents"})
			default:
				lo, hi, d := normalizedPair(va, vb)
				ok := withinTol(va, vb, tol)
				dc.Quantities = append(dc.Quantities, QuantityComparison{
					Name:            name,
					Unit:            paramUnit(name),
					A:               lo,
					B:               hi,
					Diff:            d,
					WithinTolerance: ok,
				})
				if !ok {
					fail(Mismatch{
						DeviceID: id,
						Quantity: name,
						Numeric:  true,
						A:        lo,
						B:        hi,
						Diff:     d,
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
	// 用多重集差集给出对称证据，结论不依赖两份文档谁先谁后。
	ca := canonicalConnections(a)
	cb := canonicalConnections(b)
	onlyA, onlyB := multisetDiff(ca, cb)
	if n := len(onlyA) + len(onlyB); n > 0 {
		first := ""
		for _, s := range onlyA {
			if first == "" || s < first {
				first = s
			}
		}
		for _, s := range onlyB {
			if first == "" || s < first {
				first = s
			}
		}
		fail(Mismatch{Quantity: "connections",
			Reason: "connection multisets differ: " + itoa(n) +
				" normalized connection(s) have no counterpart in the other document" +
				"; first unmatched (sorted): " + first})
	}

	if err := checkReportFinite(rep); err != nil {
		return nil, err
	}
	return rep, nil
}

// normalizedPair 把两边的归一化数值规范化为与文档传入顺序无关的形式：
// lo=min(a,b)，hi=max(a,b)，diff=hi-lo。
func normalizedPair(a, b float64) (lo, hi, diff float64) {
	if a <= b {
		return a, b, b - a
	}
	return b, a, a - b
}

// multisetDiff 返回两个已规范化（排序）字符串多重集的对称差，
// 多余的重复项按出现次数展开；multisetDiff(a,b) 与 multisetDiff(b,a)
// 仅交换两个返回切片，差集总大小不变。
func multisetDiff(a, b []string) (onlyA, onlyB []string) {
	count := func(xs []string) map[string]int {
		m := map[string]int{}
		for _, x := range xs {
			m[x]++
		}
		return m
	}
	ca, cb := count(a), count(b)
	seen := map[string]struct{}{}
	keys := make([]string, 0, len(ca)+len(cb))
	for k := range ca {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	for k := range cb {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		for i := ca[k]; i > cb[k]; i-- {
			onlyA = append(onlyA, k)
		}
		for i := cb[k]; i > ca[k]; i-- {
			onlyB = append(onlyB, k)
		}
	}
	return onlyA, onlyB
}

// checkReportFinite 是最后一道防线：报告中任何数值若为 NaN/Inf，
// 说明归一化比较无法给出确定语义，整体返回错误而不是交出无法序列化的报告。
// 正常路径下 validateModelForEquivalence 已排除此类输入，此处不可达。
func checkReportFinite(rep *EquivalenceReport) error {
	bad := func(deviceID, quantity string, v float64) error {
		return newError(ErrInvalidParameter,
			"equivalence undecidable: normalized comparison for device %q quantity %q produced a non-finite value",
			deviceID, quantity).
			withDetail("device", deviceID).
			withDetail("parameter", quantity).
			withDetail("value", v)
	}
	check := func(deviceID, quantity string, v float64) error {
		if !isFinite(v) {
			return bad(deviceID, quantity, v)
		}
		return nil
	}
	for i := range rep.Devices {
		for _, q := range rep.Devices[i].Quantities {
			for _, v := range []float64{q.A, q.B, q.Diff} {
				if err := check(rep.Devices[i].DeviceID, q.Name, v); err != nil {
					return err
				}
			}
		}
	}
	for _, m := range rep.Mismatches {
		if !m.Numeric {
			continue
		}
		for _, v := range []float64{m.A, m.B, m.Diff} {
			if err := check(m.DeviceID, m.Quantity, v); err != nil {
				return err
			}
		}
	}
	return nil
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

// validateModelForEquivalence 在比较前确认文档具有确定的物理语义。
// 等价核验不要求拓扑可编译（悬空/成环属于求解关心的事），但任何会让
// 归一化数值失去物理意义的情形都必须整体拒绝：
//   - 波长非有限/非正、器件 ID 缺失或重复；
//   - 器件参数超出物理范围、分束器同时声明 theta 与 reflectivity
//     （两种写法自相矛盾，无法确定按哪一种理解）；
//   - 归一化（损耗单位折算、reflectivity→theta）后产生非有限值
//     （如 reflectivity=30 会令 asin(sqrt(30))=NaN）。
//
// 前两类复用编译期同一套校验规则（validateCircuitParams），
// 保证“能求解的文档”与“能参与等价核验的文档”对合法取值的认定一致。
func validateModelForEquivalence(m *Model) error {
	if err := validateCircuitParams(&m.Circuit); err != nil {
		return err
	}
	for i := range m.Circuit.Devices {
		d := &m.Circuit.Devices[i]
		p := effectiveParams(d)
		for name, v := range p {
			if !isFinite(v) {
				return newError(ErrInvalidParameter,
					"normalized parameter %q of device %q is non-finite; its physical semantics cannot be determined",
					name, d.ID).
					withDetail("device", d.ID).
					withDetail("parameter", name).
					withDetail("value", v)
			}
		}
	}
	return nil
}
