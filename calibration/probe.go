package calibration

import (
	"math"
	"math/cmplx"

	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
	"github.com/h3yow/photonic-circuit-solver-go/model"
	"github.com/h3yow/photonic-circuit-solver-go/solver"
	"github.com/h3yow/photonic-circuit-solver-go/topology"
)

// probeAt builds a PRIVATE model/solver clone with the shifter offset set to
// off, solves it (linear method cross-checked against sweep) and returns the
// wrapped relative phase arg(probe)-arg(reference).
func (c *Calibrator) probeAt(req Request, off float64) (float64, error) {
	comp := c.compiled()
	clone, err := cloneModel(comp.Model)
	if err != nil {
		return 0, err
	}
	found := false
	for i := range clone.Devices {
		if clone.Devices[i].ID == req.PhaseShifter {
			if clone.Devices[i].Type != model.DevicePhaseShifter {
				return 0, engerrors.New(engerrors.KindInvalidParameter,
					"calibration target %q is %s, not phase_shifter",
					req.PhaseShifter, clone.Devices[i].Type)
			}
			clone.Devices[i].Params.Offset = off
			found = true
			break
		}
	}
	if !found {
		return 0, engerrors.New(engerrors.KindMissingPort,
			"calibration target device %q not found", req.PhaseShifter)
	}
	cc, err := topology.Compile(clone)
	if err != nil {
		return 0, err
	}
	priv, err := solver.New(cc, c.solver.NumericConfig())
	if err != nil {
		return 0, err
	}
	sreq := solver.Request{
		Excitations: req.Excitations,
		Probes:      []solver.Probe{req.Probe, req.Reference},
	}
	res, err := priv.SolveCrossChecked(solver.MethodLinear, solver.MethodSweep, sreq)
	if err != nil {
		return 0, err
	}
	zp, zr := res.Outputs[0], res.Outputs[1]
	if cmplx.Abs(zp) < c.cfg.MinAmplitude {
		return 0, engerrors.New(engerrors.KindCalibrationNoSolve,
			"probe %s.%s is dark (|amp|=%.3e < %.0e); phase undefined at offset %.9g",
			req.Probe.DeviceID, req.Probe.Port, cmplx.Abs(zp), c.cfg.MinAmplitude, off)
	}
	if cmplx.Abs(zr) < c.cfg.MinAmplitude {
		return 0, engerrors.New(engerrors.KindCalibrationNoSolve,
			"reference %s.%s is dark (|amp|=%.3e < %.0e); reference phase undefined at offset %.9g",
			req.Reference.DeviceID, req.Reference.Port, cmplx.Abs(zr), c.cfg.MinAmplitude, off)
	}
	delta := cmplx.Phase(zp) - cmplx.Phase(zr)
	// wrap into (-pi, pi]
	for delta > math.Pi {
		delta -= 2 * math.Pi
	}
	for delta <= -math.Pi {
		delta += 2 * math.Pi
	}
	return delta, nil
}

func cloneModel(m *model.CircuitModel) (*model.CircuitModel, error) {
	raw, err := model.Encode(m, m.Version)
	if err != nil {
		return nil, err
	}
	return model.Decode(raw)
}
