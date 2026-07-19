package yagonode

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
)

type crawlRunDetailSource struct {
	runs *crawlruns.Registry
	now  func() time.Time
}

func newCrawlRunDetailSource(runs *crawlruns.Registry) adminui.CrawlRunDetailSource {
	if runs == nil {
		return nil
	}

	return crawlRunDetailSource{runs: runs, now: time.Now}
}

func (source crawlRunDetailSource) CrawlRunDetail(
	_ context.Context,
	runID string,
) (adminui.CrawlRunDetail, error) {
	run, found := source.runs.Run(runID)
	if !found {
		return adminui.CrawlRunDetail{}, fmt.Errorf("no crawl run %q", runID)
	}
	outcomes := run.RecentOutcomes.NewestFirst()
	views := make([]adminui.CrawlURLOutcomeView, 0, len(outcomes))
	for _, outcome := range outcomes {
		status := "Unavailable"
		if outcome.HTTPStatus > 0 {
			status = strconv.FormatUint(uint64(outcome.HTTPStatus), 10)
		}
		views = append(views, adminui.CrawlURLOutcomeView{
			URL: outcome.URL, Class: string(outcome.Class),
			ObservedAt: outcome.ObservedAt.UTC().Format(time.RFC3339),
			HTTPStatus: status, Reason: outcome.Reason,
		})
	}

	return adminui.CrawlRunDetail{
		Run:      crawlRunView(run, source.now()),
		Outcomes: views,
	}, nil
}
