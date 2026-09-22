package solver

import (
	"context"
	"math"
	"testing"
)

func calibCircuitDoc() string {
	// 单个移相器，输入输出直连外部。
	return `{
	  "version": "1.0",
	  "circuit": {"id": "cal", "wavelength": 1.55,
	    "devices": [{"id": "ps", "kind": "phaseshifter", "params": {"u0": 0, "k": 1, "gamma": 0.01}}],
	    "connects": [
	      {"from": {"external": "in0"}, "to": {"device": "ps", "port": "in"}},
	      {"from": {"device": "ps", "port": "out"}, "to": {"external": "out0"}}
	    ]
	  }
	}`
}

func linearSamples(u0, u1, k, b float64, n int) []CalibPoint {
	pts := make([]CalibPoint, 0, n)
	for i := 0; i < n; i++ {
		u := u0 + (u1-u0)*float64(i)/float64(n-1)
		pts = append(pts, CalibPoint{Control: u, Phase: b + k*u})
	}
	return pts
}

func TestCalibrateConvergesAndReproducible(t *testing.T) {
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(calibCircuitDoc()))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	samples := linearSamples(0, 1, 1.2, 0.3, 7)
	target := 0.9
	r1, err := e.Calibrate(context.Background(), c, CalibRequest{
		ShifterID: "ps", Samples: samples, TargetPhase: target,
	})
	if err != nil {
		t.Fatalf("Calibrate: %v", err)
	}
	r2, err := e.Calibrate(context.Background(), c, CalibRequest{
		ShifterID: "ps", Samples: samples, TargetPhase: target,
	})
	if err != nil {
		t.Fatalf("Calibrate #2: %v", err)
	}
	if r1.Control != r2.Control || r1.Iterations != r2.Iterations {
		t.Fatalf("calibration not reproducible: %+v vs %+v", r1, r2)
	}
	// 反演残差必须达到相位容差
	if math.Abs(1.2*r1.Control+0.3-target) > 1e-8 {
		t.Fatalf("inversion residual too large: u=%v", r1.Control)
	}
	t.Logf("u=%v iterations=%d k=%v intercept=%v", r1.Control, r1.Iterations, r1.K, r1.Intercept)
}

func TestCalibrateDriftCompensation(t *testing.T) {
	e, _ := NewEngine()
	c, _ := e.LoadModel(context.Background(), []byte(calibCircuitDoc()))
	samples := linearSamples(0, 1, 1.0, 0.0, 7)
	drift := []CalibPoint{{Control: 0.5, Phase: 0.5 + 0.2}} // 实测多出 +0.2 rad
	r, err := e.Calibrate(context.Background(), c, CalibRequest{
		ShifterID: "ps", Samples: samples, TargetPhase: 0.5, Drift: drift,
	})
	if err != nil {
		t.Fatalf("Calibrate: %v", err)
	}
	if math.Abs(r.DriftShift-0.2) > 1e-9 {
		t.Fatalf("drift shift = %v, want 0.2", r.DriftShift)
	}
	// 需要比无漂移时的 u=0.5 低 0.2 控制量
	if math.Abs(r.Control-0.3) > 1e-8 {
		t.Fatalf("compensated control = %v, want ~0.3", r.Control)
	}
}

func TestCalibrateRejectNonMonotone(t *testing.T) {
	e, _ := NewEngine()
	c, _ := e.LoadModel(context.Background(), []byte(calibCircuitDoc()))
	// 展开后出现下降（第二个 wrap 后增量为负大值）
	pts := []CalibPoint{
		{Control: 0, Phase: 0},
		{Control: 0.3, Phase: 2},
		{Control: 0.6, Phase: 0.5},
		{Control: 0.9, Phase: 3},
	}
	_, err := e.Calibrate(context.Background(), c, CalibRequest{
		ShifterID: "ps", Samples: pts, TargetPhase: 1,
	})
	if !IsCode(err, ErrCalibNoSolution) {
		t.Fatalf("want CALIB_NO_SOLUTION, got %v (input=%v)", err, pts)
	}
	t.Logf("rejected: %v", err)
}

func TestCalibrateRejectTooFewSamples(t *testing.T) {
	e, _ := NewEngine()
	c, _ := e.LoadModel(context.Background(), []byte(calibCircuitDoc()))
	_, err := e.Calibrate(context.Background(), c, CalibRequest{
		ShifterID: "ps",
		Samples:   []CalibPoint{{0, 0}, {1, 1}},
	})
	if !IsCode(err, ErrCalibNoSolution) {
		t.Fatalf("want CALIB_NO_SOLUTION, got %v", err)
	}
}

func TestCalibrateRejectInconsistentSamples(t *testing.T) {
	e, _ := NewEngine()
	c, _ := e.LoadModel(context.Background(), []byte(calibCircuitDoc()))
	// 严格递增但严重偏离任何二次曲线（折线尖峰）。
	pts := []CalibPoint{
		{Control: 0, Phase: 0},
		{Control: 0.25, Phase: 0.01},
		{Control: 0.5, Phase: 2.5},
		{Control: 0.75, Phase: 0.02},
		{Control: 1, Phase: 1},
	}
	_, err := e.Calibrate(context.Background(), c, CalibRequest{
		ShifterID: "ps", Samples: pts, TargetPhase: 0.5,
	})
	if !IsCode(err, ErrCalibNoSolution) {
		t.Fatalf("want CALIB_NO_SOLUTION for inconsistent samples, got %v", err)
	}
	t.Logf("rejected: %v", err)
}

func TestCalibrateRejectOutOfReach(t *testing.T) {
	e, _ := NewEngine()
	c, _ := e.LoadModel(context.Background(), []byte(calibCircuitDoc()))
	// 标定覆盖相位区间 [0,1]；目标 5 rad 超出（且模 2π 代表元 5-2π≈-1.28 仍不在 [0,1]）。
	samples := linearSamples(0, 1, 1.0, 0.0, 5)
	_, err := e.Calibrate(context.Background(), c, CalibRequest{
		ShifterID: "ps", Samples: samples, TargetPhase: 5,
	})
	if !IsCode(err, ErrCalibOutOfRange) {
		t.Fatalf("want CALIB_OUT_OF_RANGE, got %v", err)
	}
	t.Logf("rejected: %v", err)
}

func TestCalibrateModuloEquivalentTarget(t *testing.T) {
	e, _ := NewEngine()
	c, _ := e.LoadModel(context.Background(), []byte(calibCircuitDoc()))
	samples := linearSamples(0, 1, 1.0, 0.0, 5)
	// 目标 0.5 + 2π 应等价于 0.5
	r, err := e.Calibrate(context.Background(), c, CalibRequest{
		ShifterID: "ps", Samples: samples, TargetPhase: 0.5 + 2*math.Pi,
	})
	if err != nil {
		t.Fatalf("Calibrate: %v", err)
	}
	if math.Abs(r.Control-0.5) > 1e-8 {
		t.Fatalf("control = %v, want 0.5", r.Control)
	}
}

func TestCalibrateRejectNonFinite(t *testing.T) {
	e, _ := NewEngine()
	c, _ := e.LoadModel(context.Background(), []byte(calibCircuitDoc()))
	pts := []CalibPoint{
		{Control: 0, Phase: 0},
		{Control: 0.5, Phase: math.NaN()},
		{Control: 1, Phase: 1},
	}
	_, err := e.Calibrate(context.Background(), c, CalibRequest{
		ShifterID: "ps", Samples: pts, TargetPhase: 0.5,
	})
	if !IsCode(err, ErrNonFiniteInput) {
		t.Fatalf("want NON_FINITE_INPUT, got %v", err)
	}
}

func TestCalibrateNotConvergedWhenIterationCapped(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxCalibIterations = 2 // 1e-10 残差不可能在 2 次二分内达到
	e, _ := NewEngine(WithLimits(lim))
	c, _ := e.LoadModel(context.Background(), []byte(calibCircuitDoc()))
	samples := linearSamples(0, 1, 1.0, 0.0, 5)
	_, err := e.Calibrate(context.Background(), c, CalibRequest{
		ShifterID: "ps", Samples: samples, TargetPhase: 0.3,
	})
	if !IsCode(err, ErrCalibNotConverged) {
		t.Fatalf("want CALIB_NOT_CONVERGED, got %v", err)
	}
	t.Logf("rejected: %v", err)
}

func TestCalibrationResultDrivesSolve(t *testing.T) {
	// 端到端：标定 → 用所得控制量驱动 Solve → 输出相位应达到目标（含漂移补偿）。
	doc := `{
	  "version": "1.1",
	  "circuit": {"id": "cal", "wavelength": 1.55,
	    "devices": [{"id": "ps", "kind": "phaseshifter",
	      "params": {"u0": 0.1, "k": 2.0, "gamma": 0.05}}],
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
	modelPhi := func(u float64) float64 {
		du := u - 0.1
		return 2.0*du + 0.05*du*du
	}
	samples := make([]CalibPoint, 0, 9)
	for i := 0; i < 9; i++ {
		u := float64(i) * 0.1
		samples = append(samples, CalibPoint{Control: u, Phase: modelPhi(u)})
	}
	const target = 0.7
	cr, err := e.Calibrate(context.Background(), c, CalibRequest{
		ShifterID: "ps", Samples: samples, TargetPhase: target,
	})
	if err != nil {
		t.Fatalf("Calibrate: %v", err)
	}
	res, err := e.Solve(context.Background(), c, SolveRequest{
		Batch:    []Excitation{{Port: "in0", Amplitude: 1}},
		Controls: map[string]float64{"ps": cr.Control},
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	out := res.Results[0].Outputs[0].Amplitude
	got := math.Mod(math.Atan2(imag(out), real(out))+2*math.Pi, 2*math.Pi)
	want := math.Mod(target, 2*math.Pi)
	if math.Abs(got-want) > 1e-8 {
		t.Fatalf("realized phase = %v, want %v (control=%v iters=%d)",
			got, want, cr.Control, cr.Iterations)
	}
	t.Logf("target=%v realized=%v control=%v iters=%d", want, got, cr.Control, cr.Iterations)
}

func TestCalibrateRejectDriftOutsideSpan(t *testing.T) {
	e, _ := NewEngine()
	c, _ := e.LoadModel(context.Background(), []byte(calibCircuitDoc()))
	samples := linearSamples(0, 1, 1.0, 0.0, 5)
	_, err := e.Calibrate(context.Background(), c, CalibRequest{
		ShifterID:   "ps",
		Samples:     samples,
		TargetPhase: 0.5,
		Drift:       []CalibPoint{{Control: 5, Phase: 5}},
	})
	if !IsCode(err, ErrCalibNoSolution) {
		t.Fatalf("want CALIB_NO_SOLUTION for out-of-span drift, got %v", err)
	}
}
