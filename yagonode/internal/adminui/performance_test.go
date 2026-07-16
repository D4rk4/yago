package adminui

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type fakePerformance struct{ snap PerformanceStatus }

func (f fakePerformance) Performance(context.Context) PerformanceStatus { return f.snap }

func TestConsolePerformanceRendersMetrics(t *testing.T) {
	t.Parallel()

	snap := PerformanceStatus{
		Available:        true,
		CrawlQueueSize:   7,
		CrawlQueueKnown:  true,
		IndexQueueSize:   3,
		IndexQueueKnown:  true,
		ConnectedPeers:   5,
		LocalRWIWords:    1234,
		LocalRWIKnown:    true,
		StorageAvailable: true,
		StorageKnown:     true,
	}
	got := do(t, New(Options{Performance: fakePerformance{snap: snap}}), "/admin/performance")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		"Crawl queue", ">7<", "Index queue", ">3<",
		"Connected peers", "Local RWI words", ">1234<", "Available",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("performance page missing %q", want)
		}
	}
}

func TestConsolePerformanceUnavailableWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{}), "/admin/performance")
	if !strings.Contains(got.body, performanceUnavailable) {
		t.Fatal("expected the unavailable message without a performance source")
	}
}

func TestConsolePerformanceRendersUnknownMeasurementsAsUnavailable(t *testing.T) {
	t.Parallel()

	snap := PerformanceStatus{Available: true, ConnectedPeers: 5}
	got := do(t, New(Options{Performance: fakePerformance{snap: snap}}), "/admin/performance")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if strings.Count(mainRegion(t, got.body), ">Unavailable<") != 4 {
		t.Fatalf("unknown measurements must render unavailable: %s", got.body)
	}
	if strings.Contains(got.body, ">Full<") {
		t.Fatalf("unknown storage must not render full: %s", got.body)
	}
}
