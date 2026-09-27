package solver

import (
	"context"
	"testing"
)

func loadErrCode(t *testing.T, doc string) ErrorCode {
	t.Helper()
	e, _ := NewEngine()
	_, err := e.LoadModel(context.Background(), []byte(doc))
	if err == nil {
		t.Fatalf("expected error but model accepted.\ninput:\n%s", doc)
	}
	se, ok := AsSolverError(err)
	if !ok {
		t.Fatalf("not SolverError: %v\ninput:\n%s", err, doc)
	}
	return se.Code
}

func TestVersion10DefaultsToLossDBPerM(t *testing.T) {
	doc := `{
	  "version": "1.0",
	  "circuit": {"id": "x", "wavelength": 1.55,
	    "devices": [{"id": "w", "kind": "waveguide", "params": {"length": 1, "neff": 2}}],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "w", "port": "in"}},
	      {"from": {"device": "w", "port": "out"}, "to": {"external": "out0"}}
	    ]
	  }
	}`
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	if got := c.model.Version; got != "1.2" {
		t.Fatalf("internal version = %q, want migrated 1.2", got)
	}
	if got := c.model.DeclaredVersion; got != "1.0" {
		t.Fatalf("declared version = %q, want 1.0", got)
	}
	if got := c.model.Circuit.LossUnit; got != "db_per_m" {
		t.Fatalf("loss unit = %q, want db_per_m default", got)
	}
}

func TestVersion11ExplicitLossUnit(t *testing.T) {
	doc := `{
	  "version": "1.1",
	  "circuit": {"id": "x", "wavelength": 1.55, "loss_unit": "db_per_cm",
	    "devices": [{"id": "w", "kind": "waveguide", "params": {"length": 1, "neff": 2, "loss": 0}}],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "w", "port": "in"}},
	      {"from": {"device": "w", "port": "out"}, "to": {"external": "out0"}}
	    ]
	  }
	}`
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	if got := c.model.Circuit.LossUnit; got != "db_per_cm" {
		t.Fatalf("loss unit = %q, want db_per_cm", got)
	}
}

func TestVersion10WithFutureFieldRejected(t *testing.T) {
	doc := `{
	  "version": "1.0",
	  "circuit": {"id": "x", "wavelength": 1.55, "loss_unit": "db_per_m",
	    "devices": [],
	    "connects": []
	  }
	}`
	if code := loadErrCode(t, doc); code != ErrModelMigration {
		t.Fatalf("want MODEL_MIGRATION, got %s", code)
	}
}

func TestUnsupportedVersion(t *testing.T) {
	for _, v := range []string{"2.0", "1.3", "0.9", "abc", ""} {
		doc := `{"version":"` + v + `","circuit":{"id":"x","wavelength":1.55,"devices":[],"connects":[]}}`
		if code := loadErrCode(t, doc); code != ErrVersionUnsupported {
			t.Fatalf("version %q: want VERSION_UNSUPPORTED, got %s", v, code)
		}
	}
}

func TestUnknownTopLevelFieldRejected(t *testing.T) {
	doc := `{
	  "version": "1.1",
	  "circuit": {"id": "x", "wavelength": 1.55, "devices": [], "connects": []},
	  "mystery": 1
	}`
	if code := loadErrCode(t, doc); code != ErrUnknownField {
		t.Fatalf("want UNKNOWN_FIELD, got %s", code)
	}
}

func TestUnknownDeviceParamRejected(t *testing.T) {
	doc := `{
	  "version": "1.1",
	  "circuit": {"id": "x", "wavelength": 1.55,
	    "devices": [{"id": "w", "kind": "waveguide",
	      "params": {"length": 1, "neff": 2, "bogus": 3}}],
	    "connects": []
	  }
	}`
	if code := loadErrCode(t, doc); code != ErrUnknownField {
		t.Fatalf("want UNKNOWN_FIELD, got %s", code)
	}
}

func TestUnknownDeviceKindRejected(t *testing.T) {
	doc := `{
	  "version": "1.1",
	  "circuit": {"id": "x", "wavelength": 1.55,
	    "devices": [{"id": "w", "kind": "teleporter", "params": {}}],
	    "connects": []
	  }
	}`
	if code := loadErrCode(t, doc); code != ErrInvalidParameter {
		t.Fatalf("want INVALID_PARAMETER, got %s", code)
	}
}

func TestMissingWavelengthRejected(t *testing.T) {
	doc := `{
	  "version": "1.1",
	  "circuit": {"id": "x",
	    "devices": [{"id": "w", "kind": "waveguide", "params": {"length": 1, "neff": 2}}],
	    "connects": []
	  }
	}`
	if code := loadErrCode(t, doc); code != ErrInvalidParameter {
		t.Fatalf("want INVALID_PARAMETER, got %s", code)
	}
}

func TestInvalidLossUnitRejected(t *testing.T) {
	doc := `{
	  "version": "1.1",
	  "circuit": {"id": "x", "wavelength": 1.55, "loss_unit": "furlongs",
	    "devices": [], "connects": []
	  }
	}`
	if code := loadErrCode(t, doc); code != ErrInvalidParameter {
		t.Fatalf("want INVALID_PARAMETER, got %s", code)
	}
}

func TestTrailingDataRejected(t *testing.T) {
	doc := `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"devices":[],"connects":[]}} garbage`
	if code := loadErrCode(t, doc); code != ErrUnknownField && code != ErrInvalidParameter {
		t.Fatalf("want UNKNOWN_FIELD or INVALID_PARAMETER, got %s", code)
	}
}

func TestVersion12DeviceLossUnit(t *testing.T) {
	doc := `{
	  "version": "1.2",
	  "circuit": {"id": "x", "wavelength": 1.55, "loss_unit": "db_per_m",
	    "devices": [
	      {"id": "w1", "kind": "waveguide", "params": {"length": 1, "neff": 2, "loss": 2}, "loss_unit": "db_per_cm"},
	      {"id": "w2", "kind": "waveguide", "params": {"length": 1, "neff": 2, "loss": 3}}
	    ],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "w1", "port": "in"}},
	      {"from": {"device": "w1", "port": "out"}, "to": {"device": "w2", "port": "in"}},
	      {"from": {"device": "w2", "port": "out"}, "to": {"external": "out0"}}
	    ]
	  }
	}`
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	w1 := c.model.Circuit.Devices[0]
	w2 := c.model.Circuit.Devices[1]
	if w1.LossUnit != "db_per_cm" || w1.LossUnitSource != SourceDeviceDeclared {
		t.Fatalf("w1 unit = %q (%s), want db_per_cm (device_declared)", w1.LossUnit, w1.LossUnitSource)
	}
	if w2.LossUnit != "db_per_m" || w2.LossUnitSource != SourceCircuitDeclared {
		t.Fatalf("w2 unit = %q (%s), want db_per_m (circuit_declared)", w2.LossUnit, w2.LossUnitSource)
	}
	// 归一化：w1 的 2 dB/cm 应折算为 200 dB/m；w2 保持 3 dB/m。
	if got := c.devices[0].params["loss"]; got != 200 {
		t.Fatalf("w1 normalized loss = %v, want 200 (db_per_m)", got)
	}
	if got := c.devices[1].params["loss"]; got != 3 {
		t.Fatalf("w2 normalized loss = %v, want 3 (db_per_m)", got)
	}
}

// 迁移物理不变性：1.0（隐式 db_per_m）、1.1（电路级 db_per_m）、
// 1.2（器件级 db_per_cm 且数值相应缩小 100 倍）三份文档求解结果必须逐位一致。
func TestVersionMigrationPreservesPhysics(t *testing.T) {
	docs := []string{
		`{"version":"1.0","circuit":{"id":"x","wavelength":1.55,
		  "devices":[{"id":"w","kind":"waveguide","params":{"length":2,"neff":2,"loss":150}}],
		  "connects":[{"from":{"external":"in0"},"to":{"device":"w","port":"in"}},
		              {"from":{"device":"w","port":"out"},"to":{"external":"out0"}}]}}`,
		`{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"loss_unit":"db_per_m",
		  "devices":[{"id":"w","kind":"waveguide","params":{"length":2,"neff":2,"loss":150}}],
		  "connects":[{"from":{"external":"in0"},"to":{"device":"w","port":"in"}},
		              {"from":{"device":"w","port":"out"},"to":{"external":"out0"}}]}}`,
		`{"version":"1.2","circuit":{"id":"x","wavelength":1.55,
		  "devices":[{"id":"w","kind":"waveguide","params":{"length":2,"neff":2,"loss":1.5},"loss_unit":"db_per_cm"}],
		  "connects":[{"from":{"external":"in0"},"to":{"device":"w","port":"in"}},
		              {"from":{"device":"w","port":"out"},"to":{"external":"out0"}}]}}`,
	}
	e, _ := NewEngine()
	var ref *BatchResult
	for i, doc := range docs {
		c, err := e.LoadModel(context.Background(), []byte(doc))
		if err != nil {
			t.Fatalf("doc %d LoadModel: %v", i, err)
		}
		got, err := e.Solve(context.Background(), c, SolveRequest{
			Batch: []Excitation{{Port: "in0", Amplitude: 1}},
		})
		if err != nil {
			t.Fatalf("doc %d Solve: %v", i, err)
		}
		if ref == nil {
			ref = got
			continue
		}
		if !batchResultsEqual(ref, got) {
			t.Fatalf("doc %d solve result differs from doc 0:\nref=%+v\ngot=%+v", i, ref, got)
		}
	}
}

func batchResultsEqual(a, b *BatchResult) bool {
	if len(a.Results) != len(b.Results) {
		return false
	}
	for i := range a.Results {
		ra, rb := a.Results[i], b.Results[i]
		if len(ra.Outputs) != len(rb.Outputs) {
			return false
		}
		for j := range ra.Outputs {
			if ra.Outputs[j] != rb.Outputs[j] {
				return false
			}
		}
	}
	return true
}

func TestVersion11WithDeviceLossUnitRejected(t *testing.T) {
	for _, v := range []string{"1.0", "1.1"} {
		doc := `{"version":"` + v + `","circuit":{"id":"x","wavelength":1.55,
		  "devices":[{"id":"w","kind":"waveguide","params":{"length":1,"neff":2},"loss_unit":"db_per_cm"}],
		  "connects":[]}}`
		if code := loadErrCode(t, doc); code != ErrModelMigration {
			t.Fatalf("version %s: want MODEL_MIGRATION, got %s", v, code)
		}
	}
}

func TestVersion12UnknownDeviceLossUnitRejected(t *testing.T) {
	doc := `{"version":"1.2","circuit":{"id":"x","wavelength":1.55,
	  "devices":[{"id":"w","kind":"waveguide","params":{"length":1,"neff":2},"loss_unit":"db_per_km"}],
	  "connects":[]}}`
	if code := loadErrCode(t, doc); code != ErrInvalidParameter {
		t.Fatalf("want INVALID_PARAMETER, got %s", code)
	}
}

func TestVersion12LossUnitOnNonWaveguideRejected(t *testing.T) {
	doc := `{"version":"1.2","circuit":{"id":"x","wavelength":1.55,
	  "devices":[{"id":"p","kind":"phaseshifter","params":{},"loss_unit":"db_per_m"}],
	  "connects":[]}}`
	if code := loadErrCode(t, doc); code != ErrInvalidParameter {
		t.Fatalf("want INVALID_PARAMETER, got %s", code)
	}
}

func TestDuplicateKeyRejected(t *testing.T) {
	// 同一处相互冲突的声明：同一对象内重复 loss_unit 键。
	doc := `{"version":"1.2","circuit":{"id":"x","wavelength":1.55,
	  "devices":[{"id":"w","kind":"waveguide","params":{"length":1,"neff":2},
	    "loss_unit":"db_per_m","loss_unit":"db_per_cm"}],
	  "connects":[]}}`
	if code := loadErrCode(t, doc); code != ErrInvalidParameter {
		t.Fatalf("want INVALID_PARAMETER, got %s", code)
	}
}
