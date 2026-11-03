package adminui

import (
	"context"
	"fmt"
	"time"
)

type CrawlerWorkerPulse struct {
	WorkerID                   string
	ProcessLabel               string
	ActiveFetches              int
	ActiveFetchesKnown         bool
	FetchLimit                 int
	FetchAdmissionWaiting      int
	FetchAdmissionWaitingKnown bool
	FetchedPagesPerMinute      float64
	IndexedPagesPerMinute      float64
	RateKnown                  bool
	ObservedAt                 time.Time
}

type CrawlerPulse struct {
	Available    bool
	QueuePending int
	QueueKnown   bool
	Workers      []CrawlerWorkerPulse
}

type CrawlerPulseSource interface {
	CrawlerPulse(ctx context.Context) CrawlerPulse
}

type crawlerWorkerPulseView struct {
	WorkerID     string
	ProcessLabel string
	Active       string
	Waiting      string
	Fetched      string
	Indexed      string
	LastReport   string
}

type crawlerPulseView struct {
	Available    bool
	Refresh      bool
	Workers      int
	Fetched      string
	Indexed      string
	QueuePending string
	WorkerRows   []crawlerWorkerPulseView
	Hints        []string
}

type crawlerPulseMeasurements struct {
	fetchedTotal    float64
	indexedTotal    float64
	knownRates      int
	knownActivity   int
	unknownActivity int
	activeTotal     int
	waitingTotal    int
	fetchCapacity   int
	waitingKnown    bool
}

func buildCrawlerPulseView(pulse CrawlerPulse, now time.Time) crawlerPulseView {
	view := crawlerPulseView{Available: pulse.Available, Refresh: pulse.Available}
	if !pulse.Available {
		return view
	}
	view.Workers = len(pulse.Workers)
	if pulse.QueueKnown {
		view.QueuePending = formatPulseInteger(pulse.QueuePending)
	} else {
		view.QueuePending = "Unavailable"
	}
	measurements := crawlerPulseMeasurements{waitingKnown: true}

	for _, worker := range pulse.Workers {
		view.WorkerRows = append(view.WorkerRows, buildCrawlerWorkerPulseView(worker, now))
		measurements.record(worker)
	}
	view.Fetched = pulseAggregateRate(
		measurements.fetchedTotal,
		measurements.knownRates,
		len(pulse.Workers),
		measurements.knownActivity,
		measurements.activeTotal,
	)
	view.Indexed = pulseAggregateRate(
		measurements.indexedTotal,
		measurements.knownRates,
		len(pulse.Workers),
		measurements.knownActivity,
		measurements.activeTotal,
	)
	view.Hints = crawlerPulseHints(pulse, measurements)

	return view
}

func buildCrawlerWorkerPulseView(worker CrawlerWorkerPulse, now time.Time) crawlerWorkerPulseView {
	row := crawlerWorkerPulseView{
		WorkerID:     worker.WorkerID,
		ProcessLabel: worker.ProcessLabel,
		Active:       pulseActiveFetches(worker),
		Waiting:      pulseWaitingFetches(worker),
		Fetched:      pulseWorkerRate(worker, worker.FetchedPagesPerMinute),
		Indexed:      pulseWorkerRate(worker, worker.IndexedPagesPerMinute),
		LastReport:   "Unavailable",
	}
	if row.ProcessLabel == "" {
		row.ProcessLabel = "Unavailable"
	}
	if !worker.ObservedAt.IsZero() {
		row.LastReport = pulseAge(now.Sub(worker.ObservedAt))
	}

	return row
}

func (measurements *crawlerPulseMeasurements) record(worker CrawlerWorkerPulse) {
	if worker.ActiveFetchesKnown {
		measurements.knownActivity++
		measurements.activeTotal += worker.ActiveFetches
		if worker.FetchLimit > 0 {
			measurements.fetchCapacity += worker.FetchLimit
		}
	} else {
		measurements.unknownActivity++
		measurements.waitingKnown = false
	}
	if worker.FetchAdmissionWaitingKnown {
		measurements.waitingTotal += worker.FetchAdmissionWaiting
	} else {
		measurements.waitingKnown = false
	}
	if worker.RateKnown {
		measurements.knownRates++
		measurements.fetchedTotal += worker.FetchedPagesPerMinute
		measurements.indexedTotal += worker.IndexedPagesPerMinute
	}
}

func pulseActiveFetches(worker CrawlerWorkerPulse) string {
	if !worker.ActiveFetchesKnown {
		return "Unavailable"
	}
	if worker.FetchLimit <= 0 {
		return formatPulseInteger(worker.ActiveFetches)
	}

	return fmt.Sprintf("%d / %d", worker.ActiveFetches, worker.FetchLimit)
}

func pulseWaitingFetches(worker CrawlerWorkerPulse) string {
	if !worker.FetchAdmissionWaitingKnown {
		return "Unavailable"
	}

	return formatPulseInteger(worker.FetchAdmissionWaiting)
}

func pulseWorkerRate(worker CrawlerWorkerPulse, value float64) string {
	if worker.RateKnown {
		return formatPagesPerMinute(value)
	}

	return pulseRateWithoutMeasurement(worker)
}

func pulseAggregateRate(value float64, known, total, knownActivity, active int) string {
	if known == 0 {
		if total == 0 || knownActivity != total {
			return "Unavailable"
		}
		if active == 0 {
			return "Idle"
		}

		return "Collecting"
	}
	rate := formatPagesPerMinute(value)
	if known != total {
		return fmt.Sprintf("%s (%d/%d workers reporting)", rate, known, total)
	}

	return rate
}

func pulseRateWithoutMeasurement(worker CrawlerWorkerPulse) string {
	if worker.ActiveFetchesKnown && worker.ActiveFetches == 0 {
		return "Idle"
	}
	if !worker.ActiveFetchesKnown {
		return "Unavailable"
	}

	return "Collecting"
}

func formatPagesPerMinute(value float64) string {
	return fmt.Sprintf("%.1f", value)
}

func formatPulseInteger(value int) string {
	return fmt.Sprintf("%d", value)
}

func pulseAge(age time.Duration) string {
	if age < 0 {
		age = 0
	}
	return age.Round(time.Second).String() + " ago"
}

func crawlerPulseHints(pulse CrawlerPulse, measurements crawlerPulseMeasurements) []string {
	hints := make([]string, 0, 2)
	if pulse.QueueKnown && pulse.QueuePending > 0 && len(pulse.Workers) == 0 {
		hints = append(hints, "Crawl orders are queued with no connected crawler workers.")
	}
	if len(pulse.Workers) > 0 && measurements.knownActivity == 0 &&
		measurements.unknownActivity > 0 {
		hints = append(
			hints,
			"Crawler fetch activity is unavailable until a current heartbeat arrives.",
		)
	}
	if !pulse.QueueKnown || pulse.QueuePending == 0 ||
		measurements.knownActivity != len(pulse.Workers) {
		return hints
	}
	if measurements.waitingKnown && measurements.activeTotal > 0 &&
		float64(measurements.waitingTotal)/float64(measurements.activeTotal) >= 0.8 {
		hints = append(
			hints,
			"Most active fetch jobs are waiting for a start permit; adding workers is unlikely to raise throughput.",
		)
		return hints
	}
	if measurements.fetchCapacity > 0 && measurements.activeTotal >= measurements.fetchCapacity {
		hints = append(
			hints,
			"Queued orders remain while all reported fetch slots are occupied; additional worker capacity may help.",
		)
		return hints
	}
	if measurements.fetchCapacity > 0 &&
		measurements.activeTotal*2 < measurements.fetchCapacity {
		hints = append(
			hints,
			"Queued orders remain while most reported fetch slots are idle; inspect worker health and per-host politeness.",
		)
	}

	return hints
}
