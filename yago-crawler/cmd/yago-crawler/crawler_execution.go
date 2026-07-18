package main

import (
	"context"

	"github.com/D4rk4/yago/yago-crawler/internal/crawldelay"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlorder"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
	"github.com/D4rk4/yago/yago-crawler/internal/runtally"
	"github.com/D4rk4/yago/yagoegress"
)

type crawlerExecution struct {
	checkpoint      crawlerCheckpointSession
	nodeRPC         crawlerNodeRPC
	pace            *crawldelay.AdaptivePace
	tally           *runtally.Tally
	frontier        *frontier.Frontier
	orders          *crawlorder.GRPCOrderReceiver
	concurrency     *workerConcurrency
	activeRuns      *crawlorder.ActiveRunAdmission
	emitter         ingest.BatchEmitter
	growthAdmission interface {
		WaitForGrowth(context.Context) bool
	}
}

func (execution crawlerExecution) lifecycle(
	cfg ServiceConfig,
	source pagefetch.PageSource,
	metrics *crawlermetrics.Metrics,
) (crawlerLifecycle, error) {
	guard := yagoegress.NewGuard(
		cfg.EgressAllowLAN,
		yagoegress.WithPrivateAllowlist(cfg.EgressAllowedCIDRs),
	)
	client := newGuardedEgressClient(guard, cfg.Crawl)
	chains, err := buildFetchChains(guard, client, cfg.Crawl, source, metrics)
	if err != nil {
		return crawlerLifecycle{}, err
	}
	worker := pipeline.NewPipeline(
		execution.frontier,
		chains.verifying,
		pageindex.NewIndexBuilder(),
		execution.emitter,
		pipeline.WithObserver(metrics),
		pipeline.WithInsecureFetcher(chains.insecure),
		pipeline.WithRobotsIgnoringFetchers(chains.verifyingDirect, chains.insecureDirect),
		pipeline.WithHostLoadFeedback(execution.pace),
		pipeline.WithLeaseGrants(execution.checkpoint.leaseGrants),
	)
	progress := crawlorder.NewGRPCProgressReporter(
		execution.nodeRPC.control,
		cfg.WorkerID,
		crawlorder.WithProgressLeaseSession(
			execution.checkpoint.workerSessionID,
			execution.checkpoint.leaseGrants,
		),
	)
	consumer := crawlorder.NewCrawlOrderConsumer(
		execution.orders,
		execution.frontier,
		newCrawlRequestExpander(client, cfg.Crawl, guard),
	).WithProgressReporter(progress).
		WithRunTally(execution.tally).
		WithGrowthAdmission(execution.growthAdmission).
		WithActiveRunAdmission(execution.activeRuns)

	return crawlerLifecycle{
		worker:      worker,
		consumer:    consumer,
		progress:    progress,
		concurrency: execution.concurrency,
	}, nil
}
