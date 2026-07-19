package main

import (
	"context"

	"github.com/D4rk4/yago/yago-crawler/internal/crawldenylist"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlorder"
	"github.com/D4rk4/yago/yago-crawler/internal/fetchrate"
	"github.com/D4rk4/yago/yago-crawler/internal/fleetfetchstart"
	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/runtally"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type crawlerExecutionStart struct {
	context          context.Context
	config           ServiceConfig
	source           pagefetch.PageSource
	metrics          *crawlermetrics.Metrics
	checkpoint       crawlerCheckpointSession
	nodeRPC          crawlerNodeRPC
	emitter          ingest.BatchEmitter
	growthAdmission  *yagocrawlcontract.StoragePressureGate
	restart          func()
	shutdown         func()
	maximumRedirects *redirectLimit
}

func assembleCrawlerExecution(start crawlerExecutionStart) (crawlerExecution, error) {
	crawl := start.config.Crawl
	pace, err := assembleCrawlerPace(
		start.context,
		crawl,
		start.checkpoint.checkpoint,
		start.metrics,
	)
	if err != nil {
		return crawlerExecution{}, err
	}
	tally := runtally.New()
	urlDenylist := crawldenylist.New()
	crawlFrontier := newCrawlFrontier(crawl, crawlFrontierState{
		pace:            pace,
		tally:           tally,
		checkpoint:      start.checkpoint.checkpoint,
		shutdown:        start.shutdown,
		growthAdmission: start.growthAdmission,
		urlDenylist:     urlDenylist,
	})
	workerConcurrency := newWorkerConcurrency(crawl.Workers)
	fetchBudget := fetchrate.NewProcessBudget(crawl.ProcessPagesPerSecond)
	fetchStartSession := fleetfetchstart.NewSessionGate()
	fleetAdmission := fleetfetchstart.NewAdmission(fleetfetchstart.AdmissionConfig{
		Client:          start.nodeRPC.control,
		WorkerID:        start.config.WorkerID,
		WorkerSessionID: start.checkpoint.workerSessionID,
		Session:         fetchStartSession,
		PagesPerSecond:  fetchBudget.PagesPerSecond,
		PermitCapacity:  workerConcurrency.Current,
		UpstreamDemand:  func() int { return fetchBudget.Waiting() + 1 },
	})
	activeRuns := crawlorder.NewActiveRunAdmission(crawl.MaxActiveRuns)
	execution := crawlerExecution{
		checkpoint:      start.checkpoint,
		nodeRPC:         start.nodeRPC,
		pace:            pace,
		tally:           tally,
		frontier:        crawlFrontier,
		concurrency:     workerConcurrency,
		activeRuns:      activeRuns,
		fetchBudget:     fetchBudget,
		fleetAdmission:  fleetAdmission,
		emitter:         start.emitter,
		growthAdmission: start.growthAdmission,
		urlDenylist:     urlDenylist,
	}
	execution.orders = assembleCrawlerOrderReceiver(start, execution, fetchStartSession)

	return execution, nil
}

func assembleCrawlerOrderReceiver(
	start crawlerExecutionStart,
	execution crawlerExecution,
	fetchStartSession *fleetfetchstart.SessionGate,
) *crawlorder.GRPCOrderReceiver {
	maximumRedirects := maximumRedirectsControl{
		limit:  start.maximumRedirects,
		source: start.source,
	}
	runtimePolicyChanges := newCrawlerRuntimePolicyChange(
		start.config.runtimePolicy(),
		start.source,
		start.restart,
	)
	control := assembleCrawlerControlHandler(crawlerControlActions{
		restart:             start.restart,
		concurrency:         execution.concurrency,
		setProcessRate:      execution.fetchBudget.Set,
		setMaximumRedirects: maximumRedirects.Apply,
		resizeActiveRuns:    execution.activeRuns.Resize,
		frontier:            execution.frontier,
	})
	return crawlorder.NewGRPCOrderReceiver(
		start.context,
		start.nodeRPC.control,
		start.config.WorkerID,
		control,
		crawlorder.WithHeartbeatActiveFetches(start.metrics.ActiveFetchWorkerJobs),
		crawlorder.WithHeartbeatStoragePressure(
			start.growthAdmission.Snapshot,
			start.growthAdmission.SetPolicy,
		),
		crawlorder.WithHeartbeatRuntimePolicy(
			runtimePolicyChanges.Current,
			runtimePolicyChanges.Apply,
		),
		crawlorder.WithTerminalSettlementOutbox(start.checkpoint.checkpoint),
		crawlorder.WithURLDenylist(execution.urlDenylist),
		crawlorder.WithWorkerLeaseSession(
			start.checkpoint.workerSessionID,
			start.checkpoint.leaseGrants,
		),
		crawlorder.WithFetchStartSession(fetchStartSession),
	)
}
