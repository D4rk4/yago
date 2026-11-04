package yagonode

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
)

type crawlerPulseSource struct {
	control crawlerWorkerActivitySource
	runs    crawlerRunThroughputSource
	probe   func(context.Context) (crawlbroker.QueueDepth, error)
}

type crawlerWorkerActivitySource interface {
	WorkerSnapshots() []crawlbroker.CrawlerWorkerSnapshot
	RuntimeSnapshot() crawlbroker.CrawlerRuntimeSnapshot
}

type crawlerRunThroughputSource interface {
	Throughput(time.Duration) []crawlruns.WorkerThroughput
}

func newCrawlerPulseSource(
	control crawlerWorkerActivitySource,
	runs crawlerRunThroughputSource,
	probe func(context.Context) (crawlbroker.QueueDepth, error),
) adminui.CrawlerPulseSource {
	return crawlerPulseSource{control: control, runs: runs, probe: probe}
}

func (s crawlerPulseSource) CrawlerPulse(ctx context.Context) adminui.CrawlerPulse {
	workers := s.control.WorkerSnapshots()
	rates := s.runs.Throughput(time.Minute)
	type processIdentity struct {
		workerID          string
		processIdentifier string
	}
	rateByProcess := make(map[processIdentity]crawlruns.WorkerThroughput, len(rates))
	for _, rate := range rates {
		rateByProcess[processIdentity{
			workerID:          rate.WorkerID,
			processIdentifier: rate.ProcessIdentifier,
		}] = rate
	}
	capacity := s.control.RuntimeSnapshot().FetchLimitPerCrawler
	pulse := adminui.CrawlerPulse{
		Available: true,
		Workers:   make([]adminui.CrawlerWorkerPulse, 0, len(workers)),
	}
	for _, worker := range workers {
		activePages := make([]adminui.CrawlerActivePage, 0, len(worker.ActivePages))
		for _, activePage := range worker.ActivePages {
			activePages = append(activePages, adminui.CrawlerActivePage{
				RunID:               activePage.RunID,
				URL:                 activePage.URL,
				Stage:               activePage.Stage,
				ElapsedMilliseconds: activePage.ElapsedMilliseconds,
			})
		}
		waitingPages := make([]adminui.CrawlerWaitingPage, 0, len(worker.WaitingPages))
		for _, waitingPage := range worker.WaitingPages {
			waitingPages = append(waitingPages, adminui.CrawlerWaitingPage{
				RunID:       waitingPage.RunID,
				URL:         waitingPage.URL,
				State:       waitingPage.State,
				Reason:      waitingPage.Reason,
				AvailableAt: waitingPage.AvailableAt,
			})
		}
		row := adminui.CrawlerWorkerPulse{
			WorkerID:                   worker.WorkerID,
			ProcessLabel:               crawlerProcessLabel(worker.ProcessIdentifier),
			ActiveFetches:              int(worker.ActiveFetches),
			ActiveFetchesKnown:         worker.ActiveFetchesKnown,
			FetchLimit:                 capacity,
			FetchAdmissionWaiting:      int(worker.FetchAdmissionWaiting),
			FetchAdmissionWaitingKnown: worker.FetchAdmissionWaitingKnown,
			ActivePages:                activePages,
			WaitingPages:               waitingPages,
			ObservedAt:                 worker.ObservedAt,
		}
		if rate, found := rateByProcess[processIdentity{
			workerID:          worker.WorkerID,
			processIdentifier: worker.ProcessIdentifier,
		}]; found {
			row.FetchedPagesPerMinute = rate.FetchedPagesPerMinute
			row.IndexedPagesPerMinute = rate.IndexedPagesPerMinute
			row.RateKnown = rate.RateKnown
			if rate.ObservedAt.After(row.ObservedAt) {
				row.ObservedAt = rate.ObservedAt
			}
		}
		pulse.Workers = append(pulse.Workers, row)
	}
	if s.probe != nil {
		if depth, err := s.probe(ctx); err == nil {
			pulse.QueuePending = depth.Pending
			pulse.QueueKnown = true
		}
	}

	return pulse
}

func crawlerProcessLabel(processIdentifier string) string {
	if len(processIdentifier) > 8 {
		return processIdentifier[:8]
	}

	return processIdentifier
}
