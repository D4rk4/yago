package yagonode

import (
	"bytes"
	"os"
	runtimemetrics "runtime/metrics"
	"strconv"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

const (
	goHeapObjectsMetricName   = "/memory/classes/heap/objects:bytes"
	maximumProcessMemoryBytes = int64(1<<63 - 1)
	linuxProcessStatusPath    = "/proc/self/status"
)

type processMemoryDiagnostics interface {
	ProcessMemory() adminui.ProcessMemory
}

type currentProcessMemory struct {
	readStatus  func(string) ([]byte, error)
	heapObjects func() uint64
}

func newCurrentProcessMemory() currentProcessMemory {
	return currentProcessMemory{
		readStatus:  os.ReadFile,
		heapObjects: currentGoHeapObjectsBytes,
	}
}

func (memory currentProcessMemory) ProcessMemory() adminui.ProcessMemory {
	status, err := memory.readStatus(linuxProcessStatusPath)
	observation := adminui.ProcessMemory{}
	if err == nil {
		observation = processMemoryFromStatus(status)
	}
	heapObjects := memory.heapObjects()
	if heapObjects <= uint64(maximumProcessMemoryBytes) {
		observation.GoHeapObjectsBytes = int64(heapObjects)
		observation.GoHeapObjectsAvailable = true
	}

	return observation
}

func currentGoHeapObjectsBytes() uint64 {
	samples := []runtimemetrics.Sample{{Name: goHeapObjectsMetricName}}
	runtimemetrics.Read(samples)

	return samples[0].Value.Uint64()
}

func processMemoryFromStatus(status []byte) adminui.ProcessMemory {
	observation := adminui.ProcessMemory{}
	for _, line := range bytes.Split(status, []byte{'\n'}) {
		fields := bytes.Fields(line)
		if len(fields) != 3 || !bytes.Equal(fields[2], []byte("kB")) {
			continue
		}
		byteCount, available := processStatusByteCount(fields[1])
		if !available {
			continue
		}
		switch string(fields[0]) {
		case "VmRSS:":
			observation.ResidentBytes = byteCount
			observation.ResidentAvailable = true
		case "RssAnon:":
			observation.AnonymousBytes = byteCount
			observation.AnonymousAvailable = true
		case "RssFile:":
			observation.FileBackedBytes = byteCount
			observation.FileBackedAvailable = true
		case "RssShmem:":
			observation.SharedMemoryBytes = byteCount
			observation.SharedMemoryAvailable = true
		}
	}

	return observation
}

func processStatusByteCount(value []byte) (int64, bool) {
	kilobytes, err := strconv.ParseUint(string(value), 10, 64)
	if err != nil || kilobytes > uint64(maximumProcessMemoryBytes/1024) {
		return 0, false
	}

	return int64(kilobytes * 1024), true
}

func (source overviewSource) withProcessMemory(
	memory processMemoryDiagnostics,
) overviewSource {
	source.processMemory = memory

	return source
}
