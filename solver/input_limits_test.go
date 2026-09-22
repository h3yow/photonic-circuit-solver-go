package solver

import (
	"context"
	"math"
	"testing"
)

func solveWG(t *testing.T) (*Engine, *CompiledCircuit) {
	t.Helper()
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
	return e, c
}

func TestRejectNonFiniteExcitation(t *testing.T) {
	e, c := solveWG(t)
	for _, a := range []complex128{complex(math.NaN(), 0), complex(0, math.Inf(1)), complex(math.Inf(-1), 0)} {
		_, err := e.Solve(context.Background(), c, SolveRequest{
			Batch: []Excitation{{Port: "in0", Amplitude: a}},
		})
		if !IsCode(err, ErrNonFiniteInput) {
			t.Fatalf("amplitude=%v: want NON_FINITE_INPUT, got %v", a, err)
		}
	}
}

func TestRejectOverBoundExcitation(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxInputAmplitude = 10
	e, _ := NewEngine(WithLimits(lim))
	_, c := solveWG(t)
	// 重新用该引擎加载（上限在 LoadModel 也生效）
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
	c2, err := e.LoadModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	_ = c
	_, err = e.Solve(context.Background(), c2, SolveRequest{
		Batch: []Excitation{{Port: "in0", Amplitude: 11}},
	})
	if !IsCode(err, ErrOutOfBounds) {
		t.Fatalf("want OUT_OF_BOUNDS for amplitude 11>10, got %v", err)
	}
	// 边界值本身必须接受（不静默截断）
	if _, err := e.Solve(context.Background(), c2, SolveRequest{
		Batch: []Excitation{{Port: "in0", Amplitude: 10}},
	}); err != nil {
		t.Fatalf("amplitude exactly at bound should be accepted, got %v", err)
	}
}

func TestRejectUnknownExcitationPort(t *testing.T) {
	e, c := solveWG(t)
	_, err := e.Solve(context.Background(), c, SolveRequest{
		Batch: []Excitation{{Port: "nope", Amplitude: 1}},
	})
	if !IsCode(err, ErrPortMissing) {
		t.Fatalf("want PORT_MISSING, got %v", err)
	}
}

func TestRejectEmptyBatch(t *testing.T) {
	e, c := solveWG(t)
	_, err := e.Solve(context.Background(), c, SolveRequest{})
	if !IsCode(err, ErrInvalidParameter) {
		t.Fatalf("want INVALID_PARAMETER, got %v", err)
	}
}

func TestRejectBatchOverLimit(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxBatchSize = 2
	e, _ := NewEngine(WithLimits(lim))
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
	c, err := e.LoadModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	batch := []Excitation{{Port: "in0", Amplitude: 1}, {Port: "in0", Amplitude: 1}, {Port: "in0", Amplitude: 1}}
	_, err = e.Solve(context.Background(), c, SolveRequest{Batch: batch})
	if !IsCode(err, ErrLimitExceeded) {
		t.Fatalf("want LIMIT_EXCEEDED, got %v", err)
	}
}

func TestRejectConflictingShifterSetting(t *testing.T) {
	doc := `{
	  "version": "1.1",
	  "circuit": {"id": "x", "wavelength": 1.55,
	    "devices": [{"id": "ps", "kind": "phaseshifter", "params": {"u0": 0, "k": 1}}],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "ps", "port": "in"}},
	      {"from": {"device": "ps", "port": "out"}, "to": {"external": "out0"}}
	    ]
	  }
	}`
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	_, err = e.Solve(context.Background(), c, SolveRequest{
		Batch:    []Excitation{{Port: "in0", Amplitude: 1}},
		Controls: map[string]float64{"ps": 0.5},
		Phases:   map[string]float64{"ps": 0.5},
	})
	if !IsCode(err, ErrInvalidParameter) {
		t.Fatalf("want INVALID_PARAMETER for dual setting, got %v", err)
	}
}

func TestRejectDeviceOverLimit(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxDevices = 1
	e, _ := NewEngine(WithLimits(lim))
	doc := `{
	  "version": "1.0",
	  "circuit": {"id": "x", "wavelength": 1.55,
	    "devices": [
	      {"id": "w1", "kind": "waveguide", "params": {"length": 1, "neff": 2}},
	      {"id": "w2", "kind": "waveguide", "params": {"length": 1, "neff": 2}}
	    ],
	    "connects": []
	  }
	}`
	_, err := e.LoadModel(context.Background(), []byte(doc))
	if !IsCode(err, ErrLimitExceeded) {
		t.Fatalf("want LIMIT_EXCEEDED, got %v", err)
	}
}

func TestInvalidPhysicalParamsRejected(t *testing.T) {
	cases := map[string]string{
		"negative wavelength": `{"version":"1.1","circuit":{"id":"x","wavelength":-1.55,"devices":[],"connects":[]}}`,
		"negative neff":       `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"devices":[{"id":"w","kind":"waveguide","params":{"length":1,"neff":-2}}],"connects":[]}}`,
		"negative length":     `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"devices":[{"id":"w","kind":"waveguide","params":{"length":-1,"neff":2}}],"connects":[]}}`,
		"missing length":      `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"devices":[{"id":"w","kind":"waveguide","params":{"neff":2}}],"connects":[]}}`,
		"r>1 beamsplitter":    `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"devices":[{"id":"b","kind":"beamsplitter","params":{"reflectivity":1.2}}],"connects":[]}}`,
		"resonator r bounds":  `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"devices":[{"id":"r","kind":"resonator","params":{"r_self":0.9999999999999,"r_cross":0.9999999999999,"a":1.0,"phi_roundtrip":0}}],"connects":[]}}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			e, _ := NewEngine()
			_, err := e.LoadModel(context.Background(), []byte(doc))
			if !IsCode(err, ErrInvalidParameter) {
				t.Fatalf("want INVALID_PARAMETER, got %v\ninput: %s", err, doc)
			}
		})
	}
}
