package solver

import (
	"context"
	"math"
	"testing"
)

func TestSmokeWaveguide(t *testing.T) {
	doc := `{
	  "version": "1.0",
	  "circuit": {
	    "id": "wg1",
	    "wavelength": 1.55,
	    "devices": [
	      {"id": "w", "kind": "waveguide", "params": {"length": 10.0, "neff": 2.0}}
	    ],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "w", "port": "in"}},
	      {"from": {"device": "w", "port": "out"}, "to": {"external": "out0"}}
	    ]
	  }
	}`
	e, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	c, err := e.LoadModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	for _, method := range []SolverMethod{SolverTopological, SolverGlobal, SolverCrossCheck} {
		res, err := e.Solve(context.Background(), c, SolveRequest{
			Batch:  []Excitation{{Port: "in0", Amplitude: 1}},
			Method: method,
		})
		if err != nil {
			t.Fatalf("Solve %s: %v", method, err)
		}
		out := res.Results[0].Outputs[0]
		if math.Abs(out.Power-1) > 1e-12 {
			t.Fatalf("%s power = %v, want 1", method, out.Power)
		}
		want := 2 * math.Pi * 2 * 10 / 1.55
		got := math.Atan2(imag(out.Amplitude), real(out.Amplitude))
		wantWrapped := math.Mod(want+math.Pi, 2*math.Pi) - math.Pi
		if math.Abs(got-wantWrapped) > 1e-12 {
			t.Fatalf("%s phase = %v, want %v", method, got, wantWrapped)
		}
		t.Logf("%s: amplitude=%v power=%v", method, out.Amplitude, out.Power)
	}
}
