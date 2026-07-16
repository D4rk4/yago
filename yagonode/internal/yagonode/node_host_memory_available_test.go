package yagonode

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"
)

func memoryInformation(input string) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(input)), nil
	}
}

func systemMemory(total, free uint64) func(*syscall.Sysinfo_t) error {
	return func(information *syscall.Sysinfo_t) error {
		information.Totalram = total
		information.Freeram = free
		information.Unit = 1 << 30

		return nil
	}
}

func TestMemoryAvailabilityParserAcceptsOneBoundedKernelField(t *testing.T) {
	t.Parallel()

	observation := parseMemoryAvailability(strings.NewReader(
		"MemTotal: 8388608 kB\nMemFree: 1048576 kB\n  MemAvailable:\t6291456 kB  \n",
	))
	if !observation.present || !observation.valid || observation.bytes != 6<<30 {
		t.Fatalf("memory availability = %+v", observation)
	}
}

func TestMemoryAvailabilityParserRejectsMalformedDuplicateAndOverflowFields(t *testing.T) {
	t.Parallel()

	overflow := fmt.Sprintf("MemAvailable: %d kB\n", maximumSystemObservationBytes/1024+1)
	tests := []string{
		"MemAvailable 1024 kB\n",
		"MemAvailable: unknown kB\n",
		"MemAvailable: 1024 KB\n",
		"MemAvailable: 1024 kB trailing\n",
		"MemAvailableExtra: 1024 kB\n",
		"MemAvailable: 1024 kB\nMemAvailable: 1024 kB\n",
		overflow,
	}
	for _, input := range tests {
		observation := parseMemoryAvailability(strings.NewReader(input))
		if !observation.present || observation.valid {
			t.Fatalf("malformed field became valid for %q: %+v", input, observation)
		}
	}
}

func TestMemoryAvailabilityParserBoundsInputAndDistinguishesMissingField(t *testing.T) {
	t.Parallel()

	missing := parseMemoryAvailability(strings.NewReader("MemTotal: 1024 kB\nMemFree: 512 kB\n"))
	if missing.present || missing.valid || missing.bytes != 0 {
		t.Fatalf("missing field = %+v", missing)
	}
	oversized := parseMemoryAvailability(strings.NewReader(
		strings.Repeat("x", maximumProcMemoryInformationBytes+1),
	))
	if !oversized.present || oversized.valid {
		t.Fatalf("oversized input = %+v", oversized)
	}
	failed := parseMemoryAvailability(iotest.ErrReader(errors.New("read failed")))
	if !failed.present || failed.valid {
		t.Fatalf("failed input = %+v", failed)
	}
	if nilReader := parseMemoryAvailability(nil); nilReader.present {
		t.Fatalf("nil input = %+v", nilReader)
	}
}

func TestHostMemorySamplePrefersAvailableAndFallsBackOnlyWhenAbsent(t *testing.T) {
	t.Parallel()

	memory, available := sampleHostMemory(
		memoryInformation("MemAvailable: 6291456 kB\n"),
		systemMemory(8, 2),
	)
	if !available || memory.TotalBytes != 8<<30 || !memory.AvailableObserved ||
		memory.AvailableBytes != 6<<30 {
		t.Fatalf("available-memory sample = %+v, %t", memory, available)
	}

	for _, open := range []func() (io.ReadCloser, error){
		nil,
		func() (io.ReadCloser, error) { return nil, errors.New("missing") },
		memoryInformation("MemTotal: 8388608 kB\nMemFree: 2097152 kB\n"),
	} {
		memory, available = sampleHostMemory(open, systemMemory(8, 2))
		if !available || memory.TotalBytes != 8<<30 || !memory.AvailableObserved ||
			memory.AvailableBytes != 2<<30 {
			t.Fatalf("sysinfo fallback = %+v, %t", memory, available)
		}
	}
}

func TestHostMemorySampleDoesNotMaskInvalidAvailableField(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"MemAvailable: invalid kB\n",
		"MemAvailable: 9437184 kB\n",
		"MemAvailable: 1024 kB\nMemAvailable: 2048 kB\n",
	} {
		memory, available := sampleHostMemory(memoryInformation(input), systemMemory(8, 2))
		if !available || memory.TotalBytes != 8<<30 || memory.AvailableObserved ||
			memory.AvailableBytes != 0 {
			t.Fatalf("invalid available-memory sample = %+v, %t", memory, available)
		}
	}

	opened := false
	memory, available := sampleHostMemory(func() (io.ReadCloser, error) {
		opened = true

		return io.NopCloser(strings.NewReader("MemAvailable: 1 kB\n")), nil
	}, func(*syscall.Sysinfo_t) error {
		return errors.New("sysinfo failed")
	})
	if available || opened || memory.TotalBytes != 0 || memory.AvailableBytes != 0 ||
		memory.AvailableObserved {
		t.Fatalf("failed total sample = %+v, %t, opened=%t", memory, available, opened)
	}
}
