package engine

import (
	"github.com/h3yow/photonic-circuit-solver-go/calibration"
	"github.com/h3yow/photonic-circuit-solver-go/solver"
)

// DefaultConfig returns the documented engine defaults.
func DefaultConfig() Config { return defaultConfig() }

// Load decodes, validates and compiles a model, caching the immutable result
// under its content-derived key.
func (e *Engine) Load(raw []byte) (string, error) { return e.load(raw) }

// SolveOne runs one solve request.
func (e *Engine) SolveOne(key string, req SolveRequest) (*solver.Result, error) {
	return e.solveOne(key, req)
}

// BatchSolve runs a bounded batch; on failure no partial results are returned.
func (e *Engine) BatchSolve(key string, reqs []SolveRequest) ([]*solver.Result, error) {
	return e.batchSolve(key, reqs)
}

// Calibrate runs (or joins an in-flight identical) calibration and records the
// resulting offset. Concurrent identical calls share ONE calibration run.
func (e *Engine) Calibrate(circuitKey string, req calibration.Request) (*CalibrationRecord, error) {
	return e.calibrate(circuitKey, req)
}

// CompensateDrift runs drift compensation for a previously calibrated shifter.
func (e *Engine) CompensateDrift(circuitKey string, req calibration.Request, observedDelta, storedOffset float64) (*CalibrationRecord, error) {
	return e.compensateDrift(circuitKey, req, observedDelta, storedOffset)
}
