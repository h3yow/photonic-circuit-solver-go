package solver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func findDevice(t *testing.T, rep *AuditReport, id string) DeviceAudit {
	t.Helper()
	for _, d := range rep.Devices {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("device %q missing from audit report (must not be silently skipped)", id)
	return DeviceAudit{}
}

func findQuantity(t *testing.T, da DeviceAudit, name string) Quantity {
	t.Helper()
	for _, q := range da.Quantities {
		if q.Name == name {
			return q
		}
	}
	t.Fatalf("quantity %q missing for device %q", name, da.ID)
	return Quantity{}
}

// 逐器件单位声明的审计：1.2 文档中 w1 器件级 db_per_cm、w2 继承电路级，
// 损耗必须归一化到 db_per_m 并标注单位来源。
func TestAuditDeviceLevelUnitSources(t *testing.T) {
	doc := `{
	  "version": "1.2",
	  "circuit": {"id": "aud", "wavelength": 1.55, "loss_unit": "db_per_m",
	    "devices": [
	      {"id": "w1", "kind": "waveguide", "params": {"length": 2, "neff": 2, "loss": 1.5}, "loss_unit": "db_per_cm"},
	      {"id": "w2", "kind": "waveguide", "params": {"length": 2, "neff": 2, "loss": 150}}
	    ],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "w1", "port": "in"}},
	      {"from": {"device": "w1", "port": "out"}, "to": {"device": "w2", "port": "in"}},
	      {"from": {"device": "w2", "port": "out"}, "to": {"external": "out0"}}
	    ]
	  }
	}`
	e, _ := NewEngine()
	rep, err := e.AuditModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("AuditModel: %v", err)
	}
	if rep.DeclaredVersion != "1.2" || rep.ModelVersion != currentModelVersion {
		t.Fatalf("versions = %q/%q", rep.DeclaredVersion, rep.ModelVersion)
	}
	if rep.CircuitLossUnit != "db_per_m" || rep.CircuitLossUnitSource != SourceCircuitDeclared {
		t.Fatalf("circuit loss unit = %q (%s)", rep.CircuitLossUnit, rep.CircuitLossUnitSource)
	}
	q1 := findQuantity(t, findDevice(t, rep, "w1"), "loss")
	if q1.Value != 150 || q1.Unit != "db_per_m" {
		t.Fatalf("w1 loss = %v %s, want 150 db_per_m (normalized)", q1.Value, q1.Unit)
	}
	if q1.UnitSource != SourceDeviceDeclared || q1.Source != SourceDeviceParam {
		t.Fatalf("w1 loss sources = %q/%q", q1.UnitSource, q1.Source)
	}
	q2 := findQuantity(t, findDevice(t, rep, "w2"), "loss")
	if q2.Value != 150 || q2.UnitSource != SourceCircuitDeclared {
		t.Fatalf("w2 loss = %v (unit source %s), want 150 (circuit_declared)", q2.Value, q2.UnitSource)
	}
	// 两个器件归一化后衰减必须逐位一致（物理等价）。
	a1 := findQuantity(t, findDevice(t, rep, "w1"), "attenuation")
	a2 := findQuantity(t, findDevice(t, rep, "w2"), "attenuation")
	if a1.Value != a2.Value {
		t.Fatalf("attenuation mismatch: %v vs %v", a1.Value, a2.Value)
	}
}

// 1.0 文档：损耗单位按确定规则补齐，来源必须标为 version_default；
// 缺失的可选参数（loss）标为 default_rule 而不是静默写作未知。
func TestAuditVersionDefaultSources(t *testing.T) {
	doc := `{
	  "version": "1.0",
	  "circuit": {"id": "old", "wavelength": 1.55,
	    "devices": [{"id": "w", "kind": "waveguide", "params": {"length": 1, "neff": 2}}],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "w", "port": "in"}},
	      {"from": {"device": "w", "port": "out"}, "to": {"external": "out0"}}
	    ]
	  }
	}`
	e, _ := NewEngine()
	rep, err := e.AuditModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("AuditModel: %v", err)
	}
	if rep.CircuitLossUnitSource != SourceVersionDefault {
		t.Fatalf("circuit loss unit source = %q, want version_default", rep.CircuitLossUnitSource)
	}
	q := findQuantity(t, findDevice(t, rep, "w"), "loss")
	if q.Value != 0 || q.Source != SourceDefaultRule || q.UnitSource != SourceVersionDefault {
		t.Fatalf("loss = %v (src %s, unit src %s), want 0 (default_rule, version_default)",
			q.Value, q.Source, q.UnitSource)
	}
}

// 器件参数缺失/冲突时标为不可用并给出原因，不得用缺省值顶替，也不得跳过。
func TestAuditUnavailableDevices(t *testing.T) {
	doc := `{
	  "version": "1.2",
	  "circuit": {"id": "bad", "wavelength": 1.55,
	    "devices": [
	      {"id": "w_missing", "kind": "waveguide", "params": {"length": 1}},
	      {"id": "bs_conflict", "kind": "beamsplitter", "params": {"theta": 0.5, "reflectivity": 0.3}},
	      {"id": "ok", "kind": "phaseshifter", "params": {"phi0": 0.1}}
	    ],
	    "connects": []
	  }
	}`
	e, _ := NewEngine()
	rep, err := e.AuditModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("AuditModel: %v", err)
	}
	if len(rep.Devices) != 3 {
		t.Fatalf("report has %d devices, want 3 (unavailable ones must not be skipped)", len(rep.Devices))
	}
	d1 := findDevice(t, rep, "w_missing")
	if d1.Available || d1.Reason == "" || len(d1.Quantities) != 0 {
		t.Fatalf("w_missing: available=%v reason=%q quantities=%d", d1.Available, d1.Reason, len(d1.Quantities))
	}
	if !strings.Contains(d1.Reason, "neff") {
		t.Fatalf("w_missing reason should name the missing parameter, got %q", d1.Reason)
	}
	d2 := findDevice(t, rep, "bs_conflict")
	if d2.Available || !strings.Contains(d2.Reason, "theta") {
		t.Fatalf("bs_conflict: available=%v reason=%q", d2.Available, d2.Reason)
	}
	d3 := findDevice(t, rep, "ok")
	if !d3.Available {
		t.Fatalf("ok device should be available, reason=%q", d3.Reason)
	}
}

// 文档级失败（版本越界、迁移语义不确定、冲突声明）必须整体失败：
// 返回 nil 报告，绝不返回半截报告。
func TestAuditDocumentFailureNoPartialReport(t *testing.T) {
	docs := []string{
		`{"version":"9.9","circuit":{"id":"x","wavelength":1.55,"devices":[],"connects":[]}}`,
		`{"version":"1.1","circuit":{"id":"x","wavelength":1.55,
		  "devices":[{"id":"w","kind":"waveguide","params":{"length":1,"neff":2},"loss_unit":"db_per_m"}],
		  "connects":[]}}`,
		`{"version":"1.2","circuit":{"id":"x","wavelength":1.55,"loss_unit":"parsecs","devices":[],"connects":[]}}`,
	}
	e, _ := NewEngine()
	for i, doc := range docs {
		rep, err := e.AuditModel(context.Background(), []byte(doc))
		if err == nil {
			t.Fatalf("doc %d: expected error, got report %+v", i, rep)
		}
		if rep != nil {
			t.Fatalf("doc %d: partial report returned on failure: %+v", i, rep)
		}
	}
}

// 已载入电路的审计：全部器件可用，且报告可 JSON 序列化供程序继续处理。
func TestAuditLoadedCircuitAndJSON(t *testing.T) {
	doc := `{
	  "version": "1.2",
	  "circuit": {"id": "mzi", "wavelength": 1.55, "loss_unit": "db_per_cm",
	    "devices": [
	      {"id": "bs1", "kind": "beamsplitter", "params": {"reflectivity": 0.5}},
	      {"id": "w1", "kind": "waveguide", "params": {"length": 40, "neff": 2.4, "loss": 2}},
	      {"id": "ps", "kind": "phaseshifter", "params": {"k": 0.02, "u0": 0.5}},
	      {"id": "bs2", "kind": "beamsplitter", "params": {}}
	    ],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "bs1", "port": "a"}},
	      {"from": {"external": "in1"}, "to": {"device": "bs1", "port": "b"}},
	      {"from": {"device": "bs1", "port": "c"}, "to": {"device": "w1", "port": "in"}},
	      {"from": {"device": "bs1", "port": "d"}, "to": {"device": "ps", "port": "in"}},
	      {"from": {"device": "w1", "port": "out"}, "to": {"device": "bs2", "port": "a"}},
	      {"from": {"device": "ps", "port": "out"}, "to": {"device": "bs2", "port": "b"}},
	      {"from": {"device": "bs2", "port": "c"}, "to": {"external": "out0"}},
	      {"from": {"device": "bs2", "port": "d"}, "to": {"external": "out1"}}
	    ]
	  }
	}`
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	rep, err := e.Audit(context.Background(), c)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(rep.Devices) != 4 {
		t.Fatalf("report has %d devices, want 4", len(rep.Devices))
	}
	for _, d := range rep.Devices {
		if !d.Available {
			t.Fatalf("device %q unavailable in compiled circuit: %s", d.ID, d.Reason)
		}
	}
	// 缺省 50:50 分束器的来源必须标为 default_rule。
	bs2theta := findQuantity(t, findDevice(t, rep, "bs2"), "theta")
	if bs2theta.Source != SourceDefaultRule {
		t.Fatalf("bs2 theta source = %q, want default_rule", bs2theta.Source)
	}
	buf, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("report not JSON-serializable: %v", err)
	}
	var back AuditReport
	if err := json.Unmarshal(buf, &back); err != nil {
		t.Fatalf("report not JSON-round-trippable: %v", err)
	}
	if len(back.Devices) != len(rep.Devices) {
		t.Fatalf("round-trip device count = %d, want %d", len(back.Devices), len(rep.Devices))
	}
}
