package solver

import (
	"context"
	"math"
	"math/cmplx"
	"testing"
)

func TestResonatorPassthroughAndPower(t *testing.T) {
	// 无内损耗 a=1 时，through/drop 功率之和必须为 1（全耦合无耗谐振腔）。
	doc := `{
	  "version": "1.1",
	  "circuit": {"id": "rr", "wavelength": 1.55,
	    "devices": [{"id": "r", "kind": "resonator",
	      "params": {"r_self": 0.9, "r_cross": 0.9, "a": 1.0, "phi_roundtrip": 0.5}}],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "r", "port": "in"}},
	      {"from": {"external": "add0"}, "to": {"device": "r", "port": "add"}},
	      {"from": {"device": "r", "port": "through"}, "to": {"external": "th"}},
	      {"from": {"device": "r", "port": "drop"}, "to": {"external": "dr"}}
	    ]
	  }
	}`
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	res, err := e.Solve(context.Background(), c, SolveRequest{
		Batch: []Excitation{{Port: "in0", Amplitude: 1}, {Port: "add0", Amplitude: 1}},
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	for i, r := range res.Results {
		var pTh, pDr float64
		for _, o := range r.Outputs {
			switch o.Port {
			case "th":
				pTh = o.Power
			case "dr":
				pDr = o.Power
			}
		}
		if math.Abs(pTh+pDr-1) > 1e-10 {
			t.Fatalf("case %d power sum = %v, want 1", i, pTh+pDr)
		}
	}
}

func TestResonatorResonanceCondition(t *testing.T) {
	// φ=0（共振）且对称耦合、无耗：through=0，全部进入 drop。
	doc := `{
	  "version": "1.1",
	  "circuit": {"id": "rr", "wavelength": 1.55,
	    "devices": [{"id": "r", "kind": "resonator",
	      "params": {"r_self": 0.95, "r_cross": 0.95, "a": 1.0, "phi_roundtrip": 0}}],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "r", "port": "in"}},
	      {"from": {"external": "add0"}, "to": {"device": "r", "port": "add"}},
	      {"from": {"device": "r", "port": "through"}, "to": {"external": "th"}},
	      {"from": {"device": "r", "port": "drop"}, "to": {"external": "dr"}}
	    ]
	  }
	}`
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	res, err := e.Solve(context.Background(), c, SolveRequest{
		Batch: []Excitation{{Port: "in0", Amplitude: 1}},
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	m := map[string]complex128{}
	for _, o := range res.Results[0].Outputs {
		m[o.Port] = o.Amplitude
	}
	if cmplx.Abs(m["th"]) > 1e-9 {
		t.Fatalf("on resonance through = %v, want 0", m["th"])
	}
	if math.Abs(cmplx.Abs(m["dr"])-1) > 1e-9 {
		t.Fatalf("on resonance drop = %v, want unit magnitude", m["dr"])
	}
}

func TestResonatorClosedFormAgrees(t *testing.T) {
	// 与闭式公式直接比对，验证器件矩阵的解析约定。
	r1, r2, a, phi := 0.88, 0.92, 0.97, 1.1
	doc := `{
	  "version": "1.1",
	  "circuit": {"id": "rr", "wavelength": 1.55,
	    "devices": [{"id": "r", "kind": "resonator",
	      "params": {"r_self": 0.88, "r_cross": 0.92, "a": 0.97, "phi_roundtrip": 1.1}}],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "r", "port": "in"}},
	      {"from": {"external": "add0"}, "to": {"device": "r", "port": "add"}},
	      {"from": {"device": "r", "port": "through"}, "to": {"external": "th"}},
	      {"from": {"device": "r", "port": "drop"}, "to": {"external": "dr"}}
	    ]
	  }
	}`
	e, _ := NewEngine()
	c, _ := e.LoadModel(context.Background(), []byte(doc))
	res, err := e.Solve(context.Background(), c, SolveRequest{
		Batch: []Excitation{{Port: "in0", Amplitude: 1}}, Method: SolverGlobal,
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	ejp := cmplx.Exp(complex(0, phi))
	k12 := math.Sqrt((1 - r1*r1) * (1 - r2*r2))
	denom := 1 - complex(r1*r2*a, 0)*ejp
	wantTh := (complex(r1, 0) - complex(r2*a, 0)*ejp) / denom
	wantDr := complex(-k12*a, 0) / denom
	_ = k12
	for _, o := range res.Results[0].Outputs {
		var want complex128
		switch o.Port {
		case "th":
			want = wantTh
		case "dr":
			want = wantDr
		}
		if cmplx.Abs(o.Amplitude-want) > 1e-12 {
			t.Fatalf("port %s = %v, closed form = %v", o.Port, o.Amplitude, want)
		}
	}
}
