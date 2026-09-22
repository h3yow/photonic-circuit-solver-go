package calibration

import (
	"math"

	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
)

// compensateDrift restores a previously calibrated target after drift.
//
// Inputs:
//
//	req.TargetDelta - desired relative phase
//	storedOffset    - offset that produced TargetDelta at calibration time
//	observedDelta   - relative phase measured NOW at storedOffset
//
// Method (deterministic and reproducible):
//  1. measure the local numerical sensitivity s = d(delta)/d(offset) at the
//     stored point by two close probes; reject the flat (~zero s) case;
//  2. Newton-correct the offset by -slip/s, reject if it leaves [Low, High];
//  3. verify the achieved relative phase against TargetDelta to PhaseTol by
//     probing the corrected offset; iterate the Newton refinement a bounded
//     number of times. An unconverged correction is never returned.
func (c *Calibrator) compensateDrift(req Request, observedDelta, storedOffset float64) (*Result, error) {
	if err := c.validateRequest(req); err != nil {
		return nil, err
	}
	if math.IsNaN(observedDelta) || math.IsInf(observedDelta, 0) {
		return nil, engerrors.New(engerrors.KindNonFiniteInput, "observed delta is not finite")
	}
	if storedOffset < req.Low || storedOffset > req.High {
		return nil, engerrors.New(engerrors.KindCalibrationRange,
			"stored offset %.9g lies outside supported interval [%.9g,%.9g]",
			storedOffset, req.Low, req.High)
	}
	slip0 := wrapPhase(observedDelta - req.TargetDelta)
	if math.Abs(slip0) <= c.cfg.PhaseTol {
		// No measurable drift.
		dNow, err := c.probeAt(req, storedOffset)
		if err != nil {
			return nil, err
		}
		return &Result{Offset: storedOffset, AchievedDelta: dNow, Iterations: 0}, nil
	}

	eps := math.Max(1e-5, (req.High-req.Low)*1e-4)
	off := storedOffset
	slip := slip0
	iter := 0
	for ; iter < c.cfg.MaxIterations; iter++ {
		// Numerical sensitivity at the current offset.
		ep := eps
		if off+ep > req.High || off-ep < req.Low {
			ep = math.Min(ep, math.Min(off-req.Low, req.High-off))
			if ep <= 0 {
				return nil, engerrors.New(engerrors.KindCalibrationRange,
					"cannot evaluate sensitivity at boundary offset %.9g of [%.9g,%.9g]",
					off, req.Low, req.High)
			}
		}
		dPlus, err := c.probeAt(req, off+ep)
		if err != nil {
			return nil, err
		}
		dMinus, err := c.probeAt(req, off-ep)
		if err != nil {
			return nil, err
		}
		dv := wrapPhase(dPlus - dMinus)
		s := dv / (2 * ep)
		if math.Abs(s) < 1e-6 {
			return nil, engerrors.New(engerrors.KindCalibrationNoSolve,
				"phase sensitivity at offset %.9g is ~0 (s=%.3e); drift is not compensable there",
				off, s)
		}
		candidate := off - slip/s
		if candidate < req.Low || candidate > req.High {
			return nil, engerrors.New(engerrors.KindCalibrationRange,
				"drift correction would move offset to %.9g, outside supported [%.9g,%.9g]",
				candidate, req.Low, req.High)
		}
		off = candidate
		dNow, err := c.probeAt(req, off)
		if err != nil {
			return nil, err
		}
		slip = wrapPhase(dNow - req.TargetDelta)
		if math.Abs(slip) <= c.cfg.PhaseTol {
			return &Result{Offset: off, AchievedDelta: dNow, Iterations: iter + 1}, nil
		}
	}
	return nil, engerrors.New(engerrors.KindCalibrationDiverge,
		"drift compensation did not converge within %d iterations (residual %.3e)",
		c.cfg.MaxIterations, math.Abs(slip))
}

func wrapPhase(p float64) float64 {
	for p > math.Pi {
		p -= 2 * math.Pi
	}
	for p <= -math.Pi {
		p += 2 * math.Pi
	}
	return p
}

// unwrapNext returns v shifted by a multiple of 2*pi so that it lies on the
// continuous branch adjacent to anchor (|v-anchor| <= pi).
func unwrapNext(anchor, v float64) float64 {
	for v-anchor > math.Pi {
		v -= 2 * math.Pi
	}
	for anchor-v > math.Pi {
		v += 2 * math.Pi
	}
	return v
}

// nearerBranch shifts wrapped value v by 2*pi multiples to the representation
// closest to the interval spanned by the two bracket samples.
func nearerBranch(v, lo, hi float64) float64 {
	center := (lo + hi) / 2
	for v-center > math.Pi {
		v -= 2 * math.Pi
	}
	for center-v > math.Pi {
		v += 2 * math.Pi
	}
	return v
}
