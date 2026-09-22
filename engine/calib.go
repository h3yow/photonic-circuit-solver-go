package engine

import (
	"github.com/h3yow/photonic-circuit-solver-go/calibration"
	"github.com/h3yow/photonic-circuit-solver-go/solver"
)

func calibKey(circuitKey, shifter string, target float64) string {
	return circuitKey + "|" + shifter
}

func (e *Engine) calibrate(circuitKey string, req calibration.Request) (*CalibrationRecord, error) {
	ce, err := e.entry(circuitKey)
	if err != nil {
		return nil, err
	}
	// 1) Already calibrated? Return the immutable record (no repeat run).
	e.calibMu.Lock()
	if rec, ok := e.records[calibKey(circuitKey, req.PhaseShifter, req.TargetDelta)]; ok {
		e.calibMu.Unlock()
		return rec, nil
	}
	// 2) Identical run in flight? Coalesce: exactly ONE calibration runs.
	if call, ok := e.calls[calibKey(circuitKey, req.PhaseShifter, req.TargetDelta)]; ok {
		e.calibMu.Unlock()
		<-call.done
		return call.rec, call.err
	}
	call := &calibCall{done: make(chan struct{})}
	e.calls[calibKey(circuitKey, req.PhaseShifter, req.TargetDelta)] = call
	e.calibMu.Unlock()

	// 3) Run the calibration on a PRIVATE snapshot+solver.
	rec, runErr := e.runCalibration(ce, req)

	e.calibMu.Lock()
	call.rec, call.err = rec, runErr
	close(call.done)
	delete(e.calls, calibKey(circuitKey, req.PhaseShifter, req.TargetDelta))
	if runErr == nil {
		e.records[calibKey(circuitKey, req.PhaseShifter, req.TargetDelta)] = rec
	}
	e.calibMu.Unlock()
	return rec, runErr
}

func (e *Engine) runCalibration(ce *cacheEntry, req calibration.Request) (*CalibrationRecord, error) {
	// The calibrator clones+compiles a PRIVATE model for every probe offset,
	// so the shared immutable compiled circuit is only used for reading.
	cfg := e.cfg.Solver
	cfg.MaxIterations = e.effectiveIter(cfg.MaxIterations)
	sv, err := solver.New(ce.compiled, cfg)
	if err != nil {
		return nil, err
	}
	ccfg := e.cfg.Calib
	ccfg.MaxIterations = minInt(ccfg.MaxIterations, e.cfg.Limits.MaxIterations)
	cal := calibration.New(sv, ccfg)
	res, err := cal.Calibrate(req)
	if err != nil {
		return nil, err
	}
	return &CalibrationRecord{
		CircuitKey:    e.keyOf(ce),
		PhaseShifter:  req.PhaseShifter,
		Offset:        res.Offset,
		AchievedDelta: res.AchievedDelta,
	}, nil
}

func (e *Engine) compensateDrift(circuitKey string, req calibration.Request, observedDelta, storedOffset float64) (*CalibrationRecord, error) {
	ce, err := e.entry(circuitKey)
	if err != nil {
		return nil, err
	}
	cfg := e.cfg.Solver
	cfg.MaxIterations = e.effectiveIter(cfg.MaxIterations)
	sv, err := solver.New(ce.compiled, cfg)
	if err != nil {
		return nil, err
	}
	ccfg := e.cfg.Calib
	ccfg.MaxIterations = minInt(ccfg.MaxIterations, e.cfg.Limits.MaxIterations)
	cal := calibration.New(sv, ccfg)
	res, err := cal.CompensateDrift(req, observedDelta, storedOffset)
	if err != nil {
		return nil, err
	}
	return &CalibrationRecord{
		CircuitKey:    e.keyOf(ce),
		PhaseShifter:  req.PhaseShifter,
		Offset:        res.Offset,
		AchievedDelta: res.AchievedDelta,
	}, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
