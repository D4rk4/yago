package yagonode

import (
	"syscall"
)

const maximumSystemObservationBytes = uint64(1 << 62)

type nodeMemoryObservation struct {
	totalBytes    uint64
	freeBytes     uint64
	freeAvailable bool
}

func readNodeMemory(
	read func(*syscall.Sysinfo_t) error,
) (nodeMemoryObservation, bool) {
	if read == nil {
		return nodeMemoryObservation{}, false
	}
	var information syscall.Sysinfo_t
	if read(&information) != nil {
		return nodeMemoryObservation{}, false
	}

	return nodeMemoryFromValues(
		information.Totalram,
		information.Freeram,
		information.Unit,
	)
}

func nodeMemoryFromValues(
	total uint64,
	free uint64,
	unit uint32,
) (nodeMemoryObservation, bool) {
	factor := uint64(unit)
	if factor == 0 {
		factor = 1
	}
	if total == 0 || total > maximumSystemObservationBytes/factor {
		return nodeMemoryObservation{}, false
	}
	observation := nodeMemoryObservation{totalBytes: total * factor}
	if free <= maximumSystemObservationBytes/factor {
		observation.freeBytes = free * factor
		observation.freeAvailable = true
	}

	return observation, true
}
