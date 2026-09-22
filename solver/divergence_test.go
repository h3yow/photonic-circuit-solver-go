package solver

import (
	"context"
	"strings"
	"testing"
)

func TestCrossCheckReportsDivergence(t *testing.T) {
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

	// 正常情况下交叉校验通过。
	if _, err := e.Solve(context.Background(), c, SolveRequest{
		Batch:  []Excitation{{Port: "in0", Amplitude: 1}},
		Method: SolverCrossCheck,
	}); err != nil {
		t.Fatalf("expected agreement, got %v", err)
	}

	// 注入"拓扑途径"确定性的乘法扰动，使两途径结果超差。
	c.testTopologicalScale = complex(2.0, 0)
	_, err = e.Solve(context.Background(), c, SolveRequest{
		Batch:  []Excitation{{Port: "in0", Amplitude: 1}},
		Method: SolverCrossCheck,
	})
	if !IsCode(err, ErrSolverDivergence) {
		t.Fatalf("want SOLVER_DIVERGENCE, got %v", err)
	}
	se, _ := AsSolverError(err)
	// 判定依据必须包含输入与两个途径各自的结果
	joined := se.Error()
	if !strings.Contains(joined, "port out0") && !strings.Contains(se.Detail["reason"].(string), "port") {
		t.Fatalf("divergence error lacks port-level reason: %v", se.Detail)
	}
	if se.Detail["global_result"] == nil || se.Detail["topological_result"] == nil {
		t.Fatalf("divergence error must carry both solver results: %v", se.Detail)
	}
	if se.Detail["input_amplitude"] == nil {
		t.Fatalf("divergence error must carry the input evidence: %v", se.Detail)
	}
	t.Logf("correctly reported divergence: %v", se.Detail["reason"])
}
