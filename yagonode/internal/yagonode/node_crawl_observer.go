package yagonode

import (
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

// attachCrawlRunObserver wires crawl-run progress into the outcome metrics and
// the operator event log when the runtime exposes a run registry. It mirrors
// attachCrawlMetrics: a disabled crawl runtime or a test double leaves both
// untouched, as does a runtime with neither a collector nor a recorder.
func attachCrawlRunObserver(
	runtime crawlProcess,
	collector *metrics.CrawlRunMetrics,
	recorder *events.Recorder,
) {
	provider, ok := runtime.(interface {
		runRegistry() *crawlruns.Registry
	})
	if !ok || (collector == nil && recorder == nil) {
		return
	}

	provider.runRegistry().AddObserver(func(run crawlruns.Run, newlyTerminal bool, active int) {
		if collector != nil {
			collector.SetActive(active)
		}
		if !newlyTerminal {
			return
		}
		if collector != nil {
			collector.ObserveTerminal(run.State, run.Tally)
		}
		recordCrawlRunEvent(recorder, run)
	})
}

// recordCrawlRunEvent logs a finished or cancelled crawl run to the operator
// console, raising the severity to warn for a cancellation or any failed pages.
func recordCrawlRunEvent(recorder *events.Recorder, run crawlruns.Run) {
	if recorder == nil {
		return
	}

	name := "crawl.run.finished"
	severity := events.SeverityInfo
	switch {
	case run.State == yagocrawlcontract.CrawlRunCancelled:
		name, severity = "crawl.run.cancelled", events.SeverityWarn
	case run.Tally.Failed > 0:
		severity = events.SeverityWarn
	}

	recorder.Record(severity, events.CategoryCrawl, name, fmt.Sprintf(
		"crawl %q %s: %d fetched, %d indexed, %d failed, %d robots-denied, %d duplicates",
		crawlRunLabel(run), run.State,
		run.Tally.Fetched, run.Tally.Indexed, run.Tally.Failed,
		run.Tally.RobotsDenied, run.Tally.Duplicates,
	))
}

func crawlRunLabel(run crawlruns.Run) string {
	switch {
	case run.ProfileName != "":
		return run.ProfileName
	case run.ProfileHandle != "":
		return run.ProfileHandle
	default:
		return run.RunID
	}
}
