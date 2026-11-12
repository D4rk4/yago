package worker

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type waitingPageReporter struct {
	mutex        sync.Mutex
	activeRunIDs map[string]struct{}
	nextRun      int
}

func newWaitingPageReporter() *waitingPageReporter {
	return &waitingPageReporter{activeRunIDs: make(map[string]struct{})}
}

func (reporter *waitingPageReporter) runStarted(runID string) {
	reporter.mutex.Lock()
	reporter.activeRunIDs[runID] = struct{}{}
	reporter.mutex.Unlock()
}

func (reporter *waitingPageReporter) runFinished(runID string) {
	reporter.mutex.Lock()
	delete(reporter.activeRunIDs, runID)
	if len(reporter.activeRunIDs) == 0 {
		reporter.nextRun = 0
	}
	reporter.mutex.Unlock()
}

func (reporter *waitingPageReporter) snapshot(
	ctx context.Context,
	workFrontier *frontier.Frontier,
) ([]*crawlrpc.WaitingCrawlPage, error) {
	maximumRuns := yagocrawlcontract.MaximumCrawlerWaitingPageReports /
		yagocrawlcontract.MaximumCrawlerWaitingPagesPerRun
	runIDs := reporter.nextRunIDs(maximumRuns)
	if len(runIDs) == 0 {
		return nil, nil
	}
	waiting, err := workFrontier.WaitingSample(
		ctx,
		runIDs,
		yagocrawlcontract.MaximumCrawlerWaitingPagesPerRun,
		yagocrawlcontract.MaximumCrawlerWaitingPageReports,
		time.Now(),
	)
	if err != nil {
		return nil, err
	}
	statuses := make([]*crawlrpc.WaitingCrawlPage, 0, len(waiting))
	for _, page := range waiting {
		status := &crawlrpc.WaitingCrawlPage{
			RunId:  page.Page.RunID,
			Url:    page.Page.URL,
			State:  string(page.Page.State),
			Reason: page.Reason,
		}
		if !page.AvailableAt.IsZero() {
			status.AvailableAfterUnixMilliseconds = page.AvailableAt.UnixMilli()
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}

func (reporter *waitingPageReporter) nextRunIDs(maximum int) []string {
	reporter.mutex.Lock()
	defer reporter.mutex.Unlock()
	if len(reporter.activeRunIDs) == 0 || maximum <= 0 {
		return nil
	}
	runIDs := make([]string, 0, len(reporter.activeRunIDs))
	for runID := range reporter.activeRunIDs {
		runIDs = append(runIDs, runID)
	}
	sort.Strings(runIDs)
	start := reporter.nextRun % len(runIDs)
	count := min(maximum, len(runIDs)-start)
	selected := make([]string, 0, count)
	for offset := 0; offset < count; offset++ {
		selected = append(selected, runIDs[start+offset])
	}
	reporter.nextRun = start + count
	if reporter.nextRun == len(runIDs) {
		reporter.nextRun = 0
	}

	return selected
}
