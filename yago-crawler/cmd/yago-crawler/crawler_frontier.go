package main

import (
	"github.com/D4rk4/yago/yago-crawler/internal/crawldenylist"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
)

type crawlFrontierState struct {
	pace            frontier.CrawlPace
	tally           frontier.RunTally
	checkpoint      frontier.Checkpoint
	shutdown        func()
	growthAdmission frontier.GrowthAdmission
	stateGrowth     frontier.StateGrowthAdmission
	urlDenylist     *crawldenylist.Denylist
}

func newCrawlFrontier(crawl CrawlConfig, state crawlFrontierState) *frontier.Frontier {
	return frontier.NewFrontier(
		crawl.JobQueueSize,
		state.pace,
		frontier.WithMaxHostConcurrency(crawl.MaxHostConcurrency),
		frontier.WithMaxPagesPerRun(crawl.MaxPagesPerRun),
		frontier.WithDefaultRunRate(crawl.RunPagesPerMinute),
		frontier.WithRunTally(state.tally),
		frontier.WithAutomaticDiscoveryPriority(crawl.PrioritizeAutomaticDiscovery),
		frontier.WithCheckpoint(state.checkpoint),
		frontier.WithCheckpointFailureShutdown(state.shutdown),
		frontier.WithGrowthAdmission(state.growthAdmission),
		frontier.WithStateGrowthAdmission(state.stateGrowth),
		frontier.WithURLDenylist(state.urlDenylist),
	)
}
