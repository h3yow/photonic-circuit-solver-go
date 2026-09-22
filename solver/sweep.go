package solver

import (
	engerrors "github.com/h3yow/photonic-circuit-solver-go/errors"
	"github.com/h3yow/photonic-circuit-solver-go/topology"
)

// solveSweep evaluates each device exactly once in topological order.
// Because compile() guarantees a feed-forward DAG, a single pass is exact;
// it is an implementation independent of the global linear system.
func (s *Solver) solveSweep(req Request) (*Result, error) {
	src, totalIn, err := s.validateAndSource(req)
	if err != nil {
		return nil, err
	}
	order, err := s.deviceOrder()
	if err != nil {
		return nil, err
	}
	n := len(s.compiled.Ports)
	// emit[p] is the wave leaving device at p.
	emit := make([]complex128, n)
	for _, di := range order {
		blk := s.blocks[di]
		d := &s.compiled.Model.Devices[di]
		in := make([]complex128, blk.size)
		for c := 0; c < blk.size; c++ {
			gpIdx := blk.base + c
			gp := s.compiled.Ports[gpIdx]
			if topology.IsInputPort(d.Type, gp.PortName) {
				if s.isExternal(gpIdx) {
					in[c] = src[gpIdx]
				} else if q := s.compiled.Link[gpIdx]; q >= 0 {
					in[c] = emit[q]
				}
			}
		}
		for r := 0; r < blk.size; r++ {
			if topology.IsInputPort(d.Type, s.compiled.Ports[blk.base+r].PortName) {
				continue
			}
			var sum complex128
			for c := 0; c < blk.size; c++ {
				sum += blk.s[r*blk.size+c] * in[c]
			}
			emit[blk.base+r] = sum
		}
	}
	// Reconstruct entering-wave vector x to match the linear convention.
	x := make([]complex128, n)
	for p := range x {
		gp := s.compiled.Ports[p]
		dt := s.deviceTypeAt(gp.DeviceID)
		if topology.IsInputPort(dt, gp.PortName) {
			if s.isExternal(p) {
				x[p] = src[p]
			} else if q := s.compiled.Link[p]; q >= 0 {
				x[p] = emit[q]
			}
		}
	}
	return s.extract(req, x, src, totalIn)
}

// deviceOrder returns device indices in Kahn topological order.
func (s *Solver) deviceOrder() ([]int, error) {
	nd := len(s.compiled.Model.Devices)
	indeg := make([]int, nd)
	adj := make([][]int, nd)
	for ci := range s.compiled.Model.Connections {
		conn := &s.compiled.Model.Connections[ci]
		ia, _ := s.compiled.PortIndex(globalKey(conn.A.Device, conn.A.Port))
		ib, _ := s.compiled.PortIndex(globalKey(conn.B.Device, conn.B.Port))
		ta := s.deviceTypeAt(conn.A.Device)
		up, down := ia, ib
		if topology.IsInputPort(ta, conn.A.Port) {
			up, down = ib, ia
		}
		uid, did := s.compiled.Ports[up].DeviceID, s.compiled.Ports[down].DeviceID
		u := s.deviceIndex(uid)
		v := s.deviceIndex(did)
		adj[u] = append(adj[u], v)
		indeg[v]++
	}
	var queue []int
	for i := 0; i < nd; i++ {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	var order []int
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		order = append(order, u)
		for _, v := range adj[u] {
			indeg[v]--
			if indeg[v] == 0 {
				queue = append(queue, v)
			}
		}
	}
	if len(order) != nd {
		return nil, engerrors.New(engerrors.KindTopologyCycle, "topological order failed: cycle remains")
	}
	return order, nil
}

func (s *Solver) deviceIndex(id string) int {
	for i := range s.compiled.Model.Devices {
		if s.compiled.Model.Devices[i].ID == id {
			return i
		}
	}
	return -1
}
