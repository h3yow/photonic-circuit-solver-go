package topology

import (
	"github.com/h3yow/photonic-circuit-solver-go/errors"
	"github.com/h3yow/photonic-circuit-solver-go/model"
)

func portIndex(c *Compiled, k DeviceKey) (int, bool) {
	idx, ok := c.index[k]
	return idx, ok
}

// isInputPort names the signal role of a primitive port. Input ports accept
// waves from links; output ports emit waves. A legal feed-forward link joins
// an output port to an input port.
func isInputPort(t model.DeviceType, name string) bool {
	switch t {
	case model.DeviceWaveguide, model.DevicePhaseShifter, model.DeviceRingCavity:
		return name == model.PortIn
	case model.DeviceBeamSplitter:
		return name == model.PortIn1 || name == model.PortIn2
	}
	return false
}

func compile(m *model.CircuitModel) (*Compiled, error) {
	// 1) Parameter defaults + validation (duplicate ids, ranges, finiteness).
	if err := m.Defaults(); err != nil {
		return nil, err
	}

	devIndex := map[string]int{}
	for i := range m.Devices {
		devIndex[m.Devices[i].ID] = i
	}

	// 2) Build the deterministic global port table.
	c := &Compiled{Model: m, index: map[GlobalPort]int{}}
	for i := range m.Devices {
		d := &m.Devices[i]
		// Only single-mode primitives exist; any explicit multimode
		// declaration mismatches their port dimension.
		if d.Modes != 1 {
			return nil, errors.New(errors.KindPortDimension,
				"device %q declares modes=%d but built-in primitives support single-mode ports only",
				d.ID, d.Modes)
		}
		for _, pn := range model.PortList(d.Type) {
			gp := GlobalPort{DeviceID: d.ID, PortName: pn}
			c.index[gp] = len(c.Ports)
			c.Ports = append(c.Ports, gp)
		}
	}
	c.Link = make([]int, len(c.Ports))
	for i := range c.Link {
		c.Link[i] = -1
	}

	lookup := func(ref model.PortRef, ctx string) (int, error) {
		di, ok := devIndex[ref.Device]
		if !ok {
			return 0, errors.New(errors.KindMissingPort,
				"%s references unknown device %q", ctx, ref.Device)
		}
		if !model.HasPort(m.Devices[di].Type, ref.Port) {
			return 0, errors.New(errors.KindMissingPort,
				"%s references missing port %q on device %q (type %s)",
				ctx, ref.Port, ref.Device, m.Devices[di].Type)
		}
		return c.index[GlobalPort{DeviceID: ref.Device, PortName: ref.Port}], nil
	}

	// 3) Validate external port declarations.
	externalDecl := map[int]bool{}
	for _, ref := range m.ExternalPorts {
		idx, err := lookup(ref, "external port declaration")
		if err != nil {
			return nil, err
		}
		if externalDecl[idx] {
			return nil, errors.New(errors.KindInvalidParameter,
				"duplicate external port declaration for %s.%s", ref.Device, ref.Port)
		}
		externalDecl[idx] = true
	}

	// 4) Validate connections and wire links.
	adj := make([][]int, len(m.Devices))
	for ci := range m.Connections {
		conn := &m.Connections[ci]
		ia, err := lookup(conn.A, "connection endpoint a")
		if err != nil {
			return nil, err
		}
		ib, err := lookup(conn.B, "connection endpoint b")
		if err != nil {
			return nil, err
		}
		if ia == ib {
			return nil, errors.New(errors.KindTopologyCycle,
				"connection loops back %s.%s to itself", conn.A.Device, conn.A.Port)
		}
		if c.Link[ia] != -1 {
			return nil, errors.New(errors.KindPortInUse,
				"port %s.%s is already connected", conn.A.Device, conn.A.Port)
		}
		if c.Link[ib] != -1 {
			return nil, errors.New(errors.KindPortInUse,
				"port %s.%s is already connected", conn.B.Device, conn.B.Port)
		}
		if externalDecl[ia] || externalDecl[ib] {
			ref := conn.A
			if externalDecl[ib] {
				ref = conn.B
			}
			return nil, errors.New(errors.KindPortInUse,
				"port %s.%s is declared external but also connected", ref.Device, ref.Port)
		}
		pa, pb := c.Ports[ia], c.Ports[ib]
		da := m.Devices[devIndex[pa.DeviceID]]
		db := m.Devices[devIndex[pb.DeviceID]]
		aIn, bIn := isInputPort(da.Type, pa.PortName), isInputPort(db.Type, pb.PortName)
		if aIn == bIn {
			role := "two output ports"
			if aIn {
				role = "two input ports"
			}
			return nil, errors.New(errors.KindInvalidParameter,
				"connection joins %s: %s.%s <-> %s.%s; links must route an output port to an input port",
				role, pa.DeviceID, pa.PortName, pb.DeviceID, pb.PortName)
		}
		c.Link[ia] = ib
		c.Link[ib] = ia
		up, down := devIndex[pa.DeviceID], devIndex[pb.DeviceID]
		if bIn {
			up, down = devIndex[pa.DeviceID], devIndex[pb.DeviceID]
		} else {
			up, down = devIndex[pb.DeviceID], devIndex[pa.DeviceID]
		}
		if up == down {
			return nil, errors.New(errors.KindTopologyCycle,
				"connection forms a self-loop on device %q", pa.DeviceID)
		}
		adj[up] = append(adj[up], down)
	}

	// 5) Every unlinked port must be a declared external port, otherwise it
	//    is a dangling connection.
	for i, gp := range c.Ports {
		if c.Link[i] == -1 && !externalDecl[i] {
			return nil, errors.New(errors.KindDanglingConnection,
				"port %s.%s is neither connected nor declared as an external port",
				gp.DeviceID, gp.PortName)
		}
		if c.Link[i] != -1 && externalDecl[i] {
			return nil, errors.New(errors.KindPortInUse,
				"port %s.%s is both connected and declared external", gp.DeviceID, gp.PortName)
		}
	}
	for i := range c.Ports {
		if externalDecl[i] {
			c.External = append(c.External, i)
		}
	}

	// 6) Reject directed cycles (signal would chase itself forever).
	if cycle := findCycle(m.Devices, adj); cycle != nil {
		return nil, errors.New(errors.KindTopologyCycle,
			"directed cycle detected through devices %v", cycle)
	}
	return c, nil
}

// findCycle returns a device-id path participating in a directed cycle, or nil.
func findCycle(devs []model.Device, adj [][]int) []string {
	const white, gray, black = 0, 1, 2
	color := make([]byte, len(devs))
	var stack []int
	var dfs func(u int) []string
	dfs = func(u int) []string {
		color[u] = gray
		stack = append(stack, u)
		for _, v := range adj[u] {
			switch color[v] {
			case white:
				if cyc := dfs(v); cyc != nil {
					return cyc
				}
			case gray:
				path := []string{}
				for _, x := range stack {
					if x == v {
						path = append(path, devs[x].ID)
						continue
					}
					if len(path) > 0 {
						path = append(path, devs[x].ID)
					}
				}
				path = append(path, devs[v].ID)
				return path
			}
		}
		stack = stack[:len(stack)-1]
		color[u] = black
		return nil
	}
	for u := range devs {
		if color[u] == white {
			if cyc := dfs(u); cyc != nil {
				return cyc
			}
		}
	}
	return nil
}
