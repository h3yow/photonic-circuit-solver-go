package model

import (
	"math"

	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
)

// Defaults fills stable default parameters for devices whose fields are zero
// / absent. Defaults are identical across all supported versions: evolution
// never changes default meaning; new fields get explicit zero/default values
// documented in docs/model-versioning.md.
//
//	waveguide:     phase = 0
//	beam_splitter: theta = pi/4 (50:50)
//	phase_shifter: bias=0, offset=0, gain=1 (rad per control unit)
//	ring_cavity:   no defaults; r/a/phi_round are mandatory
func (m *CircuitModel) Defaults() error {
	seen := map[string]bool{}
	for i := range m.Devices {
		d := &m.Devices[i]
		if d.ID == "" {
			return engerrors.New(engerrors.KindInvalidParameter, "device at index %d has empty id", i)
		}
		if seen[d.ID] {
			return engerrors.New(engerrors.KindInvalidParameter, "duplicate device id %q", d.ID)
		}
		seen[d.ID] = true
		if !isKnownDeviceType(d.Type) {
			return engerrors.New(engerrors.KindUnknownDeviceType, "device %q has unknown type %q", d.ID, d.Type)
		}
		if d.Modes == 0 {
			d.Modes = 1
		}
		if d.Modes < 1 {
			return engerrors.New(engerrors.KindPortDimension,
				"device %q declares invalid modes=%d", d.ID, d.Modes)
		}
		switch d.Type {
		case DeviceWaveguide:
			// phase may be any real finite value; zero default is natural.
		case DeviceBeamSplitter:
			if d.Params.Theta == 0 {
				d.Params.Theta = math.Pi / 4
			}
		case DevicePhaseShifter:
			if d.Params.Gain == nil {
				g := 1.0
				d.Params.Gain = &g
			}
		case DeviceRingCavity:
			// mandatory: no silent defaults for resonator physics.
		}
		if err := validateParams(d); err != nil {
			return err
		}
	}
	return nil
}

func validateParams(d *Device) error {
	p := d.Params
	finite := func(name string, v float64) error {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return engerrors.New(engerrors.KindNonFiniteInput,
				"device %q parameter %s is not finite", d.ID, name)
		}
		return nil
	}
	switch d.Type {
	case DeviceWaveguide:
		if err := finite("phase", p.Phase); err != nil {
			return err
		}
	case DeviceBeamSplitter:
		if err := finite("theta", p.Theta); err != nil {
			return err
		}
		if p.Theta < 0 || p.Theta > math.Pi/2 {
			return engerrors.New(engerrors.KindOutOfPhysicalRange,
				"device %q theta=%g outside passive range [0, pi/2]", d.ID, p.Theta)
		}
	case DevicePhaseShifter:
		for name, v := range map[string]float64{"bias": p.Bias, "offset": p.Offset} {
			if err := finite(name, v); err != nil {
				return err
			}
		}
		if p.Gain == nil {
			return engerrors.New(engerrors.KindInvalidParameter, "device %q has nil gain", d.ID)
		}
		if err := finite("gain", *p.Gain); err != nil {
			return err
		}
		if *p.Gain <= 0 {
			return engerrors.New(engerrors.KindOutOfPhysicalRange,
				"device %q gain=%g must be positive", d.ID, *p.Gain)
		}
	case DeviceRingCavity:
		for name, v := range map[string]float64{"phi_round": p.PhiRound, "r": p.R, "a": p.A} {
			if err := finite(name, v); err != nil {
				return err
			}
		}
		// |r|=1 and |a|=1 are lossless/extreme points excluded to keep the
		// resonator strictly passive with finite linewidth; a<=0 invalid.
		if p.R <= -1 || p.R >= 1 {
			return engerrors.New(engerrors.KindOutOfPhysicalRange,
				"device %q r=%g must satisfy |r|<1 (passive, non-degenerate)", d.ID, p.R)
		}
		if p.A <= 0 || p.A >= 1 {
			return engerrors.New(engerrors.KindOutOfPhysicalRange,
				"device %q a=%g must satisfy 0<a<1 (round-trip loss)", d.ID, p.A)
		}
	}
	return nil
}
