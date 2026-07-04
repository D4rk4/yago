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
		IndexQueueSize:   3,
		ConnectedPeers:   5,
		LocalRWIWords:    1234,
		StorageAvailable: true,
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
