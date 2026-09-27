package solver

import (
	"context"
	"math"
	"strings"
	"testing"
)

func findQuantity(t *testing.T, da DeviceAudit, name string) AuditedQuantity {
	t.Helper()
	for _, q := range da.Quantities {
		if q.Name == name {
			return q
		}
	}
	t.Fatalf("device %s: quantity %q not found (silently skipped?)", da.ID, name)
	return AuditedQuantity{}
}

func findDevice(t *testing.T, r *AuditReport, id string) DeviceAudit {
	t.Helper()
	for _, d := range r.Devices {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("device %q not found in report", id)
	return DeviceAudit{}
}

func TestAuditWaveguideProvenanceAndNormalization(t *testing.T) {
	doc := `{"version":"1.2","circuit":{"id":"x","wavelength":1.55,"loss_unit":"db_per_cm",
	  "devices":[
	    {"id":"w_dev","kind":"waveguide","loss_unit":"db_per_m","params":{"length":1000000,"neff":2,"loss":200}},
	    {"id":"w_cir","kind":"waveguide","params":{"length":1000000,"neff":2,"loss":2}},
	    {"id":"w_def","kind":"waveguide","params":{"length":1000000,"neff":2}}
	  ],
	  "connects":[
	    {"from":{"external":"in0"},"to":{"device":"w_dev","port":"in"}},
	    {"from":{"device":"w_dev","port":"out"},"to":{"device":"w_cir","port":"in"}},
	    {"from":{"device":"w_cir","port":"out"},"to":{"device":"w_def","port":"in"}},
	    {"from":{"device":"w_def","port":"out"},"to":{"external":"out0"}}
	  ]}}`
	e, _ := NewEngine()
	rep, err := e.AuditDocument(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("AuditDocument: %v", err)
	}

	// 器件级声明：200 dB/m 保持 200，来源 device/device。
	q := findQuantity(t, findDevice(t, rep, "w_dev"), QLoss)
	if !q.Available || q.Value != 200 || q.Unit != "db_per_m" ||
		q.Source != SourceDeviceParam || q.UnitSource != UnitSourceDevice {
		t.Fatalf("w_dev loss = %+v", q)
	}
	// 电路级声明：2 dB/cm 归一化为 200 dB/m，来源 device_param/circuit。
	q = findQuantity(t, findDevice(t, rep, "w_cir"), QLoss)
	if !q.Available || q.Value != 200 || q.UnitSource != UnitSourceCircuit {
		t.Fatalf("w_cir loss = %+v, want normalized 200 db_per_m unit:circuit", q)
	}
	// 电路级声明时，未单独声明的器件回落到电路级单位。
	q = findQuantity(t, findDevice(t, rep, "w_def"), QLoss)
	if !q.Available || q.Value != 0 || q.Source != SourceDefault ||
		q.UnitSource != UnitSourceCircuit {
		t.Fatalf("w_def loss = %+v, want 0 default/unit:circuit", q)
	}
	// 派生量：1 m、200 dB/m => 衰减 ln(10)/10*200 奈培，透射 1e-20。
	wantAlpha := math.Ln10 / 10 * 200
	att := findQuantity(t, findDevice(t, rep, "w_dev"), QAttenuation)
	if !att.Available || math.Abs(att.Value-wantAlpha) > 1e-9*wantAlpha ||
		att.Unit != "nepers" || att.Source != SourceDerived {
		t.Fatalf("attenuation = %+v, want %v nepers derived", att, wantAlpha)
	}
	tr := findQuantity(t, findDevice(t, rep, "w_dev"), QTransmission)
	if !tr.Available || math.Abs(tr.Value-1e-20) > 1e-9*1e-20 {
		t.Fatalf("transmission = %+v, want 1e-20", tr)
	}
	ph := findQuantity(t, findDevice(t, rep, "w_dev"), QPhase)
	wantPhase := 2 * math.Pi * 2 * 1e6 / 1.55
	if !ph.Available || math.Abs(ph.Value-wantPhase) > 1e-9*wantPhase || ph.Unit != "rad" {
		t.Fatalf("phase = %+v, want %v rad", ph, wantPhase)
	}
}

func TestAuditOnLoadedCircuit(t *testing.T) {
	e, _ := NewEngine()
	c := loadDoc(t, waveguideDoc("1.1", "db_per_cm", "", 2))
	rep, err := e.Audit(context.Background(), c)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	q := findQuantity(t, findDevice(t, rep, "w"), QLoss)
	if !q.Available || q.Value != 200 || q.UnitSource != UnitSourceCircuit {
		t.Fatalf("loss = %+v, want 200 db_per_m unit:circuit", q)
	}
	// 人类可读输出包含关键信息。
	s := rep.String()
	for _, want := range []string{"w", "loss", "unit:circuit", "attenuation"} {
		if !strings.Contains(s, want) {
			t.Fatalf("String() missing %q:\n%s", want, s)
		}
	}

	// 1.0 文档：单位由版本缺省规则补齐，审计必须标明。
	rep, err = e.AuditDocument(context.Background(), []byte(waveguideDoc("1.0", "", "", 5)))
	if err != nil {
		t.Fatalf("AuditDocument 1.0: %v", err)
	}
	q = findQuantity(t, findDevice(t, rep, "w"), QLoss)
	if !q.Available || q.Value != 5 || q.UnitSource != UnitSourceVersionDefault {
		t.Fatalf("1.0 loss = %+v, want 5 db_per_m unit:version_default", q)
	}
}

func TestAuditUnavailableMarkedNotDefaulted(t *testing.T) {
	// 波导缺 neff：相位不可算，必须标不可用并给出原因，
	// 其余量（损耗、衰减、透射）仍正常报告。
	doc := `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,
	  "devices":[{"id":"w","kind":"waveguide","params":{"length":100,"loss":1}}],
	  "connects":[]}}`
	e, _ := NewEngine()
	rep, err := e.AuditDocument(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("AuditDocument: %v", err)
	}
	d := findDevice(t, rep, "w")
	ph := findQuantity(t, d, QPhase)
	if ph.Available {
		t.Fatalf("phase must be unavailable, got %+v", ph)
	}
	if !strings.Contains(ph.Reason, "neff") {
		t.Fatalf("phase reason %q should mention neff", ph.Reason)
	}
	if ph.Value != 0 {
		t.Fatalf("unavailable quantity must not carry a substitute value, got %v", ph.Value)
	}
	if q := findQuantity(t, d, QLoss); !q.Available || q.Value != 1 {
		t.Fatalf("loss should still be reported, got %+v", q)
	}
	if q := findQuantity(t, d, QAttenuation); !q.Available {
		t.Fatalf("attenuation should be computable without neff, got %+v", q)
	}
	// 人类可读输出明确标出不可用与原因。
	if s := rep.String(); !strings.Contains(s, "UNAVAILABLE") {
		t.Fatalf("String() should mark unavailable quantities:\n%s", s)
	}
}

func TestAuditConflictingBeamsplitterParams(t *testing.T) {
	doc := `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,
	  "devices":[{"id":"bs","kind":"beamsplitter","params":{"theta":0.5,"reflectivity":0.3}}],
	  "connects":[]}}`
	e, _ := NewEngine()
	rep, err := e.AuditDocument(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("AuditDocument: %v", err)
	}
	q := findQuantity(t, findDevice(t, rep, "bs"), QTheta)
	if q.Available || !strings.Contains(q.Reason, "onflict") {
		t.Fatalf("theta should be unavailable due to conflict, got %+v", q)
	}
}

func TestAuditResonatorMissingRequiredParam(t *testing.T) {
	doc := `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,
	  "devices":[{"id":"r","kind":"resonator","params":{"r_cross":0.9,"a":0.99}}],
	  "connects":[]}}`
	e, _ := NewEngine()
	rep, err := e.AuditDocument(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("AuditDocument: %v", err)
	}
	q := findQuantity(t, findDevice(t, rep, "r"), QSelfCoupling)
	if q.Available || !strings.Contains(q.Reason, "r_self") {
		t.Fatalf("r_self should be unavailable with reason, got %+v", q)
	}
	// phi_roundtrip 有确定缺省 0，应正常给出并标注来源。
	q = findQuantity(t, findDevice(t, rep, "r"), QRoundtripPhase)
	if !q.Available || q.Value != 0 || q.Source != SourceDefault {
		t.Fatalf("phi_roundtrip = %+v, want 0 default", q)
	}
}

// 文档级失败（版本超界、未知字段、冲突声明、迁移语义不确定）
// 必须整体拒绝：返回错误且报告为 nil，绝不返回半截报告。
func TestAuditDocumentLevelFailureNoPartialReport(t *testing.T) {
	e, _ := NewEngine()
	cases := map[string]ErrorCode{
		`{"version":"9.9","circuit":{"id":"x","wavelength":1.55,"devices":[],"connects":[]}}`:                                                ErrVersionUnsupported,
		`{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"devices":[],"connects":[]},"zzz":1}`:                                        ErrUnknownField,
		`{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"loss_unit":"db_per_m","loss_unit":"db_per_cm","devices":[],"connects":[]}}`: ErrConflictingDeclaration,
		// 1.0 文档携带 1.1 字段：版本语义不确定。
		`{"version":"1.0","circuit":{"id":"x","wavelength":1.55,"loss_unit":"db_per_m","devices":[],"connects":[]}}`: ErrModelMigration,
	}
	for doc, want := range cases {
		rep, err := e.AuditDocument(context.Background(), []byte(doc))
		if err == nil {
			t.Fatalf("expected error for %s", doc)
		}
		if rep != nil {
			t.Fatalf("partial report returned on failure for %s", doc)
		}
		if !IsCode(err, want) {
			t.Fatalf("doc %s: want code %s, got %v", doc, want, err)
		}
	}
}
