package solver

import (
	"context"
	"testing"
)

// waveguideDoc 生成单波导电路文档（in0 -> w -> out0）。
func waveguideDoc(version, circuitUnit, deviceUnit string, loss float64) string {
	cu := ""
	if circuitUnit != "" {
		cu = `,"loss_unit":"` + circuitUnit + `"`
	}
	du := ""
	if deviceUnit != "" {
		du = `,"loss_unit":"` + deviceUnit + `"`
	}
	lossStr := ""
	if loss >= 0 {
		lossStr = `,"loss":` + formatNum(loss)
	}
	return `{"version":"` + version + `","circuit":{"id":"x","wavelength":1.55` + cu + `,
	  "devices":[{"id":"w","kind":"waveguide"` + du + `,"params":{"length":1000000,"neff":2` + lossStr + `}}],
	  "connects":[
	    {"from":{"external":"in0"},"to":{"device":"w","port":"in"}},
	    {"from":{"device":"w","port":"out"},"to":{"external":"out0"}}
	  ]}}`
}

func loadDoc(t *testing.T, doc string) *CompiledCircuit {
	t.Helper()
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("LoadModel: %v\ninput:\n%s", err, doc)
	}
	return c
}

func TestVersion12DeviceLossUnitOverridesCircuit(t *testing.T) {
	doc := `{"version":"1.2","circuit":{"id":"x","wavelength":1.55,"loss_unit":"db_per_cm",
	  "devices":[
	    {"id":"w1","kind":"waveguide","loss_unit":"db_per_m","params":{"length":1,"neff":2,"loss":3}},
	    {"id":"w2","kind":"waveguide","params":{"length":1,"neff":2,"loss":3}}
	  ],
	  "connects":[
	    {"from":{"external":"in0"},"to":{"device":"w1","port":"in"}},
	    {"from":{"device":"w1","port":"out"},"to":{"device":"w2","port":"in"}},
	    {"from":{"device":"w2","port":"out"},"to":{"external":"out0"}}
	  ]}}`
	c := loadDoc(t, doc)
	w1 := c.model.Circuit.Devices[0]
	w2 := c.model.Circuit.Devices[1]
	if w1.LossUnit != "db_per_m" || w1.LossUnitSource != UnitSourceDevice {
		t.Fatalf("w1 unit=%q source=%q, want db_per_m/device", w1.LossUnit, w1.LossUnitSource)
	}
	if w2.LossUnit != "db_per_cm" || w2.LossUnitSource != UnitSourceCircuit {
		t.Fatalf("w2 unit=%q source=%q, want db_per_cm/circuit", w2.LossUnit, w2.LossUnitSource)
	}
	// 编译期归一化：w1 3 dB/m 保持 3；w2 3 dB/cm 折算为 300 dB/m。
	if got := c.devices[0].params["loss"]; got != 3 {
		t.Fatalf("w1 normalized loss = %v, want 3", got)
	}
	if got := c.devices[1].params["loss"]; got != 300 {
		t.Fatalf("w2 normalized loss = %v, want 300", got)
	}
}

func TestVersion12DefaultFallsBackToVersionDefault(t *testing.T) {
	c := loadDoc(t, waveguideDoc("1.2", "", "", 5))
	w := c.model.Circuit.Devices[0]
	if w.LossUnit != "db_per_m" || w.LossUnitSource != UnitSourceVersionDefault {
		t.Fatalf("unit=%q source=%q, want db_per_m/version_default", w.LossUnit, w.LossUnitSource)
	}
}

// 同一物理电路用 1.0 / 1.1 / 1.2 与器件级/电路级不同写法表达，
// 迁移后求解结果必须逐位一致（物理含义保持不变）。
func TestVersionMigrationPreservesPhysics(t *testing.T) {
	docs := []string{
		waveguideDoc("1.0", "", "", 200),                  // 1.0 缺省 db_per_m
		waveguideDoc("1.1", "db_per_m", "", 200),          // 1.1 电路级 db_per_m
		waveguideDoc("1.1", "db_per_cm", "", 2),           // 1.1 电路级 db_per_cm
		waveguideDoc("1.2", "db_per_cm", "", 2),           // 1.2 电路级
		waveguideDoc("1.2", "", "db_per_cm", 2),           // 1.2 器件级
		waveguideDoc("1.2", "db_per_m", "db_per_cm", 2),   // 1.2 器件级覆盖电路级
		waveguideDoc("1.2", "db_per_cm", "db_per_m", 200), // 1.2 器件级覆盖回 db_per_m
	}
	e, _ := NewEngine()
	var ref *BatchResult
	for i, doc := range docs {
		c := loadDoc(t, doc)
		res, err := e.Solve(context.Background(), c, SolveRequest{
			Batch:  []Excitation{{Port: "in0", Amplitude: 1}},
			Method: SolverCrossCheck,
		})
		if err != nil {
			t.Fatalf("doc %d solve: %v", i, err)
		}
		if ref == nil {
			ref = res
			continue
		}
		if !sameResult(ref.Results[0], res.Results[0]) {
			t.Fatalf("doc %d solve result differs from doc 0:\n%s\nvs\n%s",
				i, doc, docs[0])
		}
	}
}

func TestVersion12UnknownDeviceLossUnitRejected(t *testing.T) {
	doc := waveguideDoc("1.2", "", "parsecs", 1)
	if code := loadErrCode(t, doc); code != ErrInvalidParameter {
		t.Fatalf("want INVALID_PARAMETER, got %s", code)
	}
}

func TestVersion12LossUnitOnNonWaveguideRejected(t *testing.T) {
	doc := `{"version":"1.2","circuit":{"id":"x","wavelength":1.55,
	  "devices":[{"id":"ps","kind":"phaseshifter","loss_unit":"db_per_m","params":{}}],
	  "connects":[
	    {"from":{"external":"in0"},"to":{"device":"ps","port":"in"}},
	    {"from":{"device":"ps","port":"out"},"to":{"external":"out0"}}
	  ]}}`
	if code := loadErrCode(t, doc); code != ErrConflictingDeclaration {
		t.Fatalf("want CONFLICTING_DECLARATION, got %s", code)
	}
}

func TestDeviceLossUnitInOlderVersionsRejected(t *testing.T) {
	for _, v := range []string{"1.0", "1.1"} {
		doc := waveguideDoc(v, "", "db_per_cm", 1)
		if code := loadErrCode(t, doc); code != ErrModelMigration {
			t.Fatalf("version %s: want MODEL_MIGRATION, got %s", v, code)
		}
	}
}

func TestDuplicateKeyRejected(t *testing.T) {
	doc := `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,
	  "loss_unit":"db_per_m","loss_unit":"db_per_cm","devices":[],"connects":[]}}`
	if code := loadErrCode(t, doc); code != ErrConflictingDeclaration {
		t.Fatalf("want CONFLICTING_DECLARATION, got %s", code)
	}
	// 器件内重复键同样拒绝。
	doc2 := `{"version":"1.2","circuit":{"id":"x","wavelength":1.55,
	  "devices":[{"id":"w","id":"w2","kind":"waveguide","params":{"length":1,"neff":2}}],
	  "connects":[]}}`
	if code := loadErrCode(t, doc2); code != ErrConflictingDeclaration {
		t.Fatalf("device-level dup: want CONFLICTING_DECLARATION, got %s", code)
	}
}
