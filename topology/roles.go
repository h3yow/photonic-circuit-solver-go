package topology

import "github.com/h3yow/photonic-circuit-solver-go/model"

// IsInputPort reports whether a primitive port is an input-role port
// (accepts waves); the complement are output-role ports (emits waves).
func IsInputPort(t model.DeviceType, name string) bool { return isInputPort(t, name) }

// IsOutputPort reports the output-role side.
func IsOutputPort(t model.DeviceType, name string) bool {
	return model.HasPort(t, name) && !isInputPort(t, name)
}
