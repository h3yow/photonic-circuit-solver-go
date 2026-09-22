// Package topology compiles a model.CircuitModel into an immutable port-level
// network representation and rejects illegal topologies with distinct errors:
// missing ports, port dimension mismatches, dangling connections, doubly
// connected ports and cycles are all reported separately.
package topology

import (
	"github.com/h3yow/photonic-circuit-solver-go/model"
)

// GlobalPort indexes one single-mode port across the whole circuit.
type GlobalPort struct {
	DeviceID string
	PortName string
}

// Compiled is an immutable compiled circuit. Compilation never mutates the
// source model; a Compiled circuit can be shared by concurrent solves.
type Compiled struct {
	Model *model.CircuitModel

	// Ports in deterministic (device declaration, port-table) order.
	Ports []GlobalPort
	index map[GlobalPort]int

	// Link[p] = q when ports p and q are connected by an external fiber,
	// or -1 when p is an external (unconnected) circuit port.
	Link []int

	// External ordered by first external-port appearance in the model.
	External []int
}

// PortIndex returns the global index of a port.
func (c *Compiled) PortIndex(d DeviceKey) (int, bool) { return portIndex(c, d) }

// DeviceKey is a (device, port) pair used in lookups.
type DeviceKey = GlobalPort

// Compile validates and compiles a model into an immutable network.
func Compile(m *model.CircuitModel) (*Compiled, error) {
	return compile(m)
}
