package adminui

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

// HistoryPoint is one sampled value of a performance series.
type HistoryPoint struct {
	At    time.Time
	Value float64
}

// HistorySeries is one metric's recent history, oldest point first.
type HistorySeries struct {
	Name   string
	Unit   string
	Points []HistoryPoint
}

// PerformanceHistorySource supplies the sampled short-horizon history the
// Performance page charts; the node wires it to the metrics sampler.
type PerformanceHistorySource interface {
	Series() []HistorySeries
}

type historyView struct {
	Name       string
	Unit       string
	Latest     string
	ObservedAt string
	Peak       string
	Window     string
	SVG        template.HTML
	Samples    int
}

const (
	sparklineWidth  = 240
	sparklineHeight = 48
)

// performanceHistory renders every sampled series that has at least two
// points; a shorter series has no shape to draw yet.
func performanceHistory(source PerformanceHistorySource) []historyView {
	if source == nil {
		return nil
	}
	views := make([]historyView, 0, 8)
	for _, series := range source.Series() {
		if len(series.Points) < 2 {
			continue
		}
		latest := series.Points[len(series.Points)-1]
		views = append(views, historyView{
			Name:       series.Name,
			Unit:       series.Unit,
			Latest:     formatHistoryValue(latest.Value),
			ObservedAt: latest.At.UTC().Format(time.RFC3339),
			Peak:       formatHistoryValue(peakValue(series.Points)),
			Window: series.Points[len(series.Points)-1].At.
				Sub(series.Points[0].At).Round(time.Second).String(),
			SVG:     sparklineSVG(series.Points),
			Samples: len(series.Points),
		})
	}

	return views
}

func peakValue(points []HistoryPoint) float64 {
	peak := points[0].Value
	for _, point := range points[1:] {
		if point.Value > peak {
			peak = point.Value
		}
	}

	return peak
}

func formatHistoryValue(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%d", int64(value))
	}

	return strings.TrimRight(fmt.Sprintf("%.2f", value), "0")
}

// sparklineSVG draws the series as a polyline normalized into a fixed viewBox.
// The markup is built purely from numbers, so it is safe to emit as
// template.HTML; a flat series draws a midline rather than dividing by zero.
func sparklineSVG(points []HistoryPoint) template.HTML {
	minValue, maxValue := points[0].Value, points[0].Value
	for _, point := range points[1:] {
		minValue = min(minValue, point.Value)
		maxValue = max(maxValue, point.Value)
	}
	span := maxValue - minValue
	coords := make([]string, 0, len(points))
	step := float64(sparklineWidth) / float64(len(points)-1)
	for i, point := range points {
		y := float64(sparklineHeight) / 2
		if span > 0 {
			y = float64(
				sparklineHeight,
			) - (point.Value-minValue)/span*float64(
				sparklineHeight-4,
			) - 2
		}
		coords = append(coords, fmt.Sprintf("%.1f,%.1f", float64(i)*step, y))
	}
	// nosemgrep
	svg := fmt.Sprintf(
		`<svg class="cds-sparkline" viewBox="0 0 %d %d" width="%d" height="%d"`+
			` preserveAspectRatio="none" role="img" aria-hidden="true">`+
			`<polyline points="%s" fill="none" stroke="currentColor" stroke-width="1.5"/></svg>`,
		sparklineWidth, sparklineHeight, sparklineWidth, sparklineHeight,
		strings.Join(coords, " "),
	)

	// nosemgrep
	return template.HTML(svg) //nolint:gosec // numeric-only markup
}
