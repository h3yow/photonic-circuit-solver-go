package solver

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestConcurrentSolveConsistency(t *testing.T) {
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(mziDoc()))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}

	const goroutines = 64
	const iterations = 20
	results := make([][]*SolveResult, goroutines)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				res, err := e.Solve(context.Background(), c, SolveRequest{
					Batch: []Excitation{
						{Port: "in0", Amplitude: complex(0.7, 0.3)},
						{Port: "in1", Amplitude: complex(0.2, -0.1)},
					},
					Phases: map[string]float64{"ps": 0.777},
					Method: SolverCrossCheck,
				})
				if err != nil {
					t.Errorf("goroutine %d iter %d solve error: %v", g, i, err)
					return
				}
				if i == 0 {
					// 仅保存第一轮，后续轮次与其逐位比较
					snap := make([]*SolveResult, len(res.Results))
					for k, r := range res.Results {
						cp := &SolveResult{Outputs: append([]PortField(nil), r.Outputs...)}
						snap[k] = cp
					}
					results[g] = snap
				} else {
					ref := results[g]
					if len(ref) != len(res.Results) {
						t.Errorf("result length drift")
						return
					}
					for k := range ref {
						if !sameResult(ref[k], res.Results[k]) {
							t.Errorf("goroutine %d iter %d numeric drift on case %d", g, i, k)
							return
						}
					}
				}
			}
		}(g)
	}
	wg.Wait()

	// 不同 goroutine 之间的结论也必须逐位一致（零共享可变状态）。
	ref := results[0]
	for g := 1; g < goroutines; g++ {
		for k := range ref {
			if !sameResult(ref[k], results[g][k]) {
				t.Fatalf("cross-goroutine mismatch: g0 vs g%d case %d", g, k)
			}
		}
	}
}

func TestConcurrentCalibrateNoDuplicateWork(t *testing.T) {
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(calibCircuitDoc()))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	samples := linearSamples(0, 1, 1.3, 0.1, 9)
	req := CalibRequest{ShifterID: "ps", Samples: samples, TargetPhase: 0.83,
		Drift: []CalibPoint{{Control: 0.4, Phase: 0.4*1.3 + 0.1 + 0.05}}}

	const n = 32
	out := make([]*CalibResult, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for g := 0; g < n; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			out[g], errs[g] = e.Calibrate(context.Background(), c, req)
		}(g)
	}
	wg.Wait()
	for g := 0; g < n; g++ {
		if errs[g] != nil {
			t.Fatalf("goroutine %d: %v", g, errs[g])
		}
	}
	for g := 1; g < n; g++ {
		if out[g].Control != out[0].Control || out[g].Iterations != out[0].Iterations ||
			out[g].DriftShift != out[0].DriftShift {
			t.Fatalf("calibration result differs across goroutines: %+v vs %+v", out[0], out[g])
		}
	}
}

func TestConcurrentMixedLoadAndSolve(t *testing.T) {
	// 多个 goroutine 各自独立载入同一模型并求解，结果必须一致。
	doc := mziDoc()
	e, _ := NewEngine()
	const n = 24
	var wg sync.WaitGroup
	mu := struct {
		sync.Mutex
		ref map[string][2]float64
	}{ref: map[string][2]float64{}}
	for g := 0; g < n; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			c, err := e.LoadModel(context.Background(), []byte(doc))
			if err != nil {
				t.Errorf("load: %v", err)
				return
			}
			res, err := e.Solve(context.Background(), c, SolveRequest{
				Batch:  []Excitation{{Port: "in0", Amplitude: 1}},
				Phases: map[string]float64{"ps": 1.23456},
			})
			if err != nil {
				t.Errorf("solve: %v", err)
				return
			}
			got := map[string][2]float64{}
			for _, o := range res.Results[0].Outputs {
				got[o.Port] = [2]float64{real(o.Amplitude), imag(o.Amplitude)}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(mu.ref) == 0 {
				mu.ref = got
			} else {
				for k, v := range got {
					if mu.ref[k] != v {
						t.Errorf("port %s: %v != %v (g=%d) %s", k, v, mu.ref[k], g, fmt.Sprint(got))
					}
				}
			}
		}(g)
	}
	wg.Wait()
}

func sameResult(a, b *SolveResult) bool {
	if a == nil || b == nil {
		return a == b
	}
	if len(a.Outputs) != len(b.Outputs) {
		return false
	}
	for i := range a.Outputs {
		if a.Outputs[i] != b.Outputs[i] {
			return false
		}
	}
	return true
}

// 审计、等价核验与求解混合并发：结论必须逐位一致，
// 且任何调用都不得改变已载入电路或影响其它请求。
func TestConcurrentAuditCompareSolveConsistency(t *testing.T) {
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(mziDoc()))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	docA := waveguideDoc("1.0", "", "", 200)
	docB := waveguideDoc("1.2", "", "db_per_cm", 2)

	type outcome struct {
		audit *AuditReport
		eq    *EquivalenceReport
		solve *BatchResult
	}
	const n = 48
	outs := make([]outcome, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for g := 0; g < n; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			var o outcome
			if o.audit, errs[g] = e.Audit(context.Background(), c); errs[g] != nil {
				return
			}
			if o.eq, errs[g] = e.CompareModels(context.Background(), []byte(docA), []byte(docB)); errs[g] != nil {
				return
			}
			o.solve, errs[g] = e.Solve(context.Background(), c, SolveRequest{
				Batch:  []Excitation{{Port: "in0", Amplitude: complex(0.5, 0.5)}},
				Phases: map[string]float64{"ps": 0.42},
				Method: SolverCrossCheck,
			})
			outs[g] = o
		}(g)
	}
	wg.Wait()
	for g := 0; g < n; g++ {
		if errs[g] != nil {
			t.Fatalf("goroutine %d: %v", g, errs[g])
		}
	}
	ref := outs[0]
	for g := 1; g < n; g++ {
		if !reflect.DeepEqual(ref.audit, outs[g].audit) {
			t.Fatalf("audit report differs: g0 vs g%d", g)
		}
		if !reflect.DeepEqual(ref.eq, outs[g].eq) {
			t.Fatalf("equivalence report differs: g0 vs g%d", g)
		}
		if !sameResult(ref.solve.Results[0], outs[g].solve.Results[0]) {
			t.Fatalf("solve result differs: g0 vs g%d", g)
		}
	}
	// 等价结论本身正确（跨版本同一物理电路）。
	if !ref.eq.Equal {
		t.Fatalf("expected equivalent documents, got:\n%s", ref.eq.String())
	}
}
