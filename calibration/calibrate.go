package calibration

import (
	"math"

	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
)

// calibrate locates the offset whose relative phase equals req.TargetDelta.
//
// The probe/reference relative phase is periodic and wrapped to (-pi, pi].
// A passive phase shifter rotates phase monotonically, but a 2*pi interval
// necessarily crosses one wrap cut. We therefore:
//  1. sample the mapping densely enough to detect the wrap direction,
//  2. place every sample on ONE continuous branch,
//  3. verify strict monotonicity (reject otherwise as non-convergent),
//  4. check the target is bracketed (reject out-of-range otherwise),
//  5. bisect to PhaseTol, never returning an unconverged value.
func (c *Calibrator) calibrate(req Request) (*Result, error) {
	if err := c.validateRequest(req); err != nil {
		return nil, err
	}
	// Sample the whole interval on a fine grid to follow the wrap cuts.
	const grid = 64
	off := make([]float64, grid+1)
	del := make([]float64, grid+1)
	for i := 0; i <= grid; i++ {
		off[i] = req.Low + (req.High-req.Low)*float64(i)/grid
		d, err := c.probeAt(req, off[i])
		if err != nil {
			return nil, err
		}
		del[i] = d
	}
	// Choose the continuous branch: decide direction by the smaller-step
	// evolution between neighbouring wrapped samples.
	branched := make([]float64, grid+1)
	branched[0] = del[0]
	for i := 1; i <= grid; i++ {
		branched[i] = unwrapNext(branched[i-1], del[i])
	}
	dLow, dHigh := branched[0], branched[grid]
	slope := dHigh - dLow
	if math.Abs(slope) < 1e-12 {
		return nil, engerrors.New(engerrors.KindCalibrationNoSolve,
			"probe phase is invariant to %s offset on [%.9g,%.9g]; no unique calibration",
			req.PhaseShifter, req.Low, req.High)
	}
	// Verify monotonicity on the dense continuous samples.
	increasing := slope > 0
	for i := 1; i <= grid; i++ {
		step := branched[i] - branched[i-1]
		if increasing && step < -c.cfg.MonotonicitySlack {
			return nil, engerrors.New(engerrors.KindCalibrationDiverge,
				"phase map is not monotone increasing near offset %.9g (step %.3e)", off[i], step)
		}
		if !increasing && step > c.cfg.MonotonicitySlack {
			return nil, engerrors.New(engerrors.KindCalibrationDiverge,
				"phase map is not monotone decreasing near offset %.9g (step %.3e)", off[i], step)
		}
	}
	// Put the wrapped target onto the continuous branch (try shifts of 2*pi).
	target := req.TargetDelta
	if increasing {
		if target < dLow {
			target += 2 * math.Pi * math.Ceil((dLow-target)/(2*math.Pi))
		}
		if target > dHigh {
			target -= 2 * math.Pi * math.Floor((target-dHigh)/(2*math.Pi))
		}
	} else {
		if target > dLow {
			target -= 2 * math.Pi * math.Ceil((target-dLow)/(2*math.Pi))
		}
		if target < dHigh {
			target += 2 * math.Pi * math.Floor((dHigh-target)/(2*math.Pi))
		}
	}
	if target < math.Min(dLow, dHigh)-c.cfg.MonotonicitySlack ||
		target > math.Max(dLow, dHigh)+c.cfg.MonotonicitySlack {
		return nil, engerrors.New(engerrors.KindCalibrationRange,
			"target delta %.9g rad is outside attainable interval [%.9g, %.9g] over offset [%.9g, %.9g]",
			req.TargetDelta, math.Min(dLow, dHigh), math.Max(dLow, dHigh), req.Low, req.High)
	}

	lo, hi := off[0], off[grid]
	flo, fhi := branched[0], branched[grid]
	if !increasing {
		lo, hi, flo, fhi = hi, lo, fhi, flo
	}
	// After orientation: flo <= fhi and flo <= target <= fhi.
	var mid float64
	iter := 0
	for iter = 0; iter < c.cfg.MaxIterations; iter++ {
		mid = (lo + hi) / 2
		fmidWrapped, err := c.probeAt(req, mid)
		if err != nil {
			return nil, err
		}
		// Put the mid sample on the continuous branch by matching it to the
		// nearer bracket endpoint; with a shrunk bracket both endpoints are on
		// the same branch, so the nearer one disambiguates the cut.
		fmid := nearerBranch(fmidWrapped, flo, fhi)
		if math.Abs(fmid-target) <= c.cfg.PhaseTol {
			return &Result{Offset: mid, AchievedDelta: wrapPhase(fmid), Iterations: iter + 1}, nil
		}
		if fmid < target {
			lo, flo = mid, fmid
		} else {
			hi, fhi = mid, fmid
		}
	}
	_ = fhi
	return nil, engerrors.New(engerrors.KindCalibrationDiverge,
		"calibration bisection did not reach phase tolerance %g within %d iterations (last %.9g)",
		c.cfg.PhaseTol, c.cfg.MaxIterations, mid)
}

func (c *Calibrator) validateRequest(req Request) error {
	if req.PhaseShifter == "" {
		return engerrors.New(engerrors.KindInvalidParameter, "missing phase shifter id")
	}
	if len(req.Excitations) == 0 {
		return engerrors.New(engerrors.KindInvalidParameter, "calibration requires at least one excitation")
	}
	if req.Probe == (solverProbe{}) || req.Reference == (solverProbe{}) {
		return engerrors.New(engerrors.KindInvalidParameter, "probe and reference must both be set")
	}
	if req.Probe == req.Reference {
		return engerrors.New(engerrors.KindInvalidParameter, "probe and reference must be distinct ports")
	}
	if math.IsNaN(req.TargetDelta) || math.IsInf(req.TargetDelta, 0) {
		return engerrors.New(engerrors.KindNonFiniteInput, "target delta is not finite")
	}
	if !(req.Low < req.High) {
		return engerrors.New(engerrors.KindInvalidParameter,
			"search interval invalid: low=%g must be < high=%g", req.Low, req.High)
	}
	return nil
}
