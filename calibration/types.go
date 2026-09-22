// Package calibration implements reproducible phase-shifter calibration and
// drift compensation.
//
// Calibration determines the static offset of one phase shifter so that the
// phase transfer between a measured probe and a reference probe takes a
// requested target. Because achievable relative phase on a passive feed-forward
// circuit spans an interval smaller than the full 2*pi for arbitrary probes,
// targets outside that interval are rejected (calibration_out_of_range) rather
// than wrapped to a misleading value.
//
// Monotonicity: the relative probe phase is monotone in the shifter offset
// over the calibrated interval; this is verified (never assumed) during the
// bisection, and a violated mapping aborts with calibration_not_converged.
package calibration

import (
	"github.com/h3yow/photonic-circuit-solver-go/solver"
)

// Request describes one phase calibration.
type Request struct {
	// PhaseShifter is the device to calibrate (must be type phase_shifter).
	PhaseShifter string
	// Excitations drive the circuit during calibration.
	Excitations []solver.Excitation
	// Probe is the output whose relative phase is controlled.
	Probe solver.Probe
	// Reference is the output whose phase defines zero. It must be distinct
	// from Probe and carry nonzero amplitude across the calibration.
	Reference solver.Probe
	// TargetDelta is the desired arg(Probe)-arg(Reference) in radians.
	TargetDelta float64
	// Low/High bound the search interval of offset (rad). The attainable
	// range is [Low, High]; targets requiring offset outside are rejected.
	Low  float64
	High float64
}

// Result is a successful calibration.
type Result struct {
	// Offset is the calibrated static offset in radians.
	Offset float64
	// AchievedDelta is the relative phase actually obtained.
	AchievedDelta float64
	// Iterations used by the bisection.
	Iterations int
}

// Calibrator performs calibrations on an immutable compiled circuit.
type Calibrator struct {
	solver *solver.Solver
	cfg    Config
}

// Config controls convergence of the monotone bisection.
type Config struct {
	MaxIterations int
	// PhaseTol bounds |achieved-target| (radians) at success.
	PhaseTol float64
	// MinAmplitude rejects (nearly) dark probes whose phase is undefined.
	MinAmplitude float64
	// MonotonicitySlack allows tiny numerical non-monotonicity.
	MonotonicitySlack float64
}

// DefaultConfig returns the documented calibration defaults.
func DefaultConfig() Config { return defaultConfig() }

// New builds a Calibrator around an existing solver.
func New(s *solver.Solver, cfg Config) *Calibrator { return newCalibrator(s, cfg) }

// Calibrate finds the offset meeting the request or returns a classified error.
func (c *Calibrator) Calibrate(req Request) (*Result, error) { return c.calibrate(req) }

// CompensateDrift re-derives an offset that restores a previously calibrated
// target under a measured drift, given a fresh observed delta at the stored
// offset. It never returns an unconverged or out-of-range value.
func (c *Calibrator) CompensateDrift(req Request, observedDeltaAtOffset, storedOffset float64) (*Result, error) {
	return c.compensateDrift(req, observedDeltaAtOffset, storedOffset)
}
