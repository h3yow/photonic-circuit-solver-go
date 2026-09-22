package calibration

import (
	"github.com/h3yow/photonic-circuit-solver-go/solver"
	"github.com/h3yow/photonic-circuit-solver-go/topology"
)

func defaultConfig() Config {
	return Config{
		MaxIterations:     200,
		PhaseTol:          1e-8,
		MinAmplitude:      1e-7,
		MonotonicitySlack: 1e-9,
	}
}

func newCalibrator(s *solver.Solver, cfg Config) *Calibrator {
	if cfg.MaxIterations <= 0 {
		cfg = defaultConfig()
	}
	return &Calibrator{solver: s, cfg: cfg}
}

func (c *Calibrator) compiled() *topology.Compiled { return c.solver.Compiled() }
