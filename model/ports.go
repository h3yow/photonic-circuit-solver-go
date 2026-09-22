package model

// ports maps each device type to its fixed single-mode port set.
var ports = map[DeviceType][]string{
	DeviceWaveguide:    {PortIn, PortOut},
	DevicePhaseShifter: {PortIn, PortOut},
	DeviceRingCavity:   {PortIn, PortOut},
	DeviceBeamSplitter: {PortIn1, PortIn2, PortOut1, PortOut2},
}

func portCount(t DeviceType) int {
	if p, ok := ports[t]; ok {
		return len(p)
	}
	return 0
}

func hasPort(t DeviceType, port string) bool {
	for _, p := range ports[t] {
		if p == port {
			return true
		}
	}
	return false
}

func isKnownDeviceType(t DeviceType) bool {
	_, ok := ports[t]
	return ok
}
