package solver

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// bsDoc 生成一个全连接的单分束器电路，params 为 params 对象的 JSON 内容。
func bsDoc(params string) string {
	return `{"version":"1.2","circuit":{"id":"x","wavelength":1.55,
	  "devices":[{"id":"bs","kind":"beamsplitter","params":{` + params + `}}],
	  "connects":[
	    {"from":{"external":"in0"},"to":{"device":"bs","port":"a"}},
	    {"from":{"external":"in1"},"to":{"device":"bs","port":"b"}},
	    {"from":{"device":"bs","port":"c"},"to":{"external":"out0"}},
	    {"from":{"device":"bs","port":"d"},"to":{"external":"out1"}}
	  ]}}`
}

// 范围外取值（团队按百分比填写的 reflectivity=30）必须被整体拒绝：
// 返回错误（指出器件、物理量、取值与原因），且不返回任何报告——
// 绝不产出含 NaN、无法 JSON 序列化的结果。
func TestEquivalenceOutOfRangeReflectivityRejected(t *testing.T) {
	e := mustEngine(t)
	good := bsDoc(`"reflectivity":0.25`)
	bad := bsDoc(`"reflectivity":30`)
	for _, order := range [][2]string{{bad, good}, {good, bad}, {bad, bad}} {
		rep, err := e.CompareModels(context.Background(), []byte(order[0]), []byte(order[1]))
		if err == nil {
			t.Fatalf("want refusal for reflectivity=30, got report:\n%s", rep.String())
		}
		if rep != nil {
			t.Fatalf("refusal must not return any report")
		}
		se, ok := AsSolverError(err)
		if !ok {
			t.Fatalf("want classified SolverError, got %T: %v", err, err)
		}
		if se.Code != ErrInvalidParameter {
			t.Fatalf("want INVALID_PARAMETER, got %s: %v", se.Code, err)
		}
		if se.Detail["device"] != "bs" || se.Detail["parameter"] != "reflectivity" {
			t.Fatalf("error must name device and parameter: %+v", se.Detail)
		}
		if v, _ := se.Detail["value"].(float64); v != 30 {
			t.Fatalf("error must carry offending value, got %+v", se.Detail)
		}
		msg := err.Error()
		for _, want := range []string{"out of physical range", "bs", "reflectivity"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("error message missing %q: %s", want, msg)
			}
		}
	}

	// 负数与 1 以上同样拒绝；合法边界 0 与 1 必须可用。
	for _, r := range []string{"-0.1", "1.0000001"} {
		rep, err := e.CompareModels(context.Background(), []byte(bsDoc(`"reflectivity":`+r)), []byte(good))
		if err == nil || rep != nil {
			t.Fatalf("reflectivity %s must be rejected", r)
		}
	}
	for _, r := range []string{"0", "1"} {
		if _, err := e.CompareModels(context.Background(),
			[]byte(bsDoc(`"reflectivity":`+r)), []byte(good)); err != nil {
			t.Fatalf("reflectivity %s is a legal boundary value: %v", r, err)
		}
	}
}

// 同一分束器同时声明 theta 与 reflectivity 属于自相矛盾：
// 必须整体拒绝，不能静默按其中一种算下去（更不能判“相等”）。
func TestEquivalenceConflictingBeamsplitterDeclarationsRejected(t *testing.T) {
	e := mustEngine(t)
	good := bsDoc(`"theta":0.5235987755982988`)
	conflict := bsDoc(`"theta":0.5235987755982988,"reflectivity":0.25`)
	for _, order := range [][2]string{{conflict, good}, {good, conflict}, {conflict, conflict}} {
		rep, err := e.CompareModels(context.Background(), []byte(order[0]), []byte(order[1]))
		if err == nil {
			t.Fatalf("conflicting declarations must be rejected, got report:\n%s", rep.String())
		}
		if rep != nil {
			t.Fatalf("conflicting declarations must not return any report")
		}
		se, ok := AsSolverError(err)
		if !ok {
			t.Fatalf("want SolverError: %v", err)
		}
		if se.Code != ErrInvalidParameter || se.Detail["device"] != "bs" {
			t.Fatalf("want INVALID_PARAMETER naming bs, got %s %+v", se.Code, se.Detail)
		}
		if !strings.Contains(err.Error(), "theta") || !strings.Contains(err.Error(), "reflectivity") {
			t.Fatalf("error must name both conflicting parameters: %v", err)
		}
	}

	// 冲突文档与谁比较都不能“蒙混过关”：即使另一侧恰好 theta 与 reflectivity
	// 折算后数值接近，也不得给出 equal=true。
	sneaky := bsDoc(`"theta":0.5235987755982988,"reflectivity":0.24999999999999994`)
	if rep, err := e.CompareModels(context.Background(), []byte(sneaky), []byte(good)); err == nil || rep != nil {
		t.Fatal("near-matching conflicting declarations must still be rejected")
	}
}

// 相等与不等两种结论的报告都必须能被程序直接继续处理：JSON 可序列化，
// 数值型 mismatch 逐项包含 device/quantity/numeric/a/b/diff/reason，
// 结构型 mismatch 的数值字段显式为 null，不存在 NaN/Inf。
func TestEquivalenceReportIsFullySerializable(t *testing.T) {
	e := mustEngine(t)

	// 数值型判不等：200 dB/m vs 300 dB/m。
	rep, err := e.CompareModels(context.Background(),
		[]byte(waveguideDoc("1.0", "", "", 200)),
		[]byte(waveguideDoc("1.2", "", "db_per_cm", 3)))
	if err != nil || rep.Equal {
		t.Fatalf("want not-equal report, equal=%v err=%v", rep == nil, err)
	}
	data := mustMarshalJSON(t, rep)
	if !strings.Contains(string(data), `"diff":100`) {
		t.Fatalf("numeric mismatch must serialize diff:\n%s", data)
	}
	for _, key := range []string{"device_id", "quantity", "numeric", "a", "b", "diff", "reason"} {
		if !strings.Contains(string(data), `"`+key+`"`) {
			t.Fatalf("mismatch JSON missing key %q:\n%s", key, data)
		}
	}
	// 反序列化回结构仍然完整（不缺项）。
	var back EquivalenceReport
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	if len(back.Mismatches) != 1 {
		t.Fatalf("want 1 mismatch, got %d", len(back.Mismatches))
	}
	m := back.Mismatches[0]
	if !m.Numeric || m.A == nil || m.B == nil || m.Diff == nil ||
		*m.A != 200 || *m.B != 300 || *m.Diff != 100 || m.Reason == "" {
		t.Fatalf("numeric mismatch incomplete after roundtrip: %+v", m)
	}

	// 结构型判不等：器件只在一侧存在，a/b/diff 必须显式序列化为 null。
	base := waveguideDoc("1.1", "", "", 0)
	extra := `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,
	  "devices":[
	    {"id":"w","kind":"waveguide","params":{"length":1000000,"neff":2,"loss":0}},
	    {"id":"w2","kind":"waveguide","params":{"length":1,"neff":2}}
	  ],
	  "connects":[
	    {"from":{"external":"in0"},"to":{"device":"w","port":"in"}},
	    {"from":{"device":"w","port":"out"},"to":{"device":"w2","port":"in"}},
	    {"from":{"device":"w2","port":"out"},"to":{"external":"out0"}}
	  ]}}`
	rep2, err := e.CompareModels(context.Background(), []byte(base), []byte(extra))
	if err != nil || rep2.Equal {
		t.Fatalf("want structural not-equal, err=%v", err)
	}
	data2 := mustMarshalJSON(t, rep2)
	var structural int
	for _, mm := range rep2.Mismatches {
		if !mm.Numeric {
			structural++
			if mm.A != nil || mm.B != nil || mm.Diff != nil {
				t.Fatalf("structural mismatch values must be nil: %+v", mm)
			}
		}
	}
	if structural == 0 || !strings.Contains(string(data2), `"a":null`) {
		t.Fatalf("structural mismatch must serialize null values:\n%s", data2)
	}

	// 相等结论同样可序列化。
	eqRep, err := e.CompareModels(context.Background(),
		[]byte(waveguideDoc("1.0", "", "", 200)),
		[]byte(waveguideDoc("1.2", "", "db_per_cm", 2)))
	if err != nil || !eqRep.Equal {
		t.Fatalf("want equal report, err=%v", err)
	}
	eqData := mustMarshalJSON(t, eqRep)
	if !strings.Contains(string(eqData), `"equal":true`) {
		t.Fatalf("equal report JSON wrong:\n%s", eqData)
	}
}

// mustMarshalJSON 序列化报告并确保结果中不含任何 NaN/Inf 隐患
// （encoding/json 遇到非有限数会直接报错）。
func mustMarshalJSON(t *testing.T, rep *EquivalenceReport) []byte {
	t.Helper()
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("report must be JSON-serializable: %v\n%s", err, rep.String())
	}
	return data
}

// mismatchKey 给单个 mismatch 一个稳定键。
func mismatchKey(m Mismatch) string {
	return m.DeviceID + "|" + m.Quantity + "|" + fmt.Sprintf("%v", m.Numeric)
}

// 文档顺序对调：相等/不等结论、逐项归一化取值（A/B 对调）、差值、
// 容差必须完全对称一致。
func TestEquivalenceOrderSymmetry(t *testing.T) {
	e := mustEngine(t)
	pairs := [][2]string{
		// 相等：跨版本跨单位同一物理电路。
		{waveguideDoc("1.0", "", "", 200), waveguideDoc("1.2", "", "db_per_cm", 2)},
		// 数值不等：200 vs 300 dB/m。
		{waveguideDoc("1.0", "", "", 200), waveguideDoc("1.2", "", "db_per_cm", 3)},
		// theta 写法 vs reflectivity 写法，数值不等。
		{bsDoc(`"theta":0.5235987755982988`), bsDoc(`"reflectivity":0.5`)},
	}
	for _, p := range pairs {
		ab, err := e.CompareModels(context.Background(), []byte(p[0]), []byte(p[1]))
		if err != nil {
			t.Fatalf("A,B: %v", err)
		}
		ba, err := e.CompareModels(context.Background(), []byte(p[1]), []byte(p[0]))
		if err != nil {
			t.Fatalf("B,A: %v", err)
		}
		if ab.Equal != ba.Equal || ab.Tolerance != ba.Tolerance {
			t.Fatalf("conclusion differs by order: %+v vs %+v", ab, ba)
		}
		if len(ab.Mismatches) != len(ba.Mismatches) {
			t.Fatalf("mismatch count differs by order: %d vs %d",
				len(ab.Mismatches), len(ba.Mismatches))
		}

		// 数值 mismatch：键相同后 A/B 对调、Diff 相同。
		baByKey := map[string]Mismatch{}
		for _, m := range ba.Mismatches {
			baByKey[mismatchKey(m)] = m
		}
		for _, m := range ab.Mismatches {
			other, ok := baByKey[mismatchKey(m)]
			if !ok {
				t.Fatalf("reversed order missing mismatch %q", mismatchKey(m))
			}
			if m.Numeric {
				if other.A == nil || other.B == nil || other.Diff == nil ||
					*m.A != *other.B || *m.B != *other.A || *m.Diff != *other.Diff {
					t.Fatalf("numeric mismatch not symmetric:\n%+v\n%+v", m, other)
				}
			}
		}

		// 逐器件归一化对照同样对称（A/B 对调，within_tolerance 一致）。
		assertDeviceComparisonsSymmetric(t, ab, ba)
	}
}

func assertDeviceComparisonsSymmetric(t *testing.T, ab, ba *EquivalenceReport) {
	t.Helper()
	sortQuantities := func(rep *EquivalenceReport) map[string]map[string]QuantityComparison {
		out := map[string]map[string]QuantityComparison{}
		for _, d := range rep.Devices {
			qm := map[string]QuantityComparison{}
			for _, q := range d.Quantities {
				qm[q.Name] = q
			}
			out[d.DeviceID] = qm
		}
		return out
	}
	x, y := sortQuantities(ab), sortQuantities(ba)
	if len(x) != len(y) {
		t.Fatalf("device comparison count differs: %d vs %d", len(x), len(y))
	}
	for id, qx := range x {
		qy, ok := y[id]
		if !ok || len(qx) != len(qy) {
			t.Fatalf("device %s comparison missing/short in reversed report", id)
		}
		for name, a := range qx {
			b := qy[name]
			if a.Unit != b.Unit || a.WithinTolerance != b.WithinTolerance ||
				a.A != b.B || a.B != b.A || a.Diff != b.Diff {
				t.Fatalf("quantity %s.%s not symmetric: %+v vs %+v", id, name, a, b)
			}
		}
	}
}

// 并发核验：同一对文档（相等、不等、被拒绝三种）在大量 goroutine 下
// 结论逐位一致（JSON 字节相同），错误路径也一致且从不返回半截报告。
func TestEquivalenceConcurrentDeterminism(t *testing.T) {
	e := mustEngine(t)
	type pair struct {
		a, b string
		ok   bool // 是否应返回报告
	}
	pairs := []pair{
		{waveguideDoc("1.0", "", "", 200), waveguideDoc("1.2", "", "db_per_cm", 2), true},
		{waveguideDoc("1.0", "", "", 200), waveguideDoc("1.2", "", "db_per_cm", 3), true},
		{bsDoc(`"reflectivity":30`), bsDoc(`"reflectivity":0.25`), false},
		{bsDoc(`"theta":0.5,"reflectivity":0.2`), bsDoc(`"theta":0.5`), false},
	}

	const goroutines = 32
	results := make([][][]byte, len(pairs))
	var wg sync.WaitGroup
	for pi := range pairs {
		results[pi] = make([][]byte, goroutines)
		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func(pi, g int) {
				defer wg.Done()
				p := pairs[pi]
				rep, err := e.CompareModels(context.Background(), []byte(p.a), []byte(p.b))
				if p.ok {
					if err != nil {
						t.Errorf("pair %d g%d: unexpected error %v", pi, g, err)
						return
					}
					data, jerr := json.Marshal(rep)
					if jerr != nil {
						t.Errorf("pair %d g%d: marshal %v", pi, g, jerr)
						return
					}
					results[pi][g] = data
				} else {
					if err == nil {
						t.Errorf("pair %d g%d: expected rejection", pi, g)
						return
					}
					if rep != nil {
						t.Errorf("pair %d g%d: rejection returned a report", pi, g)
					}
				}
			}(pi, g)
		}
	}
	wg.Wait()
	for pi := range pairs {
		if !pairs[pi].ok {
			continue
		}
		ref := results[pi][0]
		for g := 1; g < goroutines; g++ {
			if string(ref) != string(results[pi][g]) {
				t.Fatalf("pair %d: non-deterministic report across goroutines:\n%s\nvs\n%s",
					pi, ref, results[pi][g])
			}
		}
	}
}

// 核验不得改动已载入电路，也不得影响同时在跑的求解：
// 比较前后 CompiledCircuit 的模型快照逐位相等，并发求解结果不变。
func TestEquivalenceDoesNotMutateLoadedCircuit(t *testing.T) {
	e := mustEngine(t)
	c := loadDoc(t, mziDoc())
	before := reflect.DeepEqual(c.Model(), c.Model())
	if !before {
		t.Fatal("sanity: deep equal failed")
	}
	snapshot := *c.Model()
	snapshot.Circuit.Devices = append([]Device(nil), c.Model().Circuit.Devices...)
	for i := range snapshot.Circuit.Devices {
		snapshot.Circuit.Devices[i].Params = map[string]float64{}
		for k, v := range c.Model().Circuit.Devices[i].Params {
			snapshot.Circuit.Devices[i].Params[k] = v
		}
	}
	snapshot.Circuit.Connects = append([]Connection(nil), c.Model().Circuit.Connects...)

	var wg sync.WaitGroup
	const n = 24
	errs := make([]error, 3*n)
	results := make([]*BatchResult, n)
	docA := waveguideDoc("1.0", "", "", 200)
	docB := waveguideDoc("1.2", "", "db_per_cm", 3) // 故意不等
	for g := 0; g < n; g++ {
		wg.Add(3)
		go func(g int) {
			defer wg.Done()
			_, errs[g] = e.CompareModels(context.Background(), []byte(docA), []byte(docB))
		}(g)
		go func(g int) {
			defer wg.Done()
			_, errs[n+g] = e.CompareModels(context.Background(),
				[]byte(bsDoc(`"reflectivity":30`)), []byte(bsDoc(`"reflectivity":0.25`)))
		}(g)
		go func(g int) {
			defer wg.Done()
			results[g], errs[2*n+g] = e.Solve(context.Background(), c, SolveRequest{
				Batch:  []Excitation{{Port: "in0", Amplitude: complex(0.6, -0.2)}},
				Phases: map[string]float64{"ps": 0.91},
				Method: SolverCrossCheck,
			})
		}(g)
	}
	wg.Wait()
	for i, err := range errs {
		wantErr := i >= n && i < 2*n // 第二批：范围外取值，应当拒绝
		if wantErr && err == nil {
			t.Fatalf("case %d: expected rejection", i)
		}
		if !wantErr && err != nil {
			t.Fatalf("case %d: unexpected error %v", i, err)
		}
	}
	if !reflect.DeepEqual(c.Model(), &snapshot) {
		t.Fatal("CompareModels mutated the loaded compiled circuit model")
	}
	ref := results[0]
	for g := 1; g < n; g++ {
		if !sameResult(ref.Results[0], results[g].Results[0]) {
			t.Fatalf("concurrent solve result drifted during comparisons: g0 vs g%d", g)
		}
	}
}

// 拒绝消息必须明确指出是哪一侧文档、哪个器件、哪个物理量无法确定语义。
func TestEquivalenceRejectionNamesSideDeviceQuantity(t *testing.T) {
	e := mustEngine(t)
	good := bsDoc(`"reflectivity":0.25`)
	bad := bsDoc(`"reflectivity":30`)
	rep, err := e.CompareModels(context.Background(), []byte(good), []byte(bad))
	if err == nil || rep != nil {
		t.Fatal("want rejection")
	}
	if !strings.Contains(err.Error(), "document B") {
		t.Fatalf("error must name offending side B: %v", err)
	}
	rep, err = e.CompareModels(context.Background(), []byte(bad), []byte(good))
	if err == nil || rep != nil {
		t.Fatal("want rejection")
	}
	if !strings.Contains(err.Error(), "document A") {
		t.Fatalf("error must name offending side A: %v", err)
	}
}
