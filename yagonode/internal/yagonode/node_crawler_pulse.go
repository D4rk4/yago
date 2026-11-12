package yagonode

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
)

type crawlerPulseSource struct {
	control crawlerWorkerActivitySource
	runs    crawlerRunThroughputSource
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
) adminui.CrawlerPulseSource {
	return crawlerPulseSource{control: control, runs: runs}
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
		if worker.ObservedAt.IsZero() || time.Since(worker.ObservedAt) >
			3*yagocrawlcontract.DefaultWorkerHeartbeatInterval {
			continue
		}
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
	if running, ok := s.runs.(interface{ Recent() []crawlruns.Run }); ok {
		for _, run := range running.Recent() {
			if run.State != yagocrawlcontract.CrawlRunRunning || run.Updated.IsZero() ||
				time.Since(run.Updated) >
					3*yagocrawlcontract.DefaultWorkerHeartbeatInterval {
				continue
			}
			pending := run.Tally.Pending
			maximum := uint64(^uint(0) >> 1)
			if pending > maximum-uint64(pulse.QueuePending) {
				pulse.QueuePending = int(maximum)
			} else {
				pulse.QueuePending += int(pending)
			}
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
