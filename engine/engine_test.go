package engine

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/h3yow/photonic-circuit-solver-go/calibration"
	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
	"github.com/h3yow/photonic-circuit-solver-go/solver"
)

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

func mustLoad(t *testing.T, e *Engine) string {
	t.Helper()
	key, err := e.Load([]byte(mziJSON))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return key
}

func solveReq() SolveRequest {
	return SolveRequest{
		Method:     solver.MethodLinear,
		CrossCheck: solver.MethodJacobi,
		Request: solver.Request{
			Excitations: []solver.Excitation{{DeviceID: "b1", Port: "in1", Amplitude: 1}},
			Probes:      []solver.Probe{{DeviceID: "b2", Port: "out1"}, {DeviceID: "b2", Port: "out2"}},
		},
	}
}

func TestConcurrentSolvesConsistent(t *testing.T) {
	e, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	key := mustLoad(t, e)
	const n = 64
	var wg sync.WaitGroup
	outs := make([]string, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			r, err := e.SolveOne(key, solveReq())
			if err != nil {
				errs[i] = err
				return
			}
			outs[i] = fmt.Sprintf("%v|%v", r.Outputs[0], r.Outputs[1])
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("goroutine %d error: %v", i, errs[i])
		}
	}
	for i := 1; i < n; i++ {
		if outs[i] != outs[0] {
			t.Fatalf("concurrent solve drift: #0=%s #%d=%s", outs[0], i, outs[i])
		}
	}
}

func TestConcurrentCalibrationSingleFlight(t *testing.T) {
	e, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	key := mustLoad(t, e)
	creq := calibration.Request{
		PhaseShifter: "ps",
		Excitations:  []solver.Excitation{{DeviceID: "b1", Port: "in1", Amplitude: 1}},
		Probe:        solver.Probe{DeviceID: "b2", Port: "out1"},
		Reference:    solver.Probe{DeviceID: "b1", Port: "in1"},
		TargetDelta:  1.0, Low: 0.05, High: 2*math.Pi - 0.05,
	}
	const n = 32
	var wg sync.WaitGroup
	recs := make([]*CalibrationRecord, n)
	wg.Add(n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			recs[i], _ = e.Calibrate(key, creq)
		}(i)
	}
	close(start)
	wg.Wait()
	for i := 0; i < n; i++ {
		if recs[i] == nil || recs[i].Offset != recs[0].Offset {
			t.Fatalf("calibration coalescing failed at %d: %+v vs %+v", i, recs[i], recs[0])
		}
	}
	// A later call must return the cached record instantly (no repeat run).
	var before, after int64
	atomic.StoreInt64(&before, 1)
	r1, err := e.Calibrate(key, creq)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := e.Calibrate(key, creq)
	if err != nil {
		t.Fatal(err)
	}
	atomic.AddInt64(&after, 1)
	if r1.Offset != r2.Offset || before != 1 || after != 1 {
		t.Fatal("repeated calibration must reuse the immutable record")
	}
}

func TestCacheImmutableAcrossOffsetOverrides(t *testing.T) {
	e, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	key := mustLoad(t, e)
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			off := 0.3 * float64(g%5)
			_, err := e.SolveOneWithOffsets(key, map[string]float64{"ps": off}, solveReq())
			if err != nil {
				t.Errorf("override %g: %v", off, err)
			}
		}(g)
	}
	// Also solve baseline concurrently.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := e.SolveOne(key, solveReq()); err != nil {
			t.Errorf("baseline: %v", err)
		}
	}()
	wg.Wait()
	// Baseline must be unchanged after overrides.
	r, err := e.SolveOne(key, solveReq())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(real(r.Outputs[0])) > 1e-9 || math.Abs(imag(r.Outputs[0])) > 1e-9 {
		t.Fatalf("shared cache was mutated by offset overrides: out1=%v", r.Outputs[0])
	}
}

func TestBatchLimitReject(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Limits.MaxBatch = 3
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	key := mustLoad(t, e)
	batch := make([]SolveRequest, 4)
	for i := range batch {
		batch[i] = solveReq()
	}
	_, err = e.BatchSolve(key, batch)
	if got := engerrors.KindOf(err); got != engerrors.KindLimitExceeded {
		t.Fatalf("input batch=4 limit=3: want limit_exceeded, got %v", err)
	}
}

func TestBatchDegradeSucceedsInChunks(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Limits.MaxBatch = 3
	cfg.Policy = "degrade"
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	key := mustLoad(t, e)
	batch := make([]SolveRequest, 7)
	for i := range batch {
		batch[i] = solveReq()
	}
	out, err := e.BatchSolve(key, batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 7 {
		t.Fatalf("degraded batch must return all 7 results, got %d", len(out))
	}
}

func TestBatchFailureNoPartialResults(t *testing.T) {
	e, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	key := mustLoad(t, e)
	bad := solveReq()
	bad.Request.Excitations[0].Amplitude = complex(math.NaN(), 0)
	batch := []SolveRequest{solveReq(), bad, solveReq()}
	out, err := e.BatchSolve(key, batch)
	if out != nil {
		t.Fatalf("failed batch must not leak partial results, got %d entries", len(out))
	}
	if got := engerrors.KindOf(err); got != engerrors.KindNonFiniteInput {
		t.Fatalf("want non_finite_input, got %v", err)
	}
}

func TestCircuitSizeLimit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Limits.MaxDevices = 2
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Load([]byte(mziJSON))
	if got := engerrors.KindOf(err); got != engerrors.KindLimitExceeded {
		t.Fatalf("4-device circuit with limit 2: want limit_exceeded, got %v", err)
	}
}

func TestLoadVersionIncompatible(t *testing.T) {
	e, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	old := []byte(`{"version":"0.5","devices":[],"connections":[],"external_ports":[]}`)
	if _, err := e.Load(old); engerrors.KindOf(err) != engerrors.KindUnsupportedVersion {
		t.Fatalf("v0.5 must be unsupported_version, got %v", err)
	}
	newer := []byte(`{"version":"3.0","devices":[],"connections":[],"external_ports":[]}`)
	if _, err := e.Load(newer); engerrors.KindOf(err) != engerrors.KindUnsupportedVersion {
		t.Fatalf("v3.0 must be unsupported_version, got %v", err)
	}
}

func TestLoadUnknownFieldRejected(t *testing.T) {
	e, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	bad := []byte(`{"version":"1.0","devices":[],"connections":[],"external_ports":[],"extra":1}`)
	if _, err := e.Load(bad); engerrors.KindOf(err) != engerrors.KindModelDecode {
		t.Fatalf("unknown field must not be dropped silently, got %v", err)
	}
}

func TestBadPolicyRejected(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Policy = "whatever"
	if _, err := New(cfg); engerrors.KindOf(err) != engerrors.KindInvalidParameter {
		t.Fatalf("unknown policy must be invalid_parameter, got %v", err)
	}
}
