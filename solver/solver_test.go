package solver

import (
	"math"
	"math/cmplx"
	"testing"

	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
	"github.com/h3yow/photonic-circuit-solver-go/model"
	"github.com/h3yow/photonic-circuit-solver-go/topology"
)

const mziJSON = `{
  "version":"1.1",
  "devices":[
    {"id":"b1","type":"beam_splitter","params":{"theta":0.7853981633974483}},
    {"id":"ps","type":"phase_shifter","params":{"bias":0.77}},
    {"id":"wg","type":"waveguide","params":{"phase":0.11}},
    {"id":"b2","type":"beam_splitter","params":{"theta":0.7853981633974483}}
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

func buildSolver(t *testing.T, json string) *Solver {
	t.Helper()
	m, err := model.Decode([]byte(json))
	if err != nil {
		t.Fatalf("decode: %v\ninput=%s", err, json)
	}
	c, err := topology.Compile(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	s, err := New(c, DefaultConfig())
	if err != nil {
		t.Fatalf("new solver: %v", err)
	}
	return s
}

func TestThreeMethodsAgree(t *testing.T) {
	s := buildSolver(t, mziJSON)
	req := Request{
		Excitations: []Excitation{{"b1", "in1", 1}},
		Probes:      []Probe{{"b2", "out1"}, {"b2", "out2"}},
	}
	results := map[Method]*Result{}
	for _, m := range []Method{MethodLinear, MethodSweep, MethodJacobi} {
		r, err := s.Solve(m, req)
		if err != nil {
			t.Fatalf("method %s: %v", m, err)
		}
		results[m] = r
	}
	l := results[MethodLinear]
	for _, m := range []Method{MethodSweep, MethodJacobi} {
		for i := range l.Outputs {
			if d := cmplx.Abs(l.Outputs[i] - results[m].Outputs[i]); d > 1e-9 {
				t.Fatalf("input MZI, method=%s probe#%d: linear=%v %s=%v |diff|=%.3e exceeds tol",
					m, i, l.Outputs[i], m, results[m].Outputs[i], d)
			}
		}
	}
}

func TestEnergyConservation(t *testing.T) {
	s := buildSolver(t, mziJSON)
	for _, amp := range []complex128{1, 0.5 + 0.5i, complex(0.3, -0.7)} {
		req := Request{
			Excitations: []Excitation{{"b1", "in1", amp}},
			Probes:      []Probe{{"b2", "out1"}, {"b2", "out2"}},
		}
		r, err := s.SolveCrossChecked(MethodLinear, MethodSweep, req)
		if err != nil {
			t.Fatalf("amp=%v: %v", amp, err)
		}
		pin := norm2(amp)
		pout := norm2(r.Outputs[0]) + norm2(r.Outputs[1])
		if math.Abs(pout-pin) > 1e-9 {
			t.Fatalf("amp=%v: power not conserved in=%.9f out=%.9f diff=%.3e", amp, pin, pout, pout-pin)
		}
	}
}

func TestCrossCheckPasses(t *testing.T) {
	s := buildSolver(t, mziJSON)
	req := Request{Excitations: []Excitation{{"b1", "in1", 1}}, Probes: []Probe{{"b2", "out1"}}}
	if _, err := s.SolveCrossChecked(MethodLinear, MethodJacobi, req); err != nil {
		t.Fatalf("independent methods must agree: %v", err)
	}
}

func TestCrossCheckReportsDivergence(t *testing.T) {
	s := buildSolver(t, mziJSON)
	// Two independently produced results that differ beyond tolerance must be
	// reported as divergence (exercising the comparison directly models an
	// inconsistent second method; shared blocks make in-place corruption
	// affect both paths, so the classification is tested at its boundary).
	r1 := &Result{Outputs: []complex128{0.5 + 0.5i}}
	r2 := &Result{Outputs: []complex128{0.5 + 0.6i}} // |diff| = 0.1
	err := s.compareResults(MethodLinear, MethodJacobi, r1, r2)
	if got := engerrors.KindOf(err); got != engerrors.KindSolverDivergence {
		t.Fatalf("input r1=%v r2=%v diff=0.1: want solver_divergence, got %v", r1.Outputs, r2.Outputs, err)
	}
	// Results within tolerance agree.
	r3 := &Result{Outputs: []complex128{0.5 + 0.5000000001i}}
	if err := s.compareResults(MethodLinear, MethodSweep, r1, r3); err != nil {
		t.Fatalf("sub-tolerance difference must pass, got %v", err)
	}
}

func TestInvalidExcitations(t *testing.T) {
	s := buildSolver(t, mziJSON)
	base := func() Request {
		return Request{Probes: []Probe{{"b2", "out1"}}}
	}
	cases := []struct {
		name string
		req  Request
		want engerrors.Kind
	}{
		{"non-finite", withExc(base(), Excitation{"b1", "in1", complex(math.NaN(), 0)}), engerrors.KindNonFiniteInput},
		{"+inf", withExc(base(), Excitation{"b1", "in1", complex(math.Inf(1), 0)}), engerrors.KindNonFiniteInput},
		{"amplitude over power bound", withExc(base(), Excitation{"b1", "in1", 2}), engerrors.KindOutOfPhysicalRange},
		{"excitation on output port", withExc(base(), Excitation{"b2", "out1", 1}), engerrors.KindInvalidParameter},
		{"excitation on internal", withExc(base(), Excitation{"ps", "in", 1}), engerrors.KindInvalidParameter},
		{"missing device", withExc(base(), Excitation{"nope", "in1", 1}), engerrors.KindMissingPort},
		{"probe on input", Request{Excitations: []Excitation{{"b1", "in1", 1}}, Probes: []Probe{{"b1", "in1"}}}, engerrors.KindNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Solve(MethodLinear, tc.req)
			got := engerrors.KindOf(err)
			if tc.want == engerrors.KindNone {
				if err != nil {
					t.Fatalf("probe on own input is allowed as reference, got %v", err)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("input=%+v\nerr=%v\nwant kind=%q got=%q", tc.req, err, tc.want, got)
			}
		})
	}
}

func TestTotalInputPowerBound(t *testing.T) {
	s := buildSolver(t, mziJSON)
	req := Request{
		Excitations: []Excitation{{"b1", "in1", 0.8}, {"b1", "in2", 0.8}},
		Probes:      []Probe{{"b2", "out1"}, {"b2", "out2"}},
	}
	_, err := s.Solve(MethodLinear, req)
	if got := engerrors.KindOf(err); got != engerrors.KindOutOfPhysicalRange {
		t.Fatalf("total input power 1.28 must be rejected, got %v", err)
	}
}

func TestRingMatchesAnalytic(t *testing.T) {
	s := buildSolver(t, `{
	  "version":"1.0",
	  "devices":[{"id":"r","type":"ring_cavity","params":{"phi_round":0.6,"r":0.95,"a":0.9}}],
	  "connections":[],
	  "external_ports":[{"device":"r","port":"in"},{"device":"r","port":"out"}]
	}`)
	req := Request{Excitations: []Excitation{{"r", "in", 1}}, Probes: []Probe{{"r", "out"}}}
	r, err := s.SolveCrossChecked(MethodLinear, MethodJacobi, req)
	if err != nil {
		t.Fatal(err)
	}
	got := r.Outputs[0]
	phi, rr, aa := 0.6, 0.95, 0.9
	want := complex(rr, 0)
	want -= complex(aa, 0) * cmplx.Exp(complex(0, -phi))
	want /= 1 - complex(aa*rr, 0)*cmplx.Exp(complex(0, -phi))
	if cmplx.Abs(got-want) > 1e-12 {
		t.Fatalf("ring mismatch got=%v want=%v", got, want)
	}
}

func TestJacobiIterationLimit(t *testing.T) {
	s := buildSolver(t, mziJSON)
	cfg := Config{MaxIterations: 1, AbsTol: 1e-12, RelTol: 1e-12}
	s.cfg = cfg
	req := Request{Excitations: []Excitation{{"b1", "in1", 1}}, Probes: []Probe{{"b2", "out1"}}}
	_, err := s.Solve(MethodJacobi, req)
	if got := engerrors.KindOf(err); got != engerrors.KindSolverIterations {
		t.Fatalf("iteration cap must produce solver_iteration_limit, got %v", err)
	}
}

func withExc(r Request, e Excitation) Request { r.Excitations = []Excitation{e}; return r }

func norm2(z complex128) float64 { return real(z)*real(z) + imag(z)*imag(z) }
