package yagonode

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
)

type loginStatusSystemReads struct {
	architecture   func() string
	processorCount func() int
	processorModel func() (string, error)
	memory         func(*syscall.Sysinfo_t) error
	fileSystem     func(string, *syscall.Statfs_t) error
	now            func() time.Time
}

type loginNodeStatusSource struct {
	nodeName       string
	swarmAddress   string
	processorModel string
	dataDirectory  string
	version        string
	startedAt      time.Time
	systemReads    loginStatusSystemReads
}

func newLoginNodeStatusSource(config nodeConfig) loginNodeStatusSource {
	return newLoginNodeStatusSourceWithSystemReads(config, loginStatusSystemReads{
		architecture:   func() string { return runtime.GOARCH },
		processorCount: runtime.NumCPU,
		processorModel: linuxProcessorModel,
		memory:         syscall.Sysinfo,
		fileSystem:     syscall.Statfs,
		now:            time.Now,
	})
}

func newLoginNodeStatusSourceWithSystemReads(
	config nodeConfig,
	reads loginStatusSystemReads,
) loginNodeStatusSource {
	processorModel := ""
	if reads.processorModel != nil {
		processorModel, _ = reads.processorModel()
	}

	return loginNodeStatusSource{
		nodeName:       config.Name,
		swarmAddress:   advertisedLoginAddress(config.AdvertiseHost, config.AdvertisePort),
		processorModel: processorModel,
		dataDirectory:  config.DataDir,
		version:        Version(),
		startedAt:      reads.now(),
		systemReads:    reads,
	}
}

func (s loginNodeStatusSource) LoginNodeStatus(ctx context.Context) adminauth.LoginNodeStatus {
	status := adminauth.LoginNodeStatus{
		NodeName:     s.nodeName,
		SwarmAddress: s.swarmAddress,
		Version:      s.version,
		Uptime:       loginNodeUptime(s.startedAt, s.systemReads.now()),
	}
	if ctx.Err() != nil {
		return status
	}
	status.ProcessorModel = processorModelLabel(s.processorModel, s.systemReads.architecture())
	if processors := s.systemReads.processorCount(); processors > 0 {
		status.ProcessorCount = processorCountLabel(processors)
	}
	if memory, available := readNodeMemory(s.systemReads.memory); available {
		status.MemoryCapacity = loginStatusMemoryObservation(memory)
	}
	var fileSystem syscall.Statfs_t
	if s.dataDirectory != "" && s.systemReads.fileSystem(s.dataDirectory, &fileSystem) == nil {
		status.DataFreeSpace = loginStatusFileSystemFree(fileSystem.Bavail, fileSystem.Bsize)
	}

	return status
}

func advertisedLoginAddress(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" || port < 1 {
		return ""
	}

	return net.JoinHostPort(host, strconv.Itoa(port))
}

func processorModelLabel(model, architecture string) string {
	identity := strings.TrimSpace(model)
	if identity == "" {
		identity = strings.TrimSpace(architecture)
	}

	return identity
}

func processorCountLabel(processors int) string {
	unit := "logical CPUs"
	if processors == 1 {
		unit = "logical CPU"
	}

	return fmt.Sprintf("%d %s", processors, unit)
}

func loginStatusMemory(total, free uint64, unit uint32) string {
	memory, available := nodeMemoryFromValues(total, free, unit)
	if !available {
		return ""
	}

	return loginStatusMemoryObservation(memory)
}

func loginStatusMemoryObservation(memory nodeMemoryObservation) string {
	totalBytes, available := signedSystemBytes(memory.totalBytes)
	if !available {
		return ""
	}
	capacity := humanBytes(totalBytes) + " total"
	if !memory.freeAvailable {
		return capacity
	}
	freeBytes, available := signedSystemBytes(memory.freeBytes)
	if !available || memory.freeBytes > memory.totalBytes {
		return capacity
	}

	return capacity + " · " + humanBytes(freeBytes) + " free"
}

func loginStatusFileSystemFree(blocks uint64, blockSize int64) string {
	bytes, available := signedFileSystemBytes(blocks, blockSize)
	if !available {
		return ""
	}

	return humanBytes(bytes)
}

func signedFileSystemBytes(blocks uint64, blockSize int64) (int64, bool) {
	if blockSize < 1 || blocks > maximumSystemObservationBytes/uint64(blockSize) {
		return 0, false
	}

	return signedSystemBytes(blocks * uint64(blockSize))
}

func signedSystemBytes(value uint64) (int64, bool) {
	if value > maximumSystemObservationBytes {
		return 0, false
	}

	return int64(value), true
}

func loginNodeUptime(startedAt, now time.Time) string {
	if startedAt.IsZero() || now.Before(startedAt) {
		return ""
	}

	return now.Sub(startedAt).Truncate(time.Second).String()
}
