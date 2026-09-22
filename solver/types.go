// Package solver provides independent propagation solvers for compiled
// photonic circuits and cross-checks their agreement within an explicit
// tolerance.
//
// Field convention: a port value a_p is the complex amplitude of the wave
// *entering* the device at port p. Physical power is |a|^2; total passive
// output power cannot exceed total input power.
package solver

import (
	"math"

	"github.com/h3yow/photonic-circuit-solver-go/model"
	"github.com/h3yow/photonic-circuit-solver-go/topology"
)

// Method selects an independent propagation algorithm.
type Method string

const (
	// MethodLinear assembles the global linear system (I - S*P)x = s and
	// solves it by Gaussian elimination with partial pivoting.
	MethodLinear Method = "linear"
	// MethodSweep evaluates devices once in topological order.
	MethodSweep Method = "topological_sweep"
	// MethodJacobi propagates amplitudes by fixed-point iteration until the
	// residual converges, capped at MaxIterations.
	MethodJacobi Method = "jacobi"
)

// Excitation fixes the wave amplitude entering one external input port.
type Excitation struct {
	DeviceID  string
	Port      string
	Amplitude complex128
}

// Probe names one external output port to read out.
type Probe struct {
	DeviceID string
	Port     string
}

// Request is one independent propagation problem.
type Request struct {
	Excitations []Excitation
	Probes      []Probe
}

// Result is the per-request probe outputs in probe order.
type Result struct {
	Outputs []complex128
	// Field holds the full entering-wave vector for calibration/inspection.
	Field []complex128
}

// Config sets numeric limits and agreement tolerances.
type Config struct {
	MaxIterations int
	// AbsTol/RelTol bound disagreement between independent methods and
	// iteration residual magnitude.
	AbsTol float64
	RelTol float64
}

// DefaultConfig returns the documented default tolerances.
func DefaultConfig() Config { return defaultConfig() }

// Solver runs propagation on one immutable compiled circuit.
type Solver struct {
	compiled *topology.Compiled
	cfg      Config

	// device S-matrices block-diagonal, ordered by global ports.
	blocks []deviceBlock
}

type deviceBlock struct {
	base int // first global port index
	size int
	s    []complex128 // size*size row-major
}

// New builds a Solver from a compiled circuit. It never mutates the circuit.
func New(c *topology.Compiled, cfg Config) (*Solver, error) { return newSolver(c, cfg) }

// Solve executes one request using the given method.
func (s *Solver) Solve(method Method, req Request) (*Result, error) {
	return s.solve(method, req)
}

// SolveCrossChecked solves with two independent methods and rejects the result
// when they disagree beyond tolerance (never silently picks one side).
func (s *Solver) SolveCrossChecked(primary, secondary Method, req Request) (*Result, error) {
	return s.solveCrossChecked(primary, secondary, req)
}

func finiteComplex(z complex128) bool {
	r, i := real(z), imag(z)
	return !math.IsNaN(r) && !math.IsNaN(i) && !math.IsInf(r, 0) && !math.IsInf(i, 0)
}

var _ = model.DeviceWaveguide
