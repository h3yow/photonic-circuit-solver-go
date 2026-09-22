package solver

import (
	"math"

	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
	"github.com/h3yow/photonic-circuit-solver-go/topology"
)

// solveJacobi finds the entering-wave vector by fixed-point iteration:
//
//	x[p] = src[p]                 external input port
//	x[p] = 0                      external output port
//	x[p] = emit_q(enter)          linked port, q = peer port
//
// enter[c] = src[c] for external inputs else x[c]. On a passive DAG the
// iteration converges geometrically; the cap protects against misuse.
func (s *Solver) solveJacobi(req Request) (*Result, error) {
	src, totalIn, err := s.validateAndSource(req)
	if err != nil {
		return nil, err
	}
	n := len(src)
	x := make([]complex128, n)
	// Fix external rows from the start.
	for p := 0; p < n; p++ {
		gp := s.compiled.Ports[p]
		dt := s.deviceTypeAt(gp.DeviceID)
		if s.isExternal(p) && topology.IsInputPort(dt, gp.PortName) {
			x[p] = src[p]
		}
	}
	maxIter := s.cfg.MaxIterations
	for iter := 0; iter < maxIter; iter++ {
		emit := s.outgoing(x, src)
		next := make([]complex128, n)
		res := 0.0
		for p := 0; p < n; p++ {
			gp := s.compiled.Ports[p]
			dt := s.deviceTypeAt(gp.DeviceID)
			switch {
			case s.isExternal(p) && topology.IsInputPort(dt, gp.PortName):
				next[p] = src[p]
			case s.isExternal(p):
				next[p] = 0
			default:
				if q := s.compiled.Link[p]; q >= 0 {
					next[p] = emit[q]
				}
			}
			d := next[p] - x[p]
			res = math.Max(res, real(d)*real(d)+imag(d)*imag(d))
		}
		x = next
		if math.Sqrt(res) <= s.cfg.AbsTol+s.cfg.RelTol*vecNorm(x) {
			return s.extract(req, x, src, totalIn)
		}
	}
	return nil, engerrors.New(engerrors.KindSolverIterations,
		"Jacobi propagation did not converge within %d iterations", maxIter)
}

func vecNorm(x []complex128) float64 {
	sum := 0.0
	for _, v := range x {
		sum += real(v)*real(v) + imag(v)*imag(v)
	}
	return math.Sqrt(sum)
}
