package solver

import (
	"context"
	"strings"
	"testing"
)

func compareDocs(t *testing.T, a, b string) *EquivalenceReport {
	t.Helper()
	e, _ := NewEngine()
	rep, err := e.CompareModels(context.Background(), []byte(a), []byte(b))
	if err != nil {
		t.Fatalf("CompareModels: %v", err)
	}
	if rep == nil {
		t.Fatal("nil report without error")
	}
	return rep
}

// 跨版本等价：1.0 隐式 db_per_m 与 1.2 器件级 db_per_cm 表达同一物理电路。
func TestEquivalenceAcrossVersions(t *testing.T) {
	a := waveguideDoc("1.0", "", "", 200)
	b := waveguideDoc("1.2", "", "db_per_cm", 2)
	rep := compareDocs(t, a, b)
	if !rep.Equal {
		t.Fatalf("want equal, got mismatches:\n%s", rep.String())
	}
	if rep.Tolerance != 1e-9 {
		t.Fatalf("tolerance = %v, want 1e-9", rep.Tolerance)
	}
	// 相等时给出逐器件归一化对照。
	if len(rep.Devices) != 1 || rep.Devices[0].DeviceID != "w" {
		t.Fatalf("devices = %+v", rep.Devices)
	}
	var lossCmp *QuantityComparison
	for i := range rep.Devices[0].Quantities {
		if rep.Devices[0].Quantities[i].Name == "loss" {
			lossCmp = &rep.Devices[0].Quantities[i]
		}
	}
	if lossCmp == nil {
		t.Fatalf("loss comparison missing: %+v", rep.Devices[0].Quantities)
	}
	if lossCmp.A != 200 || lossCmp.B != 200 || lossCmp.Unit != "db_per_m" || !lossCmp.WithinTolerance {
		t.Fatalf("loss comparison = %+v, want normalized 200 vs 200 db_per_m", lossCmp)
	}
}

// 跨声明等价：电路级 db_per_cm 与器件级 db_per_cm 写法不同、物理相同。
func TestEquivalenceAcrossDeclarationStyles(t *testing.T) {
	a := waveguideDoc("1.1", "db_per_cm", "", 2)
	b := waveguideDoc("1.2", "", "db_per_cm", 2)
	if rep := compareDocs(t, a, b); !rep.Equal {
		t.Fatalf("want equal:\n%s", rep.String())
	}
	// 缺省与显式写法等价：loss 缺省 0 vs 显式 loss:0。
	a = waveguideDoc("1.1", "", "", -1) // 不写 loss
	b = waveguideDoc("1.1", "", "", 0)
	if rep := compareDocs(t, a, b); !rep.Equal {
		t.Fatalf("default vs explicit loss=0 want equal:\n%s", rep.String())
	}
}

// 分束器 theta 与 reflectivity 两种写法表达同一器件时判等价。
func TestEquivalenceThetaVsReflectivity(t *testing.T) {
	mk := func(param string) string {
		return `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,
		  "devices":[{"id":"bs","kind":"beamsplitter","params":{` + param + `}}],
		  "connects":[
		    {"from":{"external":"in0"},"to":{"device":"bs","port":"a"}},
		    {"from":{"external":"in1"},"to":{"device":"bs","port":"b"}},
		    {"from":{"device":"bs","port":"c"},"to":{"external":"out0"}},
		    {"from":{"device":"bs","port":"d"},"to":{"external":"out1"}}
		  ]}}`
	}
	// theta=pi/6 <=> reflectivity=sin^2(pi/6)=0.25
	a := mk(`"theta":0.5235987755982988`)
	b := mk(`"reflectivity":0.24999999999999994`)
	rep := compareDocs(t, a, b)
	if !rep.Equal {
		t.Fatalf("theta vs reflectivity want equal:\n%s", rep.String())
	}
}

// 不等价：必须指出器件、物理量、两边取值、差值与原因。
func TestInequivalenceLossDiffers(t *testing.T) {
	a := waveguideDoc("1.0", "", "", 200)        // 200 dB/m
	b := waveguideDoc("1.2", "", "db_per_cm", 3) // 300 dB/m
	rep := compareDocs(t, a, b)
	if rep.Equal {
		t.Fatal("want not equal")
	}
	if len(rep.Mismatches) != 1 {
		t.Fatalf("want exactly 1 mismatch, got %+v", rep.Mismatches)
	}
	m := rep.Mismatches[0]
	if m.DeviceID != "w" || m.Quantity != "loss" || !m.Numeric {
		t.Fatalf("mismatch = %+v", m)
	}
	if m.A != 200 || m.B != 300 || m.Diff != 100 {
		t.Fatalf("mismatch values = %+v, want A=200 B=300 Diff=100 (normalized)", m)
	}
	if !strings.Contains(m.Reason, "tolerance") {
		t.Fatalf("reason should cite tolerance: %q", m.Reason)
	}
	// 人类可读输出包含判定依据。
	s := rep.String()
	for _, want := range []string{"MISMATCH", "w", "loss", "200", "300"} {
		if !strings.Contains(s, want) {
			t.Fatalf("String() missing %q:\n%s", want, s)
		}
	}
}

func TestInequivalenceStructural(t *testing.T) {
	base := waveguideDoc("1.1", "", "", 0)
	// 器件只在一侧出现。
	extra := `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,
	  "devices":[
	    {"id":"w","kind":"waveguide","params":{"length":1000000,"neff":2,"loss":0}},
	    {"id":"w2","kind":"waveguide","params":{"length":1,"neff":2}}
	  ],
	  "connects":[
	    {"from":{"external":"in0"},"to":{"device":"w","port":"in"}},
	    {"from":{"device":"w","port":"out"},"to":{"device":"w2","port":"in"}},
	    {"from":{"device":"w2","port":"out"},"to":{"external":"out0"}}
	  ]}}`
	rep := compareDocs(t, base, extra)
	if rep.Equal {
		t.Fatal("want not equal")
	}
	found := false
	for _, m := range rep.Mismatches {
		if m.DeviceID == "w2" && strings.Contains(m.Reason, "only in document B") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing structural mismatch for w2: %+v", rep.Mismatches)
	}

	// 波长不同（电路级量）。
	other := strings.Replace(base, `"wavelength":1.55`, `"wavelength":1.31`, 1)
	rep = compareDocs(t, base, other)
	if rep.Equal || len(rep.Mismatches) != 1 || rep.Mismatches[0].Quantity != "wavelength" {
		t.Fatalf("wavelength mismatch = %+v", rep.Mismatches)
	}

	// 连接不同。
	renamed := strings.Replace(base, `"out0"`, `"outX"`, 1)
	rep = compareDocs(t, base, renamed)
	if rep.Equal {
		t.Fatal("want not equal for different connections")
	}
	found = false
	for _, m := range rep.Mismatches {
		if m.Quantity == "connections" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing connections mismatch: %+v", rep.Mismatches)
	}
}

// 版本组合无法确定语义时必须整体拒绝：返回错误且报告为 nil，
// 不能挑能解析的一份继续算。
func TestEquivalenceUndecidableVersionComboRejected(t *testing.T) {
	e := mustEngine(t)
	good := waveguideDoc("1.1", "", "", 0)
	bad := []string{
		waveguideDoc("9.9", "", "", 0),          // 版本超界
		waveguideDoc("1.0", "db_per_m", "", 0),  // 1.0 携带 1.1 字段
		waveguideDoc("1.1", "", "db_per_cm", 0), // 1.1 携带 1.2 字段
		`{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"loss_unit":"db_per_m","loss_unit":"db_per_cm","devices":[],"connects":[]}}`, // 冲突声明
	}
	for _, doc := range bad {
		for _, order := range [][2]string{{doc, good}, {good, doc}} {
			rep, err := e.CompareModels(context.Background(), []byte(order[0]), []byte(order[1]))
			if err == nil {
				t.Fatalf("want refusal for undecidable combo:\nA=%s\nB=%s", order[0], order[1])
			}
			if rep != nil {
				t.Fatalf("partial report returned on refusal")
			}
		}
	}
}

func mustEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}
