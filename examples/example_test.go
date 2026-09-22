// Package examples holds runnable usage demos for the engine.
package examples_test

import (
	"fmt"
	"os"

	"github.com/h3yow/photonic-circuit-solver-go/calibration"
	"github.com/h3yow/photonic-circuit-solver-go/engine"
	"github.com/h3yow/photonic-circuit-solver-go/solver"
)

// ExampleMZI demonstrates load, cross-checked solve, phase calibration and
// solving again with the calibrated offset.
func Example() {
	raw, err := os.ReadFile("mzi.json")
	if err != nil {
		panic(err)
	}
	eng, err := engine.New(engine.DefaultConfig())
	if err != nil {
		panic(err)
	}
	key, err := eng.Load(raw)
	if err != nil {
		panic(err)
	}

	res, err := eng.SolveOne(key, engine.SolveRequest{
		Method:     solver.MethodLinear,
		CrossCheck: solver.MethodJacobi,
		Request: solver.Request{
			Excitations: []solver.Excitation{{DeviceID: "b1", Port: "in1", Amplitude: 1}},
			Probes: []solver.Probe{
				{DeviceID: "b2", Port: "out1"},
				{DeviceID: "b2", Port: "out2"},
			},
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("baseline powers: %.3f, %.3f\n",
		abs2(res.Outputs[0]), abs2(res.Outputs[1]))

	rec, err := eng.Calibrate(key, calibration.Request{
		PhaseShifter: "ps",
		Excitations:  []solver.Excitation{{DeviceID: "b1", Port: "in1", Amplitude: 1}},
		Probe:        solver.Probe{DeviceID: "b2", Port: "out1"},
		Reference:    solver.Probe{DeviceID: "b1", Port: "in1"},
		TargetDelta:  1.0,
		Low:          0.05, High: 6.23,
	})
	if err != nil {
		panic(err)
	}

	calibrated, err := eng.SolveOneWithOffsets(key,
		map[string]float64{"ps": rec.Offset},
		engine.SolveRequest{
			Method: solver.MethodSweep,
			Request: solver.Request{
				Excitations: []solver.Excitation{{DeviceID: "b1", Port: "in1", Amplitude: 1}},
				Probes:      []solver.Probe{{DeviceID: "b2", Port: "out1"}},
			},
		})
	if err != nil {
		panic(err)
	}
	fmt.Printf("calibrated |out1|^2 = %.3f\n", abs2(calibrated.Outputs[0]))
	// Output:
	// baseline powers: 0.000, 1.000
	// calibrated |out1|^2 = 0.292
}

func abs2(z complex128) float64 {
	r, i := real(z), imag(z)
	return r*r + i*i
}
