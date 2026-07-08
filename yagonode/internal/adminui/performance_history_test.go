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
		{Name: "too short", Unit: "x", Points: historyAt(base, 9)},
		{Name: "flat", Unit: "entries", Points: historyAt(base, 5, 5, 5)},
	}})

	if len(views) != 2 {
		t.Fatalf("views = %d, want the one-point series skipped", len(views))
	}
	first := views[0]
	if first.Current != "2" || first.Peak != "4.25" || first.Window != "20s" || first.Samples != 3 {
		t.Fatalf("view mismatch: %+v", first)
	}
	svg := string(first.SVG)
	if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "<polyline points=") {
		t.Fatalf("sparkline markup missing: %s", svg)
	}
	if !strings.Contains(svg, "0.0,") || !strings.Contains(svg, "240.0,") {
		t.Fatalf("sparkline must span the full width: %s", svg)
	}

	flat := string(views[1].SVG)
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
		"2 samples over 10s",
		`href="/admin/backup"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Errorf("performance history missing %q", want)
		}
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
