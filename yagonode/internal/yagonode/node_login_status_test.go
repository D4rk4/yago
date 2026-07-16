package yagonode

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLoginNodeStatusUsesOnlyBoundedAdvertisedAndSystemFacts(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	readPath := ""
	source := loginNodeStatusSource{
		nodeName:       "search-node",
		swarmAddress:   "search.example:8090",
		processorModel: "Intel Xeon Test CPU",
		dataDirectory:  "/srv/yago-state",
		version:        "v0.0.8",
		startedAt:      started,
		systemReads: loginStatusSystemReads{
			architecture:   func() string { return "amd64" },
			processorCount: func() int { return 8 },
			memory: func(info *syscall.Sysinfo_t) error {
				info.Totalram = 32
				info.Freeram = 12
				info.Unit = 1 << 30

				return nil
			},
			fileSystem: func(path string, info *syscall.Statfs_t) error {
				readPath = path
				info.Bavail = 250
				info.Bsize = 4096

				return nil
			},
			now: func() time.Time { return started.Add(2*time.Hour + 3*time.Minute + 4*time.Second) },
		},
	}
	status := source.LoginNodeStatus(context.Background())
	if status.NodeName != "search-node" || status.SwarmAddress != "search.example:8090" ||
		status.ProcessorModel != "Intel Xeon Test CPU" ||
		status.ProcessorCount != "8 logical CPUs" ||
		status.MemoryCapacity != "32.0 GiB total · 12.0 GiB free" ||
		status.DataFreeSpace != "1000.0 KiB" || status.Version != "v0.0.8" ||
		status.Uptime != "2h3m4s" {
		t.Fatalf("login status = %+v", status)
	}
	if readPath != "/srv/yago-state" {
		t.Fatalf("filesystem path = %q", readPath)
	}
}

func TestLoginNodeStatusFailsUnavailableFieldByField(t *testing.T) {
	t.Parallel()

	failure := errors.New("measurement unavailable")
	started := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	source := loginNodeStatusSource{
		nodeName:     "search-node",
		swarmAddress: "search.example:8090",
		version:      "v0.0.8",
		startedAt:    started,
		systemReads: loginStatusSystemReads{
			architecture:   func() string { return "amd64" },
			processorCount: func() int { return 0 },
			memory:         func(*syscall.Sysinfo_t) error { return failure },
			fileSystem:     func(string, *syscall.Statfs_t) error { return failure },
			now:            func() time.Time { return started.Add(time.Minute) },
		},
	}
	status := source.LoginNodeStatus(context.Background())
	if status.NodeName != "search-node" || status.SwarmAddress != "search.example:8090" ||
		status.ProcessorModel != "amd64" || status.Version != "v0.0.8" || status.Uptime != "1m0s" {
		t.Fatalf("static status was lost: %+v", status)
	}
	if status.ProcessorCount != "" || status.MemoryCapacity != "" || status.DataFreeSpace != "" {
		t.Fatalf("failed measurements leaked values: %+v", status)
	}
}

func TestLoginNodeStatusSkipsMeasurementsAfterRequestCancellation(t *testing.T) {
	t.Parallel()

	called := false
	now := time.Now()
	source := loginNodeStatusSource{
		nodeName:  "node",
		startedAt: now,
		systemReads: loginStatusSystemReads{
			architecture:   func() string { return "amd64" },
			processorCount: func() int { called = true; return 1 },
			memory:         func(*syscall.Sysinfo_t) error { called = true; return nil },
			fileSystem:     func(string, *syscall.Statfs_t) error { called = true; return nil },
			now:            func() time.Time { return now },
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	status := source.LoginNodeStatus(ctx)
	if called || status.NodeName != "node" || status.Uptime != "0s" {
		t.Fatalf("cancelled status = %+v, called=%v", status, called)
	}
}

func TestLoginNodeStatusFormattingRejectsInvalidMeasurements(t *testing.T) {
	t.Parallel()

	if loginStatusMemory(0, 0, 1) != "" ||
		loginStatusMemory(maximumSystemObservationBytes, 0, 2) != "" ||
		loginStatusMemoryObservation(nodeMemoryObservation{
			totalBytes: maximumSystemObservationBytes + 1,
		}) != "" ||
		loginStatusFileSystemFree(1, 0) != "" ||
		loginStatusFileSystemFree(maximumSystemObservationBytes, 2) != "" {
		t.Fatal("invalid or overflowing measurements were formatted")
	}
	if loginStatusMemory(1024, 512, 0) != "1.0 KiB total · 512 B free" ||
		loginStatusMemory(1024, maximumSystemObservationBytes, 2) != "2.0 KiB total" ||
		loginStatusMemory(1024, 2048, 1) != "1.0 KiB total" ||
		loginStatusMemoryObservation(nodeMemoryObservation{
			totalBytes:    1024,
			freeBytes:     maximumSystemObservationBytes + 1,
			freeAvailable: true,
		}) != "1.0 KiB total" ||
		loginStatusFileSystemFree(0, 4096) != "0 B" {
		t.Fatal("valid zero-unit or full-filesystem measurements were rejected")
	}
	if processorModelLabel("Intel Test", "amd64") != "Intel Test" ||
		processorModelLabel("", "amd64") != "amd64" ||
		processorModelLabel("", "") != "" || processorCountLabel(1) != "1 logical CPU" ||
		processorCountLabel(3) != "3 logical CPUs" {
		t.Fatal("processor labels are incorrect")
	}
}

func TestNodeMemoryObservationBoundsTotalAndFree(t *testing.T) {
	t.Parallel()

	if _, available := readNodeMemory(nil); available {
		t.Fatal("nil system memory read became available")
	}
	if _, available := readNodeMemory(func(*syscall.Sysinfo_t) error {
		return errors.New("unavailable")
	}); available {
		t.Fatal("failed system memory read became available")
	}
	observation, available := readNodeMemory(func(info *syscall.Sysinfo_t) error {
		info.Totalram = 8
		info.Freeram = 9
		info.Unit = 1 << 30

		return nil
	})
	if !available || !observation.freeAvailable || observation.totalBytes != 8<<30 ||
		observation.freeBytes != 9<<30 {
		t.Fatalf("system memory observation = %+v, %t", observation, available)
	}
	if got := loginStatusMemoryObservation(observation); got != "8.0 GiB total" {
		t.Fatalf("underflowing login memory = %q", got)
	}

	observation, available = nodeMemoryFromValues(
		1024,
		maximumSystemObservationBytes,
		2,
	)
	if !available || observation.freeAvailable || observation.totalBytes != 2048 {
		t.Fatalf("overflowing free memory = %+v, %t", observation, available)
	}
	if _, available = nodeMemoryFromValues(maximumSystemObservationBytes, 0, 2); available {
		t.Fatal("overflowing total memory became available")
	}
}

func TestCurrentHostMemoryUsesBoundedSystemSnapshot(t *testing.T) {
	t.Parallel()

	memory, available := currentHostMemory()
	if !available || memory.TotalBytes == 0 || memory.TotalBytes > maximumSystemObservationBytes {
		t.Fatalf("current host memory = %+v, %t", memory, available)
	}
}

func TestProcessorModelParsingPrefersModelAndBoundsFallbacks(t *testing.T) {
	t.Parallel()

	longModel := strings.Repeat("界", maximumProcessorModelRunes+20)
	for _, test := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "model name", input: "processor : 0\nProcessor : ARM fallback\nmodel name : Intel Xeon Gold 6338N\nHardware : board\n", want: "Intel Xeon Gold 6338N"},
		{name: "processor fallback", input: "processor : 0\nProcessor : ARMv8   Processor rev 1\nHardware : board\n", want: "ARMv8 Processor rev 1"},
		{name: "hardware fallback", input: "processor : 0\nHardware : BCM2711\n", want: "BCM2711"},
		{name: "malformed", input: "model name\nprocessor : 0\nProcessor : \t\nHardware no delimiter\n", want: ""},
		{name: "long model", input: "model name : " + longModel + "\n", want: strings.Repeat("界", maximumProcessorModelRunes)},
		{name: "control whitespace", input: "model name : Intel\t Xeon\r CPU\n", want: "Intel Xeon CPU"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := processorModelFromReader(strings.NewReader(test.input))
			if err != nil || got != test.want {
				t.Fatalf("processor model = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestProcessorModelParsingRejectsOversizedLine(t *testing.T) {
	t.Parallel()

	line := "model name : " + strings.Repeat("x", maximumProcessorInformationLine+1)
	if model, err := processorModelFromReader(strings.NewReader(line)); err == nil || model != "" {
		t.Fatalf("oversized model = %q, %v", model, err)
	}
}

func TestAdvertisedLoginAddressAndUptimeValidation(t *testing.T) {
	t.Parallel()

	if advertisedLoginAddress(" 2001:db8::1 ", 8090) != "[2001:db8::1]:8090" {
		t.Fatal("IPv6 swarm address was not normalized")
	}
	if advertisedLoginAddress("", 8090) != "" || advertisedLoginAddress("peer.example", 0) != "" {
		t.Fatal("incomplete swarm address was exposed")
	}
	now := time.Now()
	if loginNodeUptime(time.Time{}, now) != "" || loginNodeUptime(now.Add(time.Second), now) != "" {
		t.Fatal("invalid uptime was exposed")
	}
}

func TestNewLoginNodeStatusSourceUsesEffectiveNodeConfiguration(t *testing.T) {
	t.Parallel()

	source := newLoginNodeStatusSource(nodeConfig{
		Name:          "effective-node",
		AdvertiseHost: "peer.example",
		AdvertisePort: 8090,
		DataDir:       t.TempDir(),
	})
	status := source.LoginNodeStatus(context.Background())
	if status.NodeName != "effective-node" || status.SwarmAddress != "peer.example:8090" ||
		status.Version != Version() || status.ProcessorModel == "" || status.ProcessorCount == "" || status.MemoryCapacity == "" ||
		status.DataFreeSpace == "" || strings.HasPrefix(status.DataFreeSpace, "-") {
		t.Fatalf("effective login status = %+v", status)
	}
}

func TestLoginNodeStatusReadsProcessorModelOnceAtAssembly(t *testing.T) {
	t.Parallel()

	modelReads := 0
	now := time.Now()
	source := newLoginNodeStatusSourceWithSystemReads(nodeConfig{}, loginStatusSystemReads{
		architecture:   func() string { return "amd64" },
		processorCount: func() int { return 4 },
		processorModel: func() (string, error) {
			modelReads++

			return "Cached CPU", nil
		},
		memory:     func(*syscall.Sysinfo_t) error { return errors.New("unavailable") },
		fileSystem: func(string, *syscall.Statfs_t) error { return errors.New("unavailable") },
		now:        func() time.Time { return now },
	})
	first := source.LoginNodeStatus(context.Background())
	second := source.LoginNodeStatus(context.Background())
	if modelReads != 1 ||
		first.ProcessorModel != "Cached CPU" || first.ProcessorCount != "4 logical CPUs" ||
		second.ProcessorModel != first.ProcessorModel || second.ProcessorCount != first.ProcessorCount {
		t.Fatalf(
			"processor model reads = %d, first = %q/%q, second = %q/%q",
			modelReads,
			first.ProcessorModel,
			first.ProcessorCount,
			second.ProcessorModel,
			second.ProcessorCount,
		)
	}
}

func TestLoginNodeStatusFallsBackToArchitectureWhenProcessorModelFails(t *testing.T) {
	t.Parallel()

	now := time.Now()
	source := newLoginNodeStatusSourceWithSystemReads(nodeConfig{}, loginStatusSystemReads{
		architecture:   func() string { return "arm64" },
		processorCount: func() int { return 2 },
		processorModel: func() (string, error) { return "", errors.New("unavailable") },
		memory:         func(*syscall.Sysinfo_t) error { return errors.New("unavailable") },
		fileSystem:     func(string, *syscall.Statfs_t) error { return errors.New("unavailable") },
		now:            func() time.Time { return now },
	})
	status := source.LoginNodeStatus(context.Background())
	if status.ProcessorModel != "arm64" || status.ProcessorCount != "2 logical CPUs" {
		t.Fatalf("processor fallback = %q/%q", status.ProcessorModel, status.ProcessorCount)
	}
}
