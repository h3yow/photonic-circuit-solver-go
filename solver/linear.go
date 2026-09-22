package solver

import (
	"math"

	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
	"github.com/h3yow/photonic-circuit-solver-go/model"
)

// validateAndSource checks request ports/values and builds the source vector
// of waves entering external input ports.
func (s *Solver) validateAndSource(req Request) ([]complex128, float64, error) {
	n := len(s.compiled.Ports)
	src := make([]complex128, n)
	totalIn := 0.0
	excited := map[int]bool{}
	for e := range req.Excitations {
		ex := &req.Excitations[e]
		if !finiteComplex(ex.Amplitude) {
			return nil, 0, engerrors.New(engerrors.KindNonFiniteInput,
				"excitation at %s.%s has non-finite amplitude %v", ex.DeviceID, ex.Port, ex.Amplitude)
		}
		pow := math.Pow(real(ex.Amplitude), 2) + math.Pow(imag(ex.Amplitude), 2)
		if pow > 1.0+1e-12 {
			return nil, 0, engerrors.New(engerrors.KindOutOfPhysicalRange,
				"excitation at %s.%s has power %.6g > 1 (normalized max total input power)",
				ex.DeviceID, ex.Port, pow)
		}
		idx, ok := s.compiled.PortIndex(globalKey(ex.DeviceID, ex.Port))
		if !ok {
			return nil, 0, engerrors.New(engerrors.KindMissingPort,
				"excitation references missing port %s.%s", ex.DeviceID, ex.Port)
		}
		gp := s.compiled.Ports[idx]
		if !s.isExternal(idx) {
			return nil, 0, engerrors.New(engerrors.KindInvalidParameter,
				"excitation port %s.%s is not an external circuit port", gp.DeviceID, gp.PortName)
		}
		dt := s.deviceTypeAt(gp.DeviceID)
		if !topologyIsInput(dt, gp.PortName) {
			return nil, 0, engerrors.New(engerrors.KindInvalidParameter,
				"excitation must drive an input port; %s.%s is an output port", gp.DeviceID, gp.PortName)
		}
		if excited[idx] {
			return nil, 0, engerrors.New(engerrors.KindInvalidParameter,
				"duplicate excitation at %s.%s", gp.DeviceID, gp.PortName)
		}
		excited[idx] = true
		src[idx] = ex.Amplitude
		totalIn += pow
	}
	if totalIn > 1.0+1e-12 {
		return nil, 0, engerrors.New(engerrors.KindOutOfPhysicalRange,
			"total excitation power %.6g exceeds normalized bound 1", totalIn)
	}
	for p := range req.Probes {
		pr := &req.Probes[p]
		idx, ok := s.compiled.PortIndex(globalKey(pr.DeviceID, pr.Port))
		if !ok {
			return nil, 0, engerrors.New(engerrors.KindMissingPort,
				"probe references missing port %s.%s", pr.DeviceID, pr.Port)
		}
		gp := s.compiled.Ports[idx]
		if !s.isExternal(idx) {
			return nil, 0, engerrors.New(engerrors.KindInvalidParameter,
				"probe port %s.%s is not an external circuit port", gp.DeviceID, gp.PortName)
		}
		// Probes normally read output ports. An external input port is also
		// readable (it returns its excitation amplitude) so it can serve as a
		// stable phase reference during calibration.
	}
	return src, totalIn, nil
}

func (s *Solver) isExternal(idx int) bool {
	for _, e := range s.compiled.External {
		if e == idx {
			return true
		}
	}
	return false
}

func (s *Solver) deviceTypeAt(deviceID string) model.DeviceType {
	for i := range s.compiled.Model.Devices {
		if s.compiled.Model.Devices[i].ID == deviceID {
			return s.compiled.Model.Devices[i].Type
		}
	}
	return ""
}

func (s *Solver) solveLinear(req Request) (*Result, error) {
	src, totalIn, err := s.validateAndSource(req)
	if err != nil {
		return nil, err
	}
	n := len(src)
	a := make([][]complex128, n)
	for i := range a {
		a[i] = make([]complex128, n)
		a[i][i] = 1
	}
	rhs := make([]complex128, n)
	// Per-port equations on entering waves x:
	//   external input port p : x[p] = src[p]
	//   external output port p: x[p] = 0 (nothing injected)
	//   linked port p (peer q): x[p] = sum_c S[q,c] * enter[c]
	// where enter[c] = src[c] at external inputs, else x[c].
	for p := 0; p < n; p++ {
		gp := s.compiled.Ports[p]
		dt := s.deviceTypeAt(gp.DeviceID)
		if topologyIsInput(dt, gp.PortName) && s.isExternal(p) {
			rhs[p] = src[p]
			continue
		}
		if s.isExternal(p) {
			continue // external output: x[p] = 0
		}
		q := s.compiled.Link[p]
		if q < 0 {
			continue
		}
		qblk := s.blockOf(q)
		qr := q - qblk.base
		for c := 0; c < qblk.size; c++ {
			gj := qblk.base + c
			enter := s.enterIndex(gj)
			if enter < 0 {
				rhs[p] += qblk.s[qr*qblk.size+c] * src[gj]
			} else {
				a[p][enter] -= qblk.s[qr*qblk.size+c]
			}
		}
	}
	x, err := solveComplexLinear(a, rhs)
	if err != nil {
		return nil, err
	}
	res, err := s.extract(req, x, src, totalIn)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// extract reads probe outgoing waves and enforces finiteness/passivity.
func (s *Solver) extract(req Request, x, src []complex128, totalIn float64) (*Result, error) {
	out := make([]complex128, 0, len(req.Probes))
	// Outgoing wave at port p = sum_r S[p,r] * x[r] (source adds nothing for
	// external inputs; their entering wave is fixed externally).
	emit := s.outgoing(x, src)
	for p := range req.Probes {
		pr := &req.Probes[p]
		idx, _ := s.compiled.PortIndex(globalKey(pr.DeviceID, pr.Port))
		gp := s.compiled.Ports[idx]
		dt := s.deviceTypeAt(gp.DeviceID)
		// Reading an external input port returns the injected wave itself
		// (a stable phase reference); output ports return the device emission.
		v := emit[idx]
		if topologyIsInput(dt, gp.PortName) && s.isExternal(idx) {
			v = src[idx]
		}
		if !finiteComplex(v) {
			return nil, engerrors.New(engerrors.KindNonFiniteInput,
				"solver produced non-finite output at %s.%s", pr.DeviceID, pr.Port)
		}
		out = append(out, v)
	}
	totalOut := 0.0
	for i := range s.compiled.External {
		idx := s.compiled.External[i]
		gp := s.compiled.Ports[idx]
		dt := s.deviceTypeAt(gp.DeviceID)
		if topologyIsOutput(dt, gp.PortName) {
			v := emit[idx]
			totalOut += real(v)*real(v) + imag(v)*imag(v)
		}
	}
	// Passive network: output power <= input power up to numerical slack.
	if totalOut > totalIn+1e-8 {
		return nil, engerrors.New(engerrors.KindSolverDivergence,
			"nonphysical result: total output power %.9g exceeds input power %.9g",
			totalOut, totalIn)
	}
	return &Result{Outputs: out, Field: x}, nil
}

// outgoing computes emitted waves b = S*x, adding external sources on input
// external ports (their entering wave is the excitation, never x[p]).
func (s *Solver) outgoing(x, src []complex128) []complex128 {
	n := len(s.compiled.Ports)
	enter := make([]complex128, n)
	if x != nil {
		copy(enter, x)
	}
	for i := range s.compiled.External {
		idx := s.compiled.External[i]
		gp := s.compiled.Ports[idx]
		dt := s.deviceTypeAt(gp.DeviceID)
		if topologyIsInput(dt, gp.PortName) {
			enter[idx] = src[idx]
		}
	}
	b := make([]complex128, n)
	for _, blk := range s.blocks {
		for r := 0; r < blk.size; r++ {
			var sum complex128
			for c := 0; c < blk.size; c++ {
				sum += blk.s[r*blk.size+c] * enter[blk.base+c]
			}
			b[blk.base+r] = sum
		}
	}
	return b
}
