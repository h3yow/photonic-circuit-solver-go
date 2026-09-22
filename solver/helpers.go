package solver

import (
	"github.com/h3yow/photonic-circuit-solver-go/model"
	"github.com/h3yow/photonic-circuit-solver-go/topology"
)

func globalKey(deviceID, port string) topology.DeviceKey {
	return topology.DeviceKey{DeviceID: deviceID, PortName: port}
}

func topologyIsInput(t model.DeviceType, name string) bool {
	return topology.IsInputPort(t, name)
}

func topologyIsOutput(t model.DeviceType, name string) bool {
	return topology.IsOutputPort(t, name)
}

// blockOf returns the device block containing global port idx.
func (s *Solver) blockOf(idx int) *deviceBlock {
	for i := range s.blocks {
		b := &s.blocks[i]
		if idx >= b.base && idx < b.base+b.size {
			return b
		}
	}
	return nil
}

// enterIndex returns the global index whose x value is the entering wave at
// idx, or -1 when idx is an external input (entering wave is src instead).
func (s *Solver) enterIndex(idx int) int {
	gp := s.compiled.Ports[idx]
	dt := s.deviceTypeAt(gp.DeviceID)
	if topology.IsInputPort(dt, gp.PortName) && s.isExternal(idx) {
		return -1
	}
	return idx
}

// Compiled returns the immutable compiled circuit this solver uses.
func (s *Solver) Compiled() *topology.Compiled { return s.compiled }

// NumericConfig returns the solver numeric config (a copy).
func (s *Solver) NumericConfig() Config { return s.cfg }
