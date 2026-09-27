package solver

import (
	"context"
	"math"
	"sort"
)

// 等价核验：对两份模型文档分别解析、迁移并归一化后逐器件对照。
// 判定基于归一化量值（而非文本），因此整体声明单位与逐器件声明单位、
// 不同历史版本写法只要物理含义一致即判等价。

// DefaultCompareTolerance 是未显式给出容差时使用的相对容差。
const DefaultCompareTolerance = 1e-9

// QuantityCompare 是单个物理量在两份文档中的归一化对照。
type QuantityCompare struct {
	Name   string  `json:"name"`
	Unit   string  `json:"unit"`
	ValueA float64 `json:"value_a"`
	ValueB float64 `json:"value_b"`
	Diff   float64 `json:"diff"`
	Within bool    `json:"within_tolerance"`
}

// DeviceCompare 是单个器件的对照结果。
type DeviceCompare struct {
	DeviceID   string            `json:"device_id"`
	Kind       string            `json:"kind"`
	Quantities []QuantityCompare `json:"quantities,omitempty"`
}

// Mismatch 记录一处判为不等的依据：哪个器件、哪个物理量、
// 两边各是什么、差了多少、为什么判不等。
type Mismatch struct {
	Device   string  `json:"device,omitempty"`
	Quantity string  `json:"quantity,omitempty"`
	ValueA   float64 `json:"value_a,omitempty"`
	ValueB   float64 `json:"value_b,omitempty"`
	Diff     float64 `json:"diff,omitempty"`
	Reason   string  `json:"reason"`
}

// CompareReport 是等价核验结果。Equal 为 true 时 Devices 给出逐器件
// 归一化对照与所用容差；为 false 时 Mismatches 给出全部判定依据。
type CompareReport struct {
	Equal      bool            `json:"equal"`
	Tolerance  float64         `json:"tolerance"`
	VersionA   ModelVersion    `json:"version_a"`
	VersionB   ModelVersion    `json:"version_b"`
	Devices    []DeviceCompare `json:"devices,omitempty"`
	Mismatches []Mismatch      `json:"mismatches,omitempty"`
}

// CompareModels 对比两份模型文档的物理等价性。
// 任一文档无法解析（含版本越界、迁移语义不确定、冲突声明）时整体拒绝，
// 返回 nil 报告与错误，绝不挑一份继续算，也不返回半截报告。
// tolerance <= 0 时使用 DefaultCompareTolerance；
// 判定口径：|a-b| <= tolerance * max(1, |a|, |b|)。
func (e *Engine) CompareModels(ctx context.Context, dataA, dataB []byte, tolerance float64) (*CompareReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	mA, err := parseModel(dataA, e.limits)
	if err != nil {
		return nil, sideError("left", err)
	}
	mB, err := parseModel(dataB, e.limits)
	if err != nil {
		return nil, sideError("right", err)
	}
	if tolerance <= 0 {
		tolerance = DefaultCompareTolerance
	}
	return compareModels(mA, mB, tolerance), nil
}

// sideError 为对比中某一侧文档的解析错误补充方位信息，
// 同时保留原始错误码（如 VERSION_UNSUPPORTED），调用方可据 IsCode 判定。
func sideError(side string, err error) error {
	if se, ok := AsSolverError(err); ok {
		return wrapError(se.Code, err, "%s document rejected", side)
	}
	return wrapError(ErrInvalidParameter, err, "%s document rejected", side)
}

// compareModels 在两个已迁移模型间执行等价对照（纯函数，无共享状态）。
func compareModels(mA, mB *Model, tol float64) *CompareReport {
	rep := &CompareReport{
		Tolerance: tol,
		VersionA:  mA.DeclaredVersion,
		VersionB:  mB.DeclaredVersion,
	}
	within := func(a, b float64) bool {
		return math.Abs(a-b) <= tol*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
	}
	mismatch := func(m Mismatch) { rep.Mismatches = append(rep.Mismatches, m) }

	// 电路级物理量：工作波长。
	if !within(mA.Circuit.Wavelength, mB.Circuit.Wavelength) {
		mismatch(Mismatch{
			Quantity: "wavelength",
			ValueA:   mA.Circuit.Wavelength,
			ValueB:   mB.Circuit.Wavelength,
			Diff:     math.Abs(mA.Circuit.Wavelength - mB.Circuit.Wavelength),
			Reason:   "circuit wavelength differs beyond tolerance",
		})
	}

	audA := auditModel(mA)
	audB := auditModel(mB)
	byID := map[string]DeviceAudit{}
	for _, d := range audB.Devices {
		byID[d.ID] = d
	}
	seen := map[string]bool{}
	for _, da := range audA.Devices {
		seen[da.ID] = true
		db, ok := byID[da.ID]
		if !ok {
			mismatch(Mismatch{Device: da.ID, Reason: "device present only in left document"})
			continue
		}
		if da.Kind != db.Kind {
			mismatch(Mismatch{Device: da.ID,
				Reason: "device kind differs: " + da.Kind + " vs " + db.Kind})
			continue
		}
		// 任一侧不可用都不得悄悄跳过：记为不等并给出两侧原因。
		if !da.Available || !db.Available {
			reason := "device unavailable: "
			if !da.Available {
				reason += "left: " + da.Reason
			}
			if !db.Available {
				if !da.Available {
					reason += "; "
				}
				reason += "right: " + db.Reason
			}
			mismatch(Mismatch{Device: da.ID, Reason: reason})
			continue
		}
		dc := DeviceCompare{DeviceID: da.ID, Kind: da.Kind}
		qbByName := map[string]Quantity{}
		for _, q := range db.Quantities {
			qbByName[q.Name] = q
		}
		for _, qa := range da.Quantities {
			qb, ok := qbByName[qa.Name]
			if !ok {
				mismatch(Mismatch{Device: da.ID, Quantity: qa.Name,
					Reason: "quantity present only in left document"})
				continue
			}
			diff := math.Abs(qa.Value - qb.Value)
			okW := within(qa.Value, qb.Value)
			dc.Quantities = append(dc.Quantities, QuantityCompare{
				Name: qa.Name, Unit: qa.Unit,
				ValueA: qa.Value, ValueB: qb.Value,
				Diff: diff, Within: okW,
			})
			if !okW {
				mismatch(Mismatch{
					Device: da.ID, Quantity: qa.Name,
					ValueA: qa.Value, ValueB: qb.Value, Diff: diff,
					Reason: "normalized values differ beyond tolerance",
				})
			}
		}
		rep.Devices = append(rep.Devices, dc)
	}
	for _, db := range audB.Devices {
		if !seen[db.ID] {
			mismatch(Mismatch{Device: db.ID, Reason: "device present only in right document"})
		}
	}

	// 拓扑对照：连接归一化为 "from->to" 多重集合比较。
	if !sameTopology(mA, mB) {
		mismatch(Mismatch{Quantity: "topology",
			Reason: "connection sets differ between documents"})
	}

	rep.Equal = len(rep.Mismatches) == 0
	return rep
}

// sameTopology 比较两个模型的连接多重集合是否一致。
func sameTopology(mA, mB *Model) bool {
	norm := func(m *Model) []string {
		var out []string
		for _, cn := range m.Circuit.Connects {
			out = append(out, endpointLabel(cn.From)+"->"+endpointLabel(cn.To))
		}
		sort.Strings(out)
		return out
	}
	a, b := norm(mA), norm(mB)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
