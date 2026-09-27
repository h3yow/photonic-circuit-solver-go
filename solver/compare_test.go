package solver

import (
	"context"
	"strings"
	"testing"
)

// 同一电路的三种等价写法：1.0 隐式 db_per_m、1.1 电路级、1.2 器件级。
const (
	compareDocV10 = `{"version":"1.0","circuit":{"id":"x","wavelength":1.55,
	  "devices":[{"id":"w","kind":"waveguide","params":{"length":2,"neff":2,"loss":150}}],
	  "connects":[{"from":{"external":"in0"},"to":{"device":"w","port":"in"}},
	              {"from":{"device":"w","port":"out"},"to":{"external":"out0"}}]}}`
	compareDocV11 = `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"loss_unit":"db_per_m",
	  "devices":[{"id":"w","kind":"waveguide","params":{"length":2,"neff":2,"loss":150}}],
	  "connects":[{"from":{"external":"in0"},"to":{"device":"w","port":"in"}},
	              {"from":{"device":"w","port":"out"},"to":{"external":"out0"}}]}}`
	compareDocV12 = `{"version":"1.2","circuit":{"id":"x","wavelength":1.55,
	  "devices":[{"id":"w","kind":"waveguide","params":{"length":2,"neff":2,"loss":1.5},"loss_unit":"db_per_cm"}],
	  "connects":[{"from":{"external":"in0"},"to":{"device":"w","port":"in"}},
	              {"from":{"device":"w","port":"out"},"to":{"external":"out0"}}]}}`
)

// 等价：跨版本、跨声明写法（整体单位 vs 逐器件单位）必须判等，
// 并给出逐器件归一化对照与所用容差。
func TestCompareEquivalentAcrossVersionsAndDeclarations(t *testing.T) {
	e, _ := NewEngine()
	pairs := [][2]string{
		{compareDocV10, compareDocV11},
		{compareDocV11, compareDocV12},
		{compareDocV10, compareDocV12},
	}
	for i, p := range pairs {
		rep, err := e.CompareModels(context.Background(), []byte(p[0]), []byte(p[1]), 0)
		if err != nil {
			t.Fatalf("pair %d: %v", i, err)
		}
		if !rep.Equal {
			t.Fatalf("pair %d: want equal, mismatches=%+v", i, rep.Mismatches)
		}
		if rep.Tolerance != DefaultCompareTolerance {
			t.Fatalf("pair %d: tolerance = %v, want default %v", i, rep.Tolerance, DefaultCompareTolerance)
		}
		if len(rep.Devices) != 1 || rep.Devices[0].DeviceID != "w" {
			t.Fatalf("pair %d: devices = %+v", i, rep.Devices)
		}
		// 归一化对照必须包含 loss 且两侧值一致（都已折算为 db_per_m）。
		found := false
		for _, q := range rep.Devices[0].Quantities {
			if q.Name == "loss" {
				found = true
				if q.ValueA != 150 || q.ValueB != 150 || !q.Within || q.Unit != "db_per_m" {
					t.Fatalf("pair %d: loss compare = %+v", i, q)
				}
			}
		}
		if !found {
			t.Fatalf("pair %d: loss quantity missing from comparison", i)
		}
	}
}

// 不等：必须指出器件、物理量、两边取值、差值与原因。
func TestCompareUnequalReportsEvidence(t *testing.T) {
	docB := `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"loss_unit":"db_per_m",
	  "devices":[{"id":"w","kind":"waveguide","params":{"length":2,"neff":2,"loss":200}}],
	  "connects":[{"from":{"external":"in0"},"to":{"device":"w","port":"in"}},
	              {"from":{"device":"w","port":"out"},"to":{"external":"out0"}}]}}`
	e, _ := NewEngine()
	rep, err := e.CompareModels(context.Background(), []byte(compareDocV11), []byte(docB), 0)
	if err != nil {
		t.Fatalf("CompareModels: %v", err)
	}
	if rep.Equal {
		t.Fatalf("want unequal")
	}
	var m *Mismatch
	for i := range rep.Mismatches {
		if rep.Mismatches[i].Quantity == "loss" {
			m = &rep.Mismatches[i]
		}
	}
	if m == nil {
		t.Fatalf("no loss mismatch reported: %+v", rep.Mismatches)
	}
	if m.Device != "w" || m.ValueA != 150 || m.ValueB != 200 || m.Diff != 50 || m.Reason == "" {
		t.Fatalf("mismatch evidence incomplete: %+v", m)
	}
}

// 不等：器件集合与拓扑差异也必须判为不等并说明。
func TestCompareStructuralDifferences(t *testing.T) {
	extraDev := `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"loss_unit":"db_per_m",
	  "devices":[{"id":"w","kind":"waveguide","params":{"length":2,"neff":2,"loss":150}},
	             {"id":"w2","kind":"waveguide","params":{"length":1,"neff":2}}],
	  "connects":[{"from":{"external":"in0"},"to":{"device":"w","port":"in"}},
	              {"from":{"device":"w","port":"out"},"to":{"external":"out0"}}]}}`
	diffTopo := `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"loss_unit":"db_per_m",
	  "devices":[{"id":"w","kind":"waveguide","params":{"length":2,"neff":2,"loss":150}}],
	  "connects":[{"from":{"external":"inA"},"to":{"device":"w","port":"in"}},
	              {"from":{"device":"w","port":"out"},"to":{"external":"out0"}}]}}`
	e := mustEngine(t)
	rep, err := e.CompareModels(context.Background(), []byte(compareDocV11), []byte(extraDev), 0)
	if err != nil {
		t.Fatalf("CompareModels: %v", err)
	}
	if rep.Equal || !hasMismatchReason(rep, "w2") {
		t.Fatalf("extra device not flagged: equal=%v mismatches=%+v", rep.Equal, rep.Mismatches)
	}
	rep, err = e.CompareModels(context.Background(), []byte(compareDocV11), []byte(diffTopo), 0)
	if err != nil {
		t.Fatalf("CompareModels: %v", err)
	}
	if rep.Equal || !hasMismatchReason(rep, "topology") {
		t.Fatalf("topology difference not flagged: equal=%v mismatches=%+v", rep.Equal, rep.Mismatches)
	}
}

func hasMismatchReason(rep *CompareReport, substr string) bool {
	for _, m := range rep.Mismatches {
		if strings.Contains(m.Reason, substr) || m.Device == substr || m.Quantity == substr {
			return true
		}
	}
	return false
}

func mustEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// 容差口径：差异在容差内判等，超出判不等。
func TestCompareToleranceSemantics(t *testing.T) {
	docClose := `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"loss_unit":"db_per_m",
	  "devices":[{"id":"w","kind":"waveguide","params":{"length":2,"neff":2,"loss":150.0000001}}],
	  "connects":[{"from":{"external":"in0"},"to":{"device":"w","port":"in"}},
	              {"from":{"device":"w","port":"out"},"to":{"external":"out0"}}]}}`
	e := mustEngine(t)
	rep, err := e.CompareModels(context.Background(), []byte(compareDocV11), []byte(docClose), 1e-6)
	if err != nil {
		t.Fatalf("CompareModels: %v", err)
	}
	if !rep.Equal {
		t.Fatalf("within tolerance should be equal: %+v", rep.Mismatches)
	}
	rep, err = e.CompareModels(context.Background(), []byte(compareDocV11), []byte(docClose), 1e-12)
	if err != nil {
		t.Fatalf("CompareModels: %v", err)
	}
	if rep.Equal {
		t.Fatalf("beyond tolerance should be unequal")
	}
}

// 版本组合无法确定语义时必须整体拒绝：返回 nil 报告，
// 保留原始错误码，不得挑一份继续算。
func TestCompareUndeterminedVersionCombinationRejected(t *testing.T) {
	bad := `{"version":"3.0","circuit":{"id":"x","wavelength":1.55,"devices":[],"connects":[]}}`
	migr := `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,
	  "devices":[{"id":"w","kind":"waveguide","params":{"length":1,"neff":2},"loss_unit":"db_per_m"}],
	  "connects":[]}}`
	e := mustEngine(t)
	cases := []struct {
		a, b string
		code ErrorCode
	}{
		{bad, compareDocV11, ErrVersionUnsupported},
		{compareDocV11, bad, ErrVersionUnsupported},
		{migr, compareDocV11, ErrModelMigration},
		{compareDocV11, migr, ErrModelMigration},
	}
	for i, tc := range cases {
		rep, err := e.CompareModels(context.Background(), []byte(tc.a), []byte(tc.b), 0)
		if err == nil {
			t.Fatalf("case %d: expected rejection, got report %+v", i, rep)
		}
		if rep != nil {
			t.Fatalf("case %d: partial report on failure: %+v", i, rep)
		}
		if !IsCode(err, tc.code) {
			t.Fatalf("case %d: want code %s, got %v", i, tc.code, err)
		}
	}
}
