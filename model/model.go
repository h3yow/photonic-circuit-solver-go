// Package model defines the local photonic circuit description: device types,
// parameters, topology references and versioned on-disk models.
//
// Optical/phase conventions (also documented in docs/):
//   - Time dependence is exp(-i*omega*t); forward propagation accumulates
//     phase exp(i*beta*L).
//   - A phase shifter applies S = exp(i*phi) on its through path, where
//     phi = bias + offset + 2*pi*gain*control (rad, wrapped into [0,2*pi)).
//   - A symmetric beam splitter is i*[[t, r],[r, t]] (reflection port picks
//     up a pi/2 phase); r=sin(theta), t=cos(theta).
//   - All devices are single-mode: every port carries exactly one complex
//     amplitude; multi-mode fields are rejected (port_dimension_mismatch).
package model

// Version is the supported model format version family.
const (
	Version1_0 = "1.0"
	Version1_1 = "1.1"
	MinVersion = Version1_0
	MaxVersion = Version1_1
)

// DeviceType enumerates the local primitive devices.
type DeviceType string

const (
	DeviceWaveguide    DeviceType = "waveguide"     // 2 端口波导
	DeviceBeamSplitter DeviceType = "beam_splitter" // 2x2 分束器
	DevicePhaseShifter DeviceType = "phase_shifter" // 2 端口移相器
	DeviceRingCavity   DeviceType = "ring_cavity"   // 全通环形谐振腔（2 端口总线）
)

// Port name constants.
const (
	PortIn   = "in"
	PortOut  = "out"
	PortIn1  = "in1"
	PortIn2  = "in2"
	PortOut1 = "out1"
	PortOut2 = "out2"
)

// DeviceParams holds the parameters of a single device.
// Only the fields relevant to Type must be provided (see validation).
type DeviceParams struct {
	// Waveguide: optical length phase beta*L in radians.
	Phase float64 `json:"phase,omitempty"`
	// Beam splitter: coupling angle theta; r=sin(theta), t=cos(theta).
	Theta float64 `json:"theta,omitempty"`
	// Phase shifter: static bias phase (rad).
	Bias float64 `json:"bias,omitempty"`
	// Phase shifter calibration (filled after calibration; rad).
	Offset float64 `json:"offset,omitempty"`
	// Phase shifter control gain (rad per control unit). Pointer so that
	// an explicit zero is distinguishable from an absent field (default 1).
	Gain *float64 `json:"gain,omitempty"`
	// Ring: single-pass phase phiRound (rad).
	PhiRound float64 `json:"phi_round,omitempty"`
	// Ring: self-coupling amplitude r (|r|<=1).
	R float64 `json:"r,omitempty"`
	// Ring: round-trip loss amplitude a (0 < a <= 1).
	A float64 `json:"a,omitempty"`
}

// Device is one node in the circuit.
type Device struct {
	ID     string       `json:"id"`
	Type   DeviceType   `json:"type"`
	Params DeviceParams `json:"params"`
	// Modes is the port dimensionality. Absent in JSON means 1 (single mode).
	// Built-in primitives support only single-mode ports, so values other
	// than 1 are rejected at compile time (port_dimension_mismatch).
	Modes int `json:"modes,omitempty"`
}

// PortRef references one device port.
type PortRef struct {
	Device string `json:"device"`
	Port   string `json:"port"`
}

// Connection is an undirected/full-duplex optical link between two ports.
type Connection struct {
	A PortRef `json:"a"`
	B PortRef `json:"b"`
}

// CircuitModel is the in-memory representation of a versioned model file.
type CircuitModel struct {
	Version     string       `json:"version"`
	Devices     []Device     `json:"devices"`
	Connections []Connection `json:"connections"`
	// ExternalPorts declares unconnected device ports that are reachable from
	// outside the circuit. Any unconnected port not listed here is dangling.
	ExternalPorts []PortRef `json:"external_ports"`
}

// PortCount returns the number of ports for a known device type, or 0.
func PortCount(t DeviceType) int { return portCount(t) }

// HasPort reports whether the device type owns a port with the given name.
func HasPort(t DeviceType, port string) bool { return hasPort(t, port) }

// IsKnownDeviceType reports whether t is a supported primitive.
func IsKnownDeviceType(t DeviceType) bool { return isKnownDeviceType(t) }

// PortList returns the ordered port names of a device type.
func PortList(t DeviceType) []string { return append([]string(nil), ports[t]...) }
