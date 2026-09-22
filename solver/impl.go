package solver

import (
	"math/cmplx"

	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
	"github.com/h3yow/photonic-circuit-solver-go/model"
	"github.com/h3yow/photonic-circuit-solver-go/topology"
)

func defaultConfig() Config {
	return Config{
		MaxIterations: 10000,
		AbsTol:        1e-10,
		RelTol:        1e-9,
	}
}

func newSolver(c *topology.Compiled, cfg Config) (*Solver, error) {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = defaultConfig().MaxIterations
	}
	if cfg.AbsTol <= 0 {
		cfg.AbsTol = defaultConfig().AbsTol
	}
	if cfg.RelTol <= 0 {
		cfg.RelTol = defaultConfig().RelTol
	}
	s := &Solver{compiled: c, cfg: cfg}
	base := 0
	for i := range c.Model.Devices {
		d := &c.Model.Devices[i]
		n := model.PortCount(d.Type)
		blk := deviceBlock{base: base, size: n, s: make([]complex128, n*n)}
		fillDeviceS(&blk, d)
		s.blocks = append(s.blocks, blk)
		base += n
	}
	return s, nil
}

func (s *Solver) solve(method Method, req Request) (*Result, error) {
	switch method {
	case MethodLinear:
		return s.solveLinear(req)
	case MethodSweep:
		return s.solveSweep(req)
	case MethodJacobi:
		return s.solveJacobi(req)
	default:
		return nil, engerrors.New(engerrors.KindInvalidParameter, "unknown solver method %q", method)
	}
}

func (s *Solver) solveCrossChecked(primary, secondary Method, req Request) (*Result, error) {
	r1, err := s.solve(primary, req)
	if err != nil {
		return nil, err
	}
	r2, err := s.solve(secondary, req)
	if err != nil {
		return nil, err
	}
	if err := s.compareResults(primary, secondary, r1, r2); err != nil {
		return nil, err
	}
	// Return the primary result: agreement is verified rather than assumed.
	return r1, nil
}

// compareResults classifies any disagreement between two independent methods
// as KindSolverDivergence (never silently selects one result).
func (s *Solver) compareResults(primary, secondary Method, r1, r2 *Result) error {
	for i := range r1.Outputs {
		diff := cmplx.Abs(r1.Outputs[i] - r2.Outputs[i])
		scale := cmplx.Abs(r1.Outputs[i])
		if diff > s.cfg.AbsTol+s.cfg.RelTol*scale {
			return engerrors.New(engerrors.KindSolverDivergence,
				"solver disagreement at probe #%d: %s=%v vs %s=%v (|diff|=%.3e, tol=%.3e)",
				i, primary, r1.Outputs[i], secondary, r2.Outputs[i],
				diff, s.cfg.AbsTol+s.cfg.RelTol*scale)
		}
	}
	return nil
}
