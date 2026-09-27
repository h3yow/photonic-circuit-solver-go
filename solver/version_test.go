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
