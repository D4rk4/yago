package adminui

import (
	"strings"
	"testing"
)

func TestOverviewRendersProcessMemoryDiagnostics(t *testing.T) {
	t.Parallel()

	overview := sampleOverview()
	overview.ProcessMemory = ProcessMemory{
		ResidentBytes:          3 << 30,
		ResidentAvailable:      true,
		AnonymousBytes:         1 << 30,
		AnonymousAvailable:     true,
		FileBackedBytes:        2 << 30,
		FileBackedAvailable:    true,
		SharedMemoryBytes:      0,
		SharedMemoryAvailable:  true,
		GoHeapObjectsBytes:     256 << 20,
		GoHeapObjectsAvailable: true,
	}
	console := New(Options{Overview: fakeOverview{snap: overview}})

	for _, page := range []capture{
		do(t, console, "/admin/overview"),
		do(t, console, "/admin/overview/metrics"),
	} {
		for _, want := range []string{
			"Memory diagnostics",
			"Process resident set (RSS)",
			"Anonymous RSS",
			"File-backed RSS",
			"Shared-memory RSS",
			"Go heap objects",
			"3.0 GiB",
			"1.0 GiB",
			"2.0 GiB",
			"0 B",
			"256.0 MiB",
			"Go heap objects are a runtime allocation measure",
			"do not add them to RSS",
		} {
			if !strings.Contains(page.body, want) {
				t.Fatalf("memory diagnostics missing %q", want)
			}
		}
	}
}
