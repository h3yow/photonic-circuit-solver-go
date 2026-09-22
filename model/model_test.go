package model

import (
	"math"
	"strings"
	"testing"

	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
)

func TestDecode_VersionSupport(t *testing.T) {
	cases := []struct {
		name    string
		version string
		want    engerrors.Kind
	}{
		{"v1.0 accepted", "1.0", engerrors.KindNone},
		{"v1.1 accepted", "1.1", engerrors.KindNone},
		{"v0.9 too old", "0.9", engerrors.KindUnsupportedVersion},
		{"v2.0 too new", "2.0", engerrors.KindUnsupportedVersion},
		{"empty version", "", engerrors.KindUnsupportedVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"version":"` + tc.version + `","devices":[],"connections":[],"external_ports":[]}`)
			_, err := Decode(raw)
			got := engerrors.KindOf(err)
			if got != tc.want {
				t.Fatalf("input version=%q\nerr=%v\nwant kind=%q got=%q", tc.version, err, tc.want, got)
			}
		})
	}
}

func TestDecode_UnknownFieldRejected(t *testing.T) {
	raw := []byte(`{"version":"1.0","devices":[],"connections":[],"external_ports":[],"bogus":1}`)
	_, err := Decode(raw)
	if kind := engerrors.KindOf(err); kind != engerrors.KindModelDecode {
		t.Fatalf("input had unknown field\nerr=%v\nwant model decode error", err)
	}
	if err != nil && !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error should name unknown field, got %v", err)
	}
}

func TestDecode_UnknownDeviceFieldRejected(t *testing.T) {
	raw := []byte(`{"version":"1.0","devices":[{"id":"x","type":"waveguide","params":{},"zap":true}],"connections":[],"external_ports":[]}`)
	_, err := Decode(raw)
	if kind := engerrors.KindOf(err); kind != engerrors.KindModelDecode {
		t.Fatalf("unknown device field must be rejected, got %v", err)
	}
}

func TestDefaults_StableAcrossVersions(t *testing.T) {
	for _, v := range []string{Version1_0, Version1_1} {
		raw := []byte(`{"version":"` + v + `","devices":[
			{"id":"bs","type":"beam_splitter","params":{}},
			{"id":"ps","type":"phase_shifter","params":{}}
		],"connections":[],"external_ports":[]}`)
		m, err := Decode(raw)
		if err != nil {
			t.Fatalf("version %s: %v", v, err)
		}
		if err := m.Defaults(); err != nil {
			t.Fatalf("version %s defaults: %v", v, err)
		}
		if m.Devices[0].Params.Theta != math.Pi/4 {
			t.Fatalf("version %s default theta changed: %.12f", v, m.Devices[0].Params.Theta)
		}
		if m.Devices[1].Params.Gain == nil || *m.Devices[1].Params.Gain != 1 {
			g := -1.0
			if m.Devices[1].Params.Gain != nil {
				g = *m.Devices[1].Params.Gain
			}
			t.Fatalf("version %s default gain changed: %v", v, g)
		}
	}
}

func TestValidate_Ranges(t *testing.T) {
	cases := []struct {
		name string
		dev  Device
		want engerrors.Kind
	}{
		{"splitter theta negative", Device{ID: "d", Type: DeviceBeamSplitter, Modes: 1, Params: DeviceParams{Theta: -0.1}}, engerrors.KindOutOfPhysicalRange},
		{"splitter theta too big", Device{ID: "d", Type: DeviceBeamSplitter, Modes: 1, Params: DeviceParams{Theta: 2}}, engerrors.KindOutOfPhysicalRange},
		{"ring r at 1", Device{ID: "d", Type: DeviceRingCavity, Modes: 1, Params: DeviceParams{R: 1, A: 0.9, PhiRound: 0}}, engerrors.KindOutOfPhysicalRange},
		{"ring a zero", Device{ID: "d", Type: DeviceRingCavity, Modes: 1, Params: DeviceParams{R: 0.9, A: 0, PhiRound: 0}}, engerrors.KindOutOfPhysicalRange},
		{"non-finite phase", Device{ID: "d", Type: DeviceWaveguide, Modes: 1, Params: DeviceParams{Phase: math.NaN()}}, engerrors.KindNonFiniteInput},
		{"zero gain", Device{ID: "d", Type: DevicePhaseShifter, Modes: 1, Params: DeviceParams{Gain: ptr(0.0)}}, engerrors.KindOutOfPhysicalRange},
		{"negative gain", Device{ID: "d", Type: DevicePhaseShifter, Modes: 1, Params: DeviceParams{Gain: ptr(-2.0)}}, engerrors.KindOutOfPhysicalRange},
		{"unknown type", Device{ID: "d", Type: "mystery", Modes: 1}, engerrors.KindUnknownDeviceType},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &CircuitModel{Version: Version1_0, Devices: []Device{tc.dev}}
			err := m.Defaults()
			if got := engerrors.KindOf(err); got != tc.want {
				t.Fatalf("input device=%+v\nerr=%v\nwant=%q got=%q", tc.dev, err, tc.want, got)
			}
		})
	}

	t.Run("duplicate id", func(t *testing.T) {
		d := Device{ID: "d", Type: DeviceWaveguide, Modes: 1}
		m := &CircuitModel{Version: Version1_0, Devices: []Device{d, d}}
		err := m.Defaults()
		if got := engerrors.KindOf(err); got != engerrors.KindInvalidParameter {
			t.Fatalf("duplicate id must be invalid_parameter, got %v", err)
		}
	})

	t.Run("empty id", func(t *testing.T) {
		m := &CircuitModel{Version: Version1_0, Devices: []Device{{Type: DeviceWaveguide, Modes: 1}}}
		if got := engerrors.KindOf(m.Defaults()); got != engerrors.KindInvalidParameter {
			t.Fatalf("empty id must be rejected, got kind %q", got)
		}
	})
}

func ptr(v float64) *float64 { return &v }
