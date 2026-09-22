package solver_test

import (
	"context"
	"fmt"

	"github.com/h3yow/photonic-circuit-solver-go/solver"
)

// ExampleNewEngine 演示载入模型、交叉校验求解与读取输出。
// L=λ 且 n_eff=1 时相位恰为 2π，输出振幅为 1。
func ExampleNewEngine() {
	doc := `{
	  "version": "1.0",
	  "circuit": {"id": "wg", "wavelength": 1.55,
	    "devices": [{"id": "w", "kind": "waveguide", "params": {"length": 1.55, "neff": 1.0}}],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "w", "port": "in"}},
	      {"from": {"device": "w", "port": "out"}, "to": {"external": "out0"}}
	    ]
	  }
	}`
	eng, err := solver.NewEngine()
	if err != nil {
		panic(err)
	}
	circuit, err := eng.LoadModel(context.Background(), []byte(doc))
	if err != nil {
		panic(err)
	}
	res, err := eng.Solve(context.Background(), circuit, solver.SolveRequest{
		Batch:  []solver.Excitation{{Port: "in0", Amplitude: complex(1, 0)}},
		Phases: nil,
		Method: solver.SolverCrossCheck,
	})
	if err != nil {
		panic(err)
	}
	for _, out := range res.Results[0].Outputs {
		fmt.Printf("%s power=%.3f\n", out.Port, out.Power)
	}
	// Output: out0 power=1.000
}
