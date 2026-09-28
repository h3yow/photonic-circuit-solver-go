package solver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// bsDoc 构造单分束器电路，paramJSON 为 params 对象内容（可含多个参数）。
func bsDoc(version, paramJSON string) string {
	return `{"version":"` + version + `","circuit":{"id":"x","wavelength":1.55,
	  "devices":[{"id":"bs","kind":"beamsplitter","params":{` + paramJSON + `}}],
	  "connects":[
	    {"from":{"external":"in0"},"to":{"device":"bs","port":"a"}},
	    {"from":{"external":"in1"},"to":{"device":"bs","port":"b"}},
	    {"from":{"device":"bs","port":"c"},"to":{"external":"out0"}},
	    {"from":{"device":"bs","port":"d"},"to":{"external":"out1"}}
	  ]}}`
}

func compareDocs(t *testing.T, a, b string) *EquivalenceReport {
	t.Helper()
	e, _ := NewEngine()
	rep, err := e.CompareModels(context.Background(), []byte(a), []byte(b))
	if err != nil {
		t.Fatalf("CompareModels: %v", err)
	}
	if rep == nil {
		t.Fatal("nil report without error")
	}
	return rep
}

// 跨版本等价：1.0 隐式 db_per_m 与 1.2 器件级 db_per_cm 表达同一物理电路。
func TestEquivalenceAcrossVersions(t *testing.T) {
	a := waveguideDoc("1.0", "", "", 200)
	b := waveguideDoc("1.2", "", "db_per_cm", 2)
	rep := compareDocs(t, a, b)
	if !rep.Equal {
		t.Fatalf("want equal, got mismatches:\n%s", rep.String())
	}
	if rep.Tolerance != 1e-9 {
		t.Fatalf("tolerance = %v, want 1e-9", rep.Tolerance)
	}
	// 相等时给出逐器件归一化对照。
	if len(rep.Devices) != 1 || rep.Devices[0].DeviceID != "w" {
		t.Fatalf("devices = %+v", rep.Devices)
	}
	var lossCmp *QuantityComparison
	for i := range rep.Devices[0].Quantities {
		if rep.Devices[0].Quantities[i].Name == "loss" {
			lossCmp = &rep.Devices[0].Quantities[i]
		}
	}
	if lossCmp == nil {
		t.Fatalf("loss comparison missing: %+v", rep.Devices[0].Quantities)
	}
	if lossCmp.A != 200 || lossCmp.B != 200 || lossCmp.Unit != "db_per_m" || !lossCmp.WithinTolerance {
		t.Fatalf("loss comparison = %+v, want normalized 200 vs 200 db_per_m", lossCmp)
	}
}

// 跨声明等价：电路级 db_per_cm 与器件级 db_per_cm 写法不同、物理相同。
func TestEquivalenceAcrossDeclarationStyles(t *testing.T) {
	a := waveguideDoc("1.1", "db_per_cm", "", 2)
	b := waveguideDoc("1.2", "", "db_per_cm", 2)
	if rep := compareDocs(t, a, b); !rep.Equal {
		t.Fatalf("want equal:\n%s", rep.String())
	}
	// 缺省与显式写法等价：loss 缺省 0 vs 显式 loss:0。
	a = waveguideDoc("1.1", "", "", -1) // 不写 loss
	b = waveguideDoc("1.1", "", "", 0)
	if rep := compareDocs(t, a, b); !rep.Equal {
		t.Fatalf("default vs explicit loss=0 want equal:\n%s", rep.String())
	}
}

// 分束器 theta 与 reflectivity 两种写法表达同一器件时判等价。
func TestEquivalenceThetaVsReflectivity(t *testing.T) {
	mk := func(param string) string {
		return `{"version":"1.1","circuit":{"id":"x","wavelength":1.55,
		  "devices":[{"id":"bs","kind":"beamsplitter","params":{` + param + `}}],
		  "connects":[
		    {"from":{"external":"in0"},"to":{"device":"bs","port":"a"}},
		    {"from":{"external":"in1"},"to":{"device":"bs","port":"b"}},
		    {"from":{"device":"bs","port":"c"},"to":{"external":"out0"}},
		    {"from":{"device":"bs","port":"d"},"to":{"external":"out1"}}
		  ]}}`
	}
	// theta=pi/6 <=> reflectivity=sin^2(pi/6)=0.25
	a := mk(`"theta":0.5235987755982988`)
	b := mk(`"reflectivity":0.24999999999999994`)
	rep := compareDocs(t, a, b)
	if !rep.Equal {
		t.Fatalf("theta vs reflectivity want equal:\n%s", rep.String())
	}
}

// 不等价：必须指出器件、物理量、两边取值、差值与原因。
func TestInequivalenceLossDiffers(t *testing.T) {
	a := waveguideDoc("1.0", "", "", 200)        // 200 dB/m
	b := waveguideDoc("1.2", "", "db_per_cm", 3) // 300 dB/m
	rep := compareDocs(t, a, b)
	if rep.Equal {
		t.Fatal("want not equal")
	}
	if len(rep.Mismatches) != 1 {
		t.Fatalf("want exactly 1 mismatch, got %+v", rep.Mismatches)
	}
	m := rep.Mismatches[0]
	if m.DeviceID != "w" || m.Quantity != "loss" || !m.Numeric {
		t.Fatalf("mismatch = %+v", m)
	}
	if m.A != 200 || m.B != 300 || m.Diff != 100 {
		t.Fatalf("mismatch values = %+v, want A=200 B=300 Diff=100 (normalized)", m)
	}
	if !strings.Contains(m.Reason, "tolerance") {
		t.Fatalf("reason should cite tolerance: %q", m.Reason)
	}
	// 人类可读输出包含判定依据。
	s := rep.String()
	for _, want := range []string{"MISMATCH", "w", "loss", "200", "300"} {
		if !strings.Contains(s, want) {
			t.Fatalf("String() missing %q:\n%s", want, s)
		}
	}
}

func TestInequivalenceStructural(t *testing.T) {
	base := waveguideDoc("1.1", "", "", 0)
	// 器件只在一侧出现。
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
	rep := compareDocs(t, base, extra)
	if rep.Equal {
		t.Fatal("want not equal")
	}
	found := false
	for _, m := range rep.Mismatches {
		if m.DeviceID == "w2" && strings.Contains(m.Reason, "only one of the two documents") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing structural mismatch for w2: %+v", rep.Mismatches)
	}

	// 波长不同（电路级量）。
	other := strings.Replace(base, `"wavelength":1.55`, `"wavelength":1.31`, 1)
	rep = compareDocs(t, base, other)
	if rep.Equal || len(rep.Mismatches) != 1 || rep.Mismatches[0].Quantity != "wavelength" {
		t.Fatalf("wavelength mismatch = %+v", rep.Mismatches)
	}

	// 连接不同。
	renamed := strings.Replace(base, `"out0"`, `"outX"`, 1)
	rep = compareDocs(t, base, renamed)
	if rep.Equal {
		t.Fatal("want not equal for different connections")
	}
	found = false
	for _, m := range rep.Mismatches {
		if m.Quantity == "connections" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing connections mismatch: %+v", rep.Mismatches)
	}
}

// 版本组合无法确定语义时必须整体拒绝：返回错误且报告为 nil，
// 不能挑能解析的一份继续算。
func TestEquivalenceUndecidableVersionComboRejected(t *testing.T) {
	e := mustEngine(t)
	good := waveguideDoc("1.1", "", "", 0)
	bad := []string{
		waveguideDoc("9.9", "", "", 0),          // 版本超界
		waveguideDoc("1.0", "db_per_m", "", 0),  // 1.0 携带 1.1 字段
		waveguideDoc("1.1", "", "db_per_cm", 0), // 1.1 携带 1.2 字段
		`{"version":"1.1","circuit":{"id":"x","wavelength":1.55,"loss_unit":"db_per_m","loss_unit":"db_per_cm","devices":[],"connects":[]}}`, // 冲突声明
	}
	for _, doc := range bad {
		for _, order := range [][2]string{{doc, good}, {good, doc}} {
			rep, err := e.CompareModels(context.Background(), []byte(order[0]), []byte(order[1]))
			if err == nil {
				t.Fatalf("want refusal for undecidable combo:\nA=%s\nB=%s", order[0], order[1])
			}
			if rep != nil {
				t.Fatalf("partial report returned on refusal")
			}
		}
	}
}

func mustEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// 分束器 reflectivity 被写成百分比（30）等物理范围外取值时，
// 归一化为 theta 会产生 NaN，必须整体拒绝：返回错误、报告为 nil，
// 错误要点名器件、物理量与越界取值，不能硬凑出一份无法序列化的结果。
func TestEquivalenceReflectivityOutOfRangeRejected(t *testing.T) {
	e := mustEngine(t)
	good := bsDoc("1.1", `"reflectivity":0.25`)
	badVals := []string{"30", "-0.01", "1.0000001"}
	for _, v := range badVals {
		bad := bsDoc("1.1", `"reflectivity":`+v)
		for _, order := range [][2]string{{bad, good}, {good, bad}} {
			rep, err := e.CompareModels(context.Background(), []byte(order[0]), []byte(order[1]))
			if err == nil {
				t.Fatalf("reflectivity=%s: want refusal, got report:\n%s", v, rep.String())
			}
			if rep != nil {
				t.Fatalf("reflectivity=%s: partial report returned on refusal", v)
			}
			se, ok := AsSolverError(err)
			if !ok {
				t.Fatalf("reflectivity=%s: want *SolverError, got %T (%v)", v, err, err)
			}
			if se.Code != ErrInvalidParameter {
				t.Fatalf("reflectivity=%s: code=%s, want INVALID_PARAMETER", v, se.Code)
			}
			if se.Detail["device"] != "bs" || se.Detail["parameter"] != "reflectivity" {
				t.Fatalf("reflectivity=%s: detail missing device/parameter: %+v", v, se.Detail)
			}
			msg := err.Error()
			for _, want := range []string{"undecidable", "bs", "reflectivity"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("reflectivity=%s: error message missing %q: %s", v, want, msg)
				}
			}
		}
	}
}

// 同一分束器同时声明 theta 与 reflectivity 属于自相矛盾的声明，
// 必须整体拒绝，不能任选一种写法继续算甚至给出“相等”结论。
func TestEquivalenceThetaAndReflectivityConflictRejected(t *testing.T) {
	e := mustEngine(t)
	// 两种写法恰好物理一致（theta=π/6, r=0.25）也不允许同时出现。
	conflict := bsDoc("1.1", `"theta":0.5235987755982988,"reflectivity":0.25`)
	good := bsDoc("1.1", `"theta":0.5235987755982988`)
	for _, order := range [][2]string{{conflict, good}, {good, conflict}} {
		rep, err := e.CompareModels(context.Background(), []byte(order[0]), []byte(order[1]))
		if err == nil {
			t.Fatalf("conflicting declaration: want refusal, got:\n%s", rep.String())
		}
		if rep != nil {
			t.Fatalf("conflicting declaration: partial report returned on refusal")
		}
		se, ok := AsSolverError(err)
		if !ok {
			t.Fatalf("want *SolverError, got %T", err)
		}
		if se.Code != ErrInvalidParameter || se.Detail["device"] != "bs" {
			t.Fatalf("conflicting declaration error = %+v detail=%v", se, se.Detail)
		}
		if !strings.Contains(err.Error(), "theta") || !strings.Contains(err.Error(), "reflectivity") {
			t.Fatalf("error should name both declarations: %s", err.Error())
		}
	}
}

// 判不等时返回结果必须可直接 JSON 序列化，且逐项给出器件、物理量、
// 两边归一化取值、差值、原因；数值字段即便为 0 也不得缺项。
func TestInequivalenceReportIsCompleteAndSerializable(t *testing.T) {
	a := waveguideDoc("1.0", "", "", 200)        // 200 dB/m
	b := waveguideDoc("1.2", "", "db_per_cm", 3) // 300 dB/m
	rep := compareDocs(t, a, b)
	if rep.Equal {
		t.Fatal("want not equal")
	}
	raw, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("report must be JSON-serializable: %v\n%s", err, rep.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	mm, ok := decoded["mismatches"].([]any)
	if !ok || len(mm) != 1 {
		t.Fatalf("mismatches = %v", decoded["mismatches"])
	}
	m0 := mm[0].(map[string]any)
	for _, key := range []string{"device_id", "quantity", "numeric", "a", "b", "diff", "reason"} {
		if _, present := m0[key]; !present {
			t.Fatalf("mismatch JSON missing key %q: %s", key, raw)
		}
	}
	if m0["a"] != 200.0 || m0["b"] != 300.0 || m0["diff"] != 100.0 || m0["numeric"] != true {
		t.Fatalf("normalized evidence = %+v", m0)
	}
	if !strings.Contains(m0["reason"].(string), "tolerance") {
		t.Fatalf("reason = %v", m0["reason"])
	}

	// 结构性差异同样必须可序列化，且数值键固定存在（0 值，非缺项）。
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
	rep2 := compareDocs(t, base, extra)
	raw2, err := json.Marshal(rep2)
	if err != nil {
		t.Fatalf("structural report must be serializable: %v", err)
	}
	if !strings.Contains(string(raw2), `"numeric":false`) ||
		!strings.Contains(string(raw2), `"a":0`) {
		t.Fatalf("structural mismatch missing fixed numeric keys: %s", raw2)
	}
}

// 相等与不等两种结论都必须可正常序列化（历史上越界 reflectivity 产生 NaN
// 导致整个报告无法 Marshal）。
func TestEquivalenceReportAlwaysSerializable(t *testing.T) {
	// 相等情形。
	a := waveguideDoc("1.0", "", "", 200)
	b := waveguideDoc("1.2", "", "db_per_cm", 2)
	rep := compareDocs(t, a, b)
	if !rep.Equal {
		t.Fatalf("want equal:\n%s", rep.String())
	}
	if raw, err := json.Marshal(rep); err != nil {
		t.Fatalf("equal report not serializable: %v (json=%s)", err, raw)
	}
	// 相等报告里每个对照量也都必须是有限值。
	for _, d := range rep.Devices {
		for _, q := range d.Quantities {
			for _, v := range []float64{q.A, q.B, q.Diff} {
				if !isFinite(v) {
					t.Fatalf("non-finite quantity in equal report: %+v", q)
				}
			}
		}
	}
}

// 文档传入顺序相反时，结论（equal、逐项数值证据、JSON 形态）逐位一致。
func TestEquivalenceOrderIndependent(t *testing.T) {
	e := mustEngine(t)
	// 数值不等（loss 200 vs 300）。
	a := waveguideDoc("1.0", "", "", 200)
	b := waveguideDoc("1.2", "", "db_per_cm", 3)
	r1, err := e.CompareModels(context.Background(), []byte(a), []byte(b))
	if err != nil {
		t.Fatalf("CompareModels: %v", err)
	}
	r2, err := e.CompareModels(context.Background(), []byte(b), []byte(a))
	if err != nil {
		t.Fatalf("CompareModels reversed: %v", err)
	}
	j1, _ := json.Marshal(r1)
	j2, _ := json.Marshal(r2)
	if string(j1) != string(j2) {
		t.Fatalf("order dependence:\n%s\nvs\n%s", j1, j2)
	}
	if r1.Mismatches[0].A != 200 || r1.Mismatches[0].B != 300 {
		t.Fatalf("normalized pair should be order-independent: %+v", r1.Mismatches[0])
	}

	// 结构性差异（器件只在一侧）：原因文案也必须与顺序无关。
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
	s1, err := e.CompareModels(context.Background(), []byte(base), []byte(extra))
	if err != nil {
		t.Fatalf("CompareModels: %v", err)
	}
	s2, err := e.CompareModels(context.Background(), []byte(extra), []byte(base))
	if err != nil {
		t.Fatalf("CompareModels reversed: %v", err)
	}
	k1, _ := json.Marshal(s1)
	k2, _ := json.Marshal(s2)
	if string(k1) != string(k2) {
		t.Fatalf("structural order dependence:\n%s\nvs\n%s", k1, k2)
	}

	// 相等情形同样逐位一致。
	eq1 := compareDocs(t, a, waveguideDoc("1.2", "", "db_per_cm", 2))
	eq2 := compareDocs(t, waveguideDoc("1.2", "", "db_per_cm", 2), a)
	e1, _ := json.Marshal(eq1)
	e2, _ := json.Marshal(eq2)
	if string(e1) != string(e2) {
		t.Fatalf("equal-case order dependence:\n%s\nvs\n%s", e1, e2)
	}
}

// 并发反复调用（含正序/反序、相等/不等/拒绝三类输入）必须全部逐位一致，
// 且 -race 下无数据竞争。
func TestEquivalenceConcurrentDeterminism(t *testing.T) {
	e := mustEngine(t)
	docs := map[string][]byte{
		"lossA":    []byte(waveguideDoc("1.0", "", "", 200)),
		"lossB":    []byte(waveguideDoc("1.2", "", "db_per_cm", 3)),
		"lossSame": []byte(waveguideDoc("1.2", "", "db_per_cm", 2)),
		"badR":     []byte(bsDoc("1.1", `"reflectivity":30`)),
	}
	pairs := [][2]string{
		{"lossA", "lossB"},
		{"lossB", "lossA"},
		{"lossA", "lossSame"},
		{"lossSame", "lossA"},
		{"lossA", "badR"},
		{"badR", "lossA"},
	}
	want := map[string]string{}
	for _, p := range pairs {
		rep, err := e.CompareModels(context.Background(), docs[p[0]], docs[p[1]])
		var s string
		if err != nil {
			if rep != nil {
				t.Fatalf("pair %v: refusal must not carry a partial report", p)
			}
			se, _ := AsSolverError(err)
			s = "ERR:" + string(se.Code)
		} else {
			raw, mErr := json.Marshal(rep)
			if mErr != nil {
				t.Fatalf("pair %v: serializable baseline: %v", p, mErr)
			}
			s = "OK:" + string(raw)
		}
		want[p[0]+"|"+p[1]] = s
	}

	const goroutines = 32
	const rounds = 25
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				p := pairs[(seed+r)%len(pairs)]
				rep, err := e.CompareModels(context.Background(), docs[p[0]], docs[p[1]])
				var got string
				if err != nil {
					if rep != nil {
						errs <- fmt.Errorf("pair %v: partial report on refusal", p)
						return
					}
					se, _ := AsSolverError(err)
					got = "ERR:" + string(se.Code)
				} else {
					raw, mErr := json.Marshal(rep)
					if mErr != nil {
						errs <- fmt.Errorf("pair %v: non-serializable report: %w", p, mErr)
						return
					}
					got = "OK:" + string(raw)
				}
				if got != want[p[0]+"|"+p[1]] {
					errs <- fmt.Errorf("pair %v: non-deterministic result", p)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// 核验不得改动输入文档对应的已载入电路：比较后原电路的求解与审计结论不变。
func TestEquivalenceDoesNotMutateInputs(t *testing.T) {
	e := mustEngine(t)
	doc := waveguideDoc("1.2", "", "db_per_cm", 2)
	c, err := e.LoadModel(context.Background(), []byte(doc))
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	before, err := e.Audit(context.Background(), c)
	if err != nil {
		t.Fatalf("Audit before: %v", err)
	}
	beforeJSON, _ := json.Marshal(before)

	other := waveguideDoc("1.0", "", "", 201) // 与载入电路不等
	if rep, err := e.CompareModels(context.Background(), []byte(doc), []byte(other)); err != nil {
		t.Fatalf("CompareModels: %v", err)
	} else if rep.Equal {
		t.Fatal("want not equal")
	}
	// 再来一次非法输入，确认拒绝路径也不留任何影响。
	if _, err := e.CompareModels(context.Background(), []byte(doc), []byte(bsDoc("1.1", `"reflectivity":30`))); err == nil {
		t.Fatal("want refusal")
	}
	after, err := e.Audit(context.Background(), c)
	if err != nil {
		t.Fatalf("Audit after: %v", err)
	}
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) != string(afterJSON) {
		t.Fatalf("loaded circuit mutated by comparison:\nbefore=%s\nafter =%s", beforeJSON, afterJSON)
	}
}
