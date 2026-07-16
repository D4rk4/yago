package yagonode

import (
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/D4rk4/yago/yagonode/internal/metrichistory"
)

const maximumProcMemoryInformationBytes = 1 << 20

type memoryAvailabilityObservation struct {
	bytes   uint64
	present bool
	valid   bool
}

func currentHostMemory() (metrichistory.HostMemory, bool) {
	return sampleHostMemory(
		func() (io.ReadCloser, error) { return os.Open("/proc/meminfo") },
		syscall.Sysinfo,
	)
}

func sampleHostMemory(
	openMemoryInformation func() (io.ReadCloser, error),
	readSystemInformation func(*syscall.Sysinfo_t) error,
) (metrichistory.HostMemory, bool) {
	systemMemory, totalAvailable := readNodeMemory(readSystemInformation)
	if !totalAvailable {
		return metrichistory.HostMemory{}, false
	}
	memory := metrichistory.HostMemory{TotalBytes: systemMemory.totalBytes}
	availability := readMemoryAvailability(openMemoryInformation)
	if availability.present {
		if availability.valid && availability.bytes <= memory.TotalBytes {
			memory.AvailableBytes = availability.bytes
			memory.AvailableObserved = true
		}

		return memory, true
	}
	if systemMemory.freeAvailable && systemMemory.freeBytes <= memory.TotalBytes {
		memory.AvailableBytes = systemMemory.freeBytes
		memory.AvailableObserved = true
	}

	return memory, true
}

func readMemoryAvailability(
	openMemoryInformation func() (io.ReadCloser, error),
) memoryAvailabilityObservation {
	if openMemoryInformation == nil {
		return memoryAvailabilityObservation{}
	}
	reader, err := openMemoryInformation()
	if err != nil {
		return memoryAvailabilityObservation{}
	}
	defer func() { _ = reader.Close() }()

	return parseMemoryAvailability(reader)
}

func parseMemoryAvailability(reader io.Reader) memoryAvailabilityObservation {
	if reader == nil {
		return memoryAvailabilityObservation{}
	}
	content, err := io.ReadAll(io.LimitReader(reader, maximumProcMemoryInformationBytes+1))
	if err != nil || len(content) > maximumProcMemoryInformationBytes {
		return memoryAvailabilityObservation{present: true}
	}

	observation := memoryAvailabilityObservation{}
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "MemAvailable") {
			continue
		}
		if observation.present {
			observation.valid = false

			return observation
		}
		observation.present = true
		fields := strings.Fields(trimmed)
		if len(fields) != 3 || fields[0] != "MemAvailable:" || fields[2] != "kB" {
			continue
		}
		kilobytes, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil || kilobytes > maximumSystemObservationBytes/1024 {
			continue
		}
		observation.bytes = kilobytes * 1024
		observation.valid = true
	}

	return observation
}
