package yagonode

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

type fixedProcessMemoryDiagnostics struct {
	observation adminui.ProcessMemory
}

func (diagnostics fixedProcessMemoryDiagnostics) ProcessMemory() adminui.ProcessMemory {
	return diagnostics.observation
}

func TestProcessMemoryDiagnosticsReportsLinuxResidentSplitAndGoHeap(t *testing.T) {
	statusPath := ""
	diagnostics := currentProcessMemory{
		readStatus: func(path string) ([]byte, error) {
			statusPath = path

			return []byte(
				"Name:\tyago-node\nVmSize:\t8192 kB\nVmRSS:\t3072 kB\nRssAnon:\t1024 kB\nRssFile:\t2048 kB\nRssShmem:\t0 kB\n",
			), nil
		},
		heapObjects: func() uint64 { return 512 << 20 },
	}

	observation := diagnostics.ProcessMemory()
	if statusPath != linuxProcessStatusPath {
		t.Fatalf("status path = %q", statusPath)
	}
	if !observation.ResidentAvailable || observation.ResidentBytes != 3<<20 ||
		!observation.AnonymousAvailable || observation.AnonymousBytes != 1<<20 ||
		!observation.FileBackedAvailable || observation.FileBackedBytes != 2<<20 ||
		!observation.SharedMemoryAvailable || observation.SharedMemoryBytes != 0 ||
		!observation.GoHeapObjectsAvailable || observation.GoHeapObjectsBytes != 512<<20 {
		t.Fatalf("process memory = %+v", observation)
	}
}

func TestProcessMemoryDiagnosticsKeepsIndependentAvailability(t *testing.T) {
	readFailure := errors.New("status unavailable")
	withoutStatus := currentProcessMemory{
		readStatus:  func(string) ([]byte, error) { return nil, readFailure },
		heapObjects: func() uint64 { return 64 << 20 },
	}.ProcessMemory()
	if withoutStatus.ResidentAvailable || withoutStatus.AnonymousAvailable ||
		withoutStatus.FileBackedAvailable || withoutStatus.SharedMemoryAvailable ||
		!withoutStatus.GoHeapObjectsAvailable || withoutStatus.GoHeapObjectsBytes != 64<<20 {
		t.Fatalf("status failure = %+v", withoutStatus)
	}

	malformedStatus := currentProcessMemory{
		readStatus: func(string) ([]byte, error) {
			return []byte(
				"VmRSS: nope kB\nRssAnon: 1 MB\nRssFile: 9007199254740992 kB\nRssShmem: 1\n",
			), nil
		},
		heapObjects: func() uint64 { return 0 },
	}.ProcessMemory()
	if malformedStatus.ResidentAvailable || malformedStatus.AnonymousAvailable ||
		malformedStatus.FileBackedAvailable || malformedStatus.SharedMemoryAvailable ||
		!malformedStatus.GoHeapObjectsAvailable || malformedStatus.GoHeapObjectsBytes != 0 {
		t.Fatalf("malformed status = %+v", malformedStatus)
	}

	overflowedHeap := currentProcessMemory{
		readStatus:  func(string) ([]byte, error) { return []byte("VmRSS: 0 kB\n"), nil },
		heapObjects: func() uint64 { return uint64(maximumProcessMemoryBytes) + 1 },
	}.ProcessMemory()
	if !overflowedHeap.ResidentAvailable || overflowedHeap.ResidentBytes != 0 ||
		overflowedHeap.GoHeapObjectsAvailable {
		t.Fatalf("overflowed heap = %+v", overflowedHeap)
	}
}

func TestOverviewIncludesInjectedProcessMemory(t *testing.T) {
	want := adminui.ProcessMemory{
		ResidentBytes:          256 << 20,
		ResidentAvailable:      true,
		GoHeapObjectsBytes:     64 << 20,
		GoHeapObjectsAvailable: true,
	}
	got := newOverviewSource(stubReport{}).
		withProcessMemory(fixedProcessMemoryDiagnostics{observation: want}).
		Overview(t.Context()).ProcessMemory
	if got != want {
		t.Fatalf("overview process memory = %+v, want %+v", got, want)
	}
}

func TestCurrentProcessMemoryDependenciesAreAvailable(t *testing.T) {
	diagnostics := newCurrentProcessMemory()
	if diagnostics.readStatus == nil || diagnostics.heapObjects == nil {
		t.Fatal("current process memory dependencies are absent")
	}
	_ = currentGoHeapObjectsBytes()
}
