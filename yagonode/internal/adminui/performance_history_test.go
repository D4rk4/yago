package adminui

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeHistory struct{ series []HistorySeries }

func (f fakeHistory) Series() []HistorySeries { return f.series }

func historyAt(base time.Time, values ...float64) []HistoryPoint {
	points := make([]HistoryPoint, 0, len(values))
	for i, value := range values {
		points = append(
			points,
			HistoryPoint{At: base.Add(time.Duration(i) * 10 * time.Second), Value: value},
		)
	}

	return points
}

func TestPerformanceHistoryBuildsSparklineViews(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	views := performanceHistory(fakeHistory{series: []HistorySeries{
		{Name: "HTTP requests", Unit: "req/s", Points: historyAt(base, 1, 4.25, 2)},
		{Name: "Process memory", Unit: "bytes", Points: historyAt(base, 32<<20, 64<<20)},
		{Name: "too short", Unit: "x", Points: historyAt(base, 9)},
		{Name: "flat", Unit: "entries", Points: historyAt(base, 5, 5, 5)},
	}})

	if len(views) != 3 {
		t.Fatalf("views = %d, want the one-point series skipped", len(views))
	}
	first := views[0]
	if first.Latest != "2" || first.ObservedAt != "2026-07-08T12:00:20Z" ||
		first.Peak != "4.25" || first.Window != "20s" || first.Samples != 3 {
		t.Fatalf("view mismatch: %+v", first)
	}
	svg := string(first.SVG)
	if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "<polyline points=") {
		t.Fatalf("sparkline markup missing: %s", svg)
	}
	if !strings.Contains(svg, "0.0,") || !strings.Contains(svg, "240.0,") {
		t.Fatalf("sparkline must span the full width: %s", svg)
	}

	memory := views[1]
	if memory.Latest != "64.0 MiB" || memory.Peak != "64.0 MiB" || memory.Unit != "" {
		t.Fatalf("byte history = %+v", memory)
	}

	flat := string(views[2].SVG)
	if !strings.Contains(flat, "24.0") {
		t.Fatalf("a flat series must draw the midline, got %s", flat)
	}

	if performanceHistory(nil) != nil {
		t.Fatal("a nil source must yield no views")
	}
}

func TestPerformancePageRendersHistory(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	console := New(Options{
		Performance: fakePerformance{snap: PerformanceStatus{Available: true}},
		PerformanceHistory: fakeHistory{series: []HistorySeries{
			{Name: "HTTP requests", Unit: "req/s", Points: historyAt(base, 1, 2)},
			{Name: "Process memory", Unit: "bytes", Points: historyAt(base, 32<<20, 64<<20)},
		}},
	})
	got := do(t, console, "/admin/performance")
	if got.status != http.StatusOK {
		t.Fatalf("status = %d", got.status)
	}
	for _, want := range []string{
		"Recent history",
		"HTTP requests",
		"<polyline points=",
		"latest observed 2026-07-08T12:00:10Z",
		"2 samples over 10s",
		"64.0 MiB",
		`href="/admin/backup"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Errorf("performance history missing %q", want)
		}
	}
	if strings.Contains(got.body, "67108864") || strings.Contains(got.body, "MiB bytes") {
		t.Fatalf("performance history exposed raw or duplicated byte units: %s", got.body)
	}
}

func TestPerformancePageHintsWhileHistoryWarmsUp(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Performance:        fakePerformance{snap: PerformanceStatus{Available: true}},
		PerformanceHistory: fakeHistory{},
	})
	got := do(t, console, "/admin/performance")
	if !strings.Contains(got.body, "History charts appear after") {
		t.Fatal("warm-up hint missing")
	}
}
