package calibration

import (
	"math"
	"testing"

	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
	"github.com/h3yow/photonic-circuit-solver-go/model"
	"github.com/h3yow/photonic-circuit-solver-go/solver"
	"github.com/h3yow/photonic-circuit-solver-go/topology"
)

// A bare phase shifter gives full 2*pi controllability.
const lineJSON = `{
  "version":"1.1",
  "devices":[{"id":"ps","type":"phase_shifter","params":{"bias":0}}],
  "connections":[],
  "external_ports":[{"device":"ps","port":"in"},{"device":"ps","port":"out"}]
}`

// MZI output has dark/null points limiting the attainable phase.
const mziJSON = `{
  "version":"1.1",
  "devices":[
    {"id":"b1","type":"beam_splitter","params":{}},
    {"id":"ps","type":"phase_shifter","params":{"bias":0}},
    {"id":"wg","type":"waveguide","params":{"phase":0}},
    {"id":"b2","type":"beam_splitter","params":{}}
  ],
  "connections":[
    {"a":{"device":"b1","port":"out1"},"b":{"device":"ps","port":"in"}},
    {"a":{"device":"ps","port":"out"},"b":{"device":"b2","port":"in1"}},
    {"a":{"device":"b1","port":"out2"},"b":{"device":"wg","port":"in"}},
    {"a":{"device":"wg","port":"out"},"b":{"device":"b2","port":"in2"}}
  ],
  "external_ports":[
    {"device":"b1","port":"in1"},{"device":"b1","port":"in2"},
    {"device":"b2","port":"out1"},{"device":"b2","port":"out2"}
  ]
}`

func calibrator(t *testing.T, json string) *Calibrator {
	t.Helper()
	m, err := model.Decode([]byte(json))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	c, err := topology.Compile(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	sv, err := solver.New(c, solver.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	return New(sv, DefaultConfig())
}

func lineReq(target, lo, hi float64) Request {
	return Request{
		PhaseShifter: "ps",
		Excitations:  []solver.Excitation{{DeviceID: "ps", Port: "in", Amplitude: 1}},
		Probe:        solver.Probe{DeviceID: "ps", Port: "out"},
		Reference:    solver.Probe{DeviceID: "ps", Port: "in"},
		TargetDelta:  target,
		Low:          lo, High: hi,
	}
}

func TestCalibrate_ConvergesAcrossPhaseRange(t *testing.T) {
	c := calibrator(t, lineJSON)
	for _, target := range []float64{-3, -2, -1, -0.3, 0, 0.5, 1.2, 2.9} {
		req := lineReq(target, -math.Pi, math.Pi)
		res, err := c.Calibrate(req)
		if err != nil {
			t.Fatalf("target=%.3f: %v", target, err)
		}
		if d := wrapPhase(res.AchievedDelta - target); math.Abs(d) > 1e-7 {
			t.Fatalf("target=%.3f achieved=%.9f residual=%.3e exceeds PhaseTol",
				target, res.AchievedDelta, d)
		}
	}
}

func TestCalibrate_Reproducible(t *testing.T) {
	c := calibrator(t, lineJSON)
	req := lineReq(1.234567, -math.Pi, math.Pi)
	r1, err := c.Calibrate(req)
	if err != nil {
		t.Fatal(err)
	}
	for k := 0; k < 5; k++ {
		r2, err := c.Calibrate(req)
		if err != nil {
			t.Fatal(err)
		}
		if r1.Offset != r2.Offset {
			t.Fatalf("calibration not reproducible: %.15f vs %.15f", r1.Offset, r2.Offset)
		}
	}
}

func TestCalibrate_OutOfRangeRejected(t *testing.T) {
	c := calibrator(t, lineJSON)
	// Supported offset interval [0.1,0.4] can only reach ~0.3 rad.
	req := lineReq(2.5, 0.1, 0.4)
	_, err := c.Calibrate(req)
	if got := engerrors.KindOf(err); got != engerrors.KindCalibrationRange {
		t.Fatalf("target=2.5 over [0.1,0.4] must be out_of_range, input=%+v got %v", req, err)
	}
}

func TestCalibrate_DarkProbeNoSolution(t *testing.T) {
	c := calibrator(t, mziJSON)
	// Interval centered on the MZI null (offset 0) where out1 is dark.
	req := Request{
		PhaseShifter: "ps",
		Excitations:  []solver.Excitation{{DeviceID: "b1", Port: "in1", Amplitude: 1}},
		Probe:        solver.Probe{DeviceID: "b2", Port: "out1"},
		Reference:    solver.Probe{DeviceID: "b1", Port: "in1"},
		TargetDelta:  1.0,
		Low:          -1e-6, High: 1e-6,
	}
	_, err := c.Calibrate(req)
	got := engerrors.KindOf(err)
	if got != engerrors.KindCalibrationNoSolve && got != engerrors.KindCalibrationRange {
		t.Fatalf("dark/null probe must be no_solution or range (phase undefined), got %v", err)
	}
}

func TestCalibrate_MissingShifterRejected(t *testing.T) {
	c := calibrator(t, mziJSON)
	req := Request{
		PhaseShifter: "absent-ps",
		Excitations:  []solver.Excitation{{DeviceID: "b1", Port: "in1", Amplitude: 1}},
		Probe:        solver.Probe{DeviceID: "b2", Port: "out2"},
		Reference:    solver.Probe{DeviceID: "b1", Port: "in1"},
		TargetDelta:  0.0,
		Low:          0.1, High: 1.0,
	}
	_, err := c.Calibrate(req)
	if got := engerrors.KindOf(err); got != engerrors.KindMissingPort {
		t.Fatalf("missing shifter must be missing_port, input=%+v got %v", req, err)
	}
}

func TestCalibrate_WrongDeviceTypeRejected(t *testing.T) {
	c := calibrator(t, mziJSON)
	req := Request{
		PhaseShifter: "wg", // a waveguide, not a phase shifter
		Excitations:  []solver.Excitation{{DeviceID: "b1", Port: "in1", Amplitude: 1}},
		Probe:        solver.Probe{DeviceID: "b2", Port: "out1"},
		Reference:    solver.Probe{DeviceID: "b1", Port: "in1"},
		TargetDelta:  1.0,
		Low:          0.05, High: 1.0,
	}
	_, err := c.Calibrate(req)
	if got := engerrors.KindOf(err); got != engerrors.KindInvalidParameter {
		t.Fatalf("calibrating a waveguide must be invalid_parameter, input=%+v got %v", req, err)
	}
}

func TestCalibrate_InvalidRequests(t *testing.T) {
	c := calibrator(t, lineJSON)
	base := lineReq(1, -math.Pi, math.Pi)
	cases := []struct {
		name string
		mut  func(*Request)
		want engerrors.Kind
	}{
		{"no shifter", func(r *Request) { r.PhaseShifter = "" }, engerrors.KindInvalidParameter},
		{"no excitation", func(r *Request) { r.Excitations = nil }, engerrors.KindInvalidParameter},
		{"probe==reference", func(r *Request) { r.Reference = r.Probe }, engerrors.KindInvalidParameter},
		{"nan target", func(r *Request) { r.TargetDelta = math.NaN() }, engerrors.KindNonFiniteInput},
		{"bad interval", func(r *Request) { r.High = r.Low }, engerrors.KindInvalidParameter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			tc.mut(&req)
			_, err := c.Calibrate(req)
			if got := engerrors.KindOf(err); got != tc.want {
				t.Fatalf("input=%+v\nerr=%v\nwant=%q got=%q", req, err, tc.want, got)
			}
		})
	}
}

func TestCalibrate_IterationLimitReported(t *testing.T) {
	c := calibrator(t, lineJSON)
	cfg := DefaultConfig()
	cfg.MaxIterations = 1
	c.cfg = cfg
	req := lineReq(1.234567, -math.Pi, math.Pi)
	_, err := c.Calibrate(req)
	// One bisection step cannot hit 1e-8 unless trivially close; expect either
	// explicit non-convergence classification.
	if got := engerrors.KindOf(err); got != engerrors.KindCalibrationDiverge {
		// If it happened to converge in one step that is acceptable, but for
		// this target it cannot.
		t.Fatalf("one-iteration cap must not converge for target 1.23, got %v", err)
	}
}

func TestCompensateDrift_RestoresTarget(t *testing.T) {
	c := calibrator(t, lineJSON)
	req := lineReq(0.9, -math.Pi, math.Pi)
	res, err := c.Calibrate(req)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a +0.25 rad drift observed at the stored offset.
	fixed, err := c.CompensateDrift(req, wrapPhase(res.AchievedDelta+0.25), res.Offset)
	if err != nil {
		t.Fatal(err)
	}
	if d := math.Abs(wrapPhase(fixed.AchievedDelta - req.TargetDelta)); d > 1e-7 {
		t.Fatalf("drift compensation residual %.3e exceeds tolerance", d)
	}
}

func TestCompensateDrift_NonFiniteRejected(t *testing.T) {
	c := calibrator(t, lineJSON)
	req := lineReq(0.9, -math.Pi, math.Pi)
	_, err := c.CompensateDrift(req, math.Inf(1), 0.1)
	if got := engerrors.KindOf(err); got != engerrors.KindNonFiniteInput {
		t.Fatalf("infinite observed delta must be non_finite, got %v", err)
	}
}
