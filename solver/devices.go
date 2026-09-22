package solver

import (
	"math"
	"math/cmplx"

	"github.com/h3yow/photonic-circuit-solver-go/model"
)

// fillDeviceS writes the single-device scattering matrix S such that
// b_out = S * a_in, where a_in[k] is the wave entering at local port k.
//
// Port order follows model.PortList(type):
//
//	2-port: [in, out]; matrix describes both propagation directions.
//	beam splitter: [in1, in2, out1, out2].
//
// Conventions (package docs): forward phase exp(i*phi); symmetric splitter
// [[t,i*r],[i*r,t]]; all-pass ring (r - a*exp(-i*phi))/(1 - a*r*exp(-i*phi)).
func fillDeviceS(blk *deviceBlock, d *model.Device) {
	n := blk.size
	set := func(r, c int, v complex128) { blk.s[r*n+c] = v }
	switch d.Type {
	case model.DeviceWaveguide:
		f := cmplx.Exp(complex(0, d.Params.Phase))
		// in  -> out with phase; out -> in with phase (reciprocal).
		set(1, 0, f)
		set(0, 1, f)

	case model.DevicePhaseShifter:
		phi := d.Params.Bias + d.Params.Offset
		f := cmplx.Exp(complex(0, phi))
		set(1, 0, f)
		set(0, 1, f)

	case model.DeviceRingCavity:
		// All-pass transfer; the same amplitude links bus-in to bus-out in
		// either direction (reciprocal).
		p := d.Params
		denom := 1 - complex(p.A*p.R, 0)*cmplx.Exp(complex(0, -p.PhiRound))
		t := (complex(p.R, 0) - complex(p.A, 0)*cmplx.Exp(complex(0, -p.PhiRound))) / denom
		set(1, 0, t)
		set(0, 1, t)

	case model.DeviceBeamSplitter:
		// Local order: 0=in1,1=in2,2=out1,3=out2.
		// Symmetric reciprocal splitter (local 0=in1,1=in2,2=out1,3=out2):
		//   out1 = i*r*in1 + t*in2
		//   out2 = t*in1 + i*r*in2
		// t=cos(theta) is the through amplitude, r=sin(theta) the cross
		// amplitude; the pi/2 phase lives on the CROSS arm.
		ct := math.Cos(d.Params.Theta)
		st := math.Sin(d.Params.Theta)
		t := complex(ct, 0)
		ir := complex(0, st)
		// forward block (rows: out1,out2)
		set(2, 0, ir)
		set(2, 1, t)
		set(3, 0, t)
		set(3, 1, ir)
		// reverse block: S symmetric (reciprocal): columns mirrored.
		set(0, 2, ir)
		set(0, 3, t)
		set(1, 2, t)
		set(1, 3, ir)
	}
}
