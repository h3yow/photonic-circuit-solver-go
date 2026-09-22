package solver

import (
	"context"
	"math"
	"testing"
)

func mziDoc() string {
	// in0 -> bs1.a ; bs1.c -> wA -> ps.in ; bs1.d -> wB -> ... ; 合束 bs2
	return `{
	  "version": "1.1",
	  "circuit": {
	    "id": "mzi",
	    "wavelength": 1.55,
	    "loss_unit": "db_per_m",
	    "devices": [
	      {"id": "bs1", "kind": "beamsplitter", "params": {}},
	      {"id": "bs2", "kind": "beamsplitter", "params": {}},
	      {"id": "wA", "kind": "waveguide", "params": {"length": 15.5, "neff": 2.0}},
	      {"id": "wB", "kind": "waveguide", "params": {"length": 15.5, "neff": 2.0}},
	      {"id": "ps", "kind": "phaseshifter", "params": {"u0": 0, "k": 1}}
	    ],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "bs1", "port": "a"}},
	      {"from": {"external": "in1"}, "to": {"device": "bs1", "port": "b"}},
	      {"from": {"device": "bs1", "port": "c"}, "to": {"device": "wA", "port": "in"}},
	      {"from": {"device": "wA", "port": "out"}, "to": {"device": "ps", "port": "in"}},
	      {"from": {"device": "ps", "port": "out"}, "to": {"device": "bs2", "port": "a"}},
	      {"from": {"device": "bs1", "port": "d"}, "to": {"device": "wB", "port": "in"}},
	      {"from": {"device": "wB", "port": "out"}, "to": {"device": "bs2", "port": "b"}},
	      {"from": {"device": "bs2", "port": "c"}, "to": {"external": "out0"}},
	      {"from": {"device": "bs2", "port": "d"}, "to": {"external": "out1"}}
	    ]
	  }
	}`
}

func TestMZIInterference(t *testing.T) {
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(mziDoc()))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	powers := func(phi float64) (float64, float64) {
		res, err := e.Solve(context.Background(), c, SolveRequest{
			Batch:  []Excitation{{Port: "in0", Amplitude: 1}},
			Phases: map[string]float64{"ps": phi},
			Method: SolverCrossCheck,
		})
		if err != nil {
			t.Fatalf("Solve phi=%v: %v", phi, err)
		}
		m := map[string]float64{}
		for _, o := range res.Results[0].Outputs {
			m[o.Port] = o.Power
		}
		return m["out0"], m["out1"]
	}
	// 两臂等长，phi=0 时全部从 c 端出（实 Hadamard：c*c+s*s=1）
	p0, p1 := powers(0)
	if math.Abs(p0-1) > 1e-12 || math.Abs(p1) > 1e-12 {
		t.Fatalf("phi=0: out0=%v out1=%v expected (1,0)", p0, p1)
	}
	// phi=π 时交换
	p0, p1 = powers(math.Pi)
	if math.Abs(p0) > 1e-12 || math.Abs(p1-1) > 1e-12 {
		t.Fatalf("phi=pi: out0=%v out1=%v expected (0,1)", p0, p1)
	}
	// 功率守恒
	for _, phi := range []float64{0.3, 1.1, 2.4} {
		p0, p1 = powers(phi)
		if math.Abs(p0+p1-1) > 1e-12 {
			t.Fatalf("phi=%v: power not conserved: %v+%v", phi, p0, p1)
		}
	}
}

func TestBeamsplitterUnitarity(t *testing.T) {
	doc := `{
	  "version": "1.0",
	  "circuit": {"id": "bs", "wavelength": 1.55,
	    "devices": [{"id": "b", "kind": "beamsplitter", "params": {"reflectivity": 0.3}}],
	    "connects": [
	      {"from": {"external": "a"}, "to": {"device": "b", "port": "a"}},
	      {"from": {"external": "b"}, "to": {"device": "b", "port": "b"}},
	      {"from": {"device": "b", "port": "c"}, "to": {"external": "c"}},
	      {"from": {"device": "b", "port": "d"}, "to": {"external": "d"}}
	    ]
	  }
	}`
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	res, err := e.Solve(context.Background(), c, SolveRequest{
		Batch: []Excitation{
			{Port: "a", Amplitude: complex(1, 0)},
			{Port: "b", Amplitude: complex(0, 1)},
		},
		Method: SolverGlobal,
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	for i, r := range res.Results {
		sum := 0.0
		for _, o := range r.Outputs {
			sum += o.Power
		}
		if math.Abs(sum-1) > 1e-12 {
			t.Fatalf("case %d power sum=%v want 1", i, sum)
		}
	}
}
