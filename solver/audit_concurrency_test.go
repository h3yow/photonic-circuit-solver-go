package solver

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

// 并发下审计、对比与求解混合执行：所有结论必须逐位一致，
// 且任何调用都不得改变已载入的电路或影响其他请求。
func TestConcurrentAuditCompareSolveConsistency(t *testing.T) {
	e, _ := NewEngine()
	c, err := e.LoadModel(context.Background(), []byte(mziDoc()))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	docA := []byte(compareDocV11)
	docB := []byte(compareDocV12)

	// 参考结论（串行基线）。
	refAudit, err := e.Audit(context.Background(), c)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	refAuditJSON, err := json.Marshal(refAudit)
	if err != nil {
		t.Fatalf("marshal audit: %v", err)
	}
	refCmp, err := e.CompareModels(context.Background(), docA, docB, 0)
	if err != nil {
		t.Fatalf("CompareModels: %v", err)
	}
	refCmpJSON, err := json.Marshal(refCmp)
	if err != nil {
		t.Fatalf("marshal compare: %v", err)
	}
	refSolve, err := e.Solve(context.Background(), c, SolveRequest{
		Batch:  []Excitation{{Port: "in0", Amplitude: 1}},
		Phases: map[string]float64{"ps": 0.5},
		Method: SolverCrossCheck,
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}

	const goroutines = 48
	const iterations = 10
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				switch g % 3 {
				case 0: // 审计已载入电路
					rep, err := e.Audit(context.Background(), c)
					if err != nil {
						t.Errorf("audit: %v", err)
						return
					}
					buf, _ := json.Marshal(rep)
					if string(buf) != string(refAuditJSON) {
						t.Errorf("g%d i%d: audit report drift", g, i)
						return
					}
				case 1: // 跨版本等价核验
					rep, err := e.CompareModels(context.Background(), docA, docB, 0)
					if err != nil {
						t.Errorf("compare: %v", err)
						return
					}
					buf, _ := json.Marshal(rep)
					if string(buf) != string(refCmpJSON) {
						t.Errorf("g%d i%d: compare report drift", g, i)
						return
					}
				default: // 求解
					res, err := e.Solve(context.Background(), c, SolveRequest{
						Batch:  []Excitation{{Port: "in0", Amplitude: 1}},
						Phases: map[string]float64{"ps": 0.5},
						Method: SolverCrossCheck,
					})
					if err != nil {
						t.Errorf("solve: %v", err)
						return
					}
					if !sameResult(refSolve.Results[0], res.Results[0]) {
						t.Errorf("g%d i%d: solve drift", g, i)
						return
					}
				}
			}
		}(g)
	}
	wg.Wait()

	// 并发风暴之后，已载入电路的结论仍须与基线逐位一致（未被任何调用改变）。
	res, err := e.Solve(context.Background(), c, SolveRequest{
		Batch:  []Excitation{{Port: "in0", Amplitude: 1}},
		Phases: map[string]float64{"ps": 0.5},
		Method: SolverCrossCheck,
	})
	if err != nil {
		t.Fatalf("post-storm Solve: %v", err)
	}
	if !sameResult(refSolve.Results[0], res.Results[0]) {
		t.Fatalf("loaded circuit mutated by concurrent calls")
	}
}

// 并发下失败调用也必须每次都返回 nil 报告，不得出现半截结果。
func TestConcurrentFailuresReturnNoPartialReport(t *testing.T) {
	e, _ := NewEngine()
	badVersion := []byte(`{"version":"7.7","circuit":{"id":"x","wavelength":1.55,"devices":[],"connects":[]}}`)
	good := []byte(compareDocV11)
	badDevice := []byte(`{"version":"1.2","circuit":{"id":"x","wavelength":1.55,
	  "devices":[{"id":"p","kind":"phaseshifter","params":{},"loss_unit":"db_per_m"}],"connects":[]}}`)

	const n = 64
	var wg sync.WaitGroup
	for g := 0; g < n; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			switch g % 3 {
			case 0:
				rep, err := e.AuditModel(context.Background(), badVersion)
				if err == nil || rep != nil {
					t.Errorf("audit bad version: err=%v rep=%+v", err, rep)
				}
			case 1:
				rep, err := e.CompareModels(context.Background(), badVersion, good, 0)
				if err == nil || rep != nil {
					t.Errorf("compare bad version: err=%v rep=%+v", err, rep)
				}
			default:
				rep, err := e.AuditModel(context.Background(), badDevice)
				if err == nil || rep != nil {
					t.Errorf("audit conflicting device: err=%v rep=%+v", err, rep)
				}
			}
		}(g)
	}
	wg.Wait()
}
