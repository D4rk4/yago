package adminui

import (
	"sort"
	"time"
)

const maximumActiveCrawlURLsPerRun = 8

func attachCrawlerRunActivity(runs []CrawlRunView, pulse CrawlerPulse, now time.Time) {
	workers := make(map[string]CrawlerWorkerPulse, len(pulse.Workers))
	for _, worker := range pulse.Workers {
		workers[worker.WorkerID] = worker
	}
	for index := range runs {
		attachRunURLActivity(&runs[index], workers, now)
	}
}

func attachRunURLActivity(
	run *CrawlRunView,
	workers map[string]CrawlerWorkerPulse,
	now time.Time,
) {
	worker, found := workers[run.Worker]
	if !found || !worker.ActiveFetchesKnown || worker.ObservedAt.IsZero() {
		return
	}
	run.URLActivityKnown = true
	appendActiveRunURLs(run, worker.ActivePages)
	appendWaitingRunURLs(run, worker.WaitingPages, now)
	sort.SliceStable(run.URLActivity, func(left, right int) bool {
		if run.URLActivity[left].Kind != run.URLActivity[right].Kind {
			return run.URLActivity[left].Kind == "fetching"
		}
		return run.URLActivity[left].URL < run.URLActivity[right].URL
	})
}

func appendActiveRunURLs(run *CrawlRunView, pages []CrawlerActivePage) {
	activeURLs := 0
	for _, page := range pages {
		if page.RunID != run.RunID {
			continue
		}
		if activeURLs == maximumActiveCrawlURLsPerRun {
			run.URLActivityMore++
			continue
		}
		activeURLs++
		run.URLActivity = append(run.URLActivity, CrawlRunURLActivity{
			URL:  page.URL,
			Kind: "fetching",
			Detail: crawlerPageStageLabel(
				page.Stage,
			) + " · " + pulseElapsed(
				page.ElapsedMilliseconds,
			),
		})
	}
}

func appendWaitingRunURLs(run *CrawlRunView, pages []CrawlerWaitingPage, now time.Time) {
	for _, page := range pages {
		if page.RunID != run.RunID {
			continue
		}
		run.URLActivity = append(run.URLActivity, CrawlRunURLActivity{
			URL:          page.URL,
			Kind:         page.State,
			Detail:       page.Reason,
			Availability: waitingPageAvailability(page, now),
		})
	}
}

func pulseElapsed(milliseconds uint64) string {
	return time.Duration(milliseconds * uint64(time.Millisecond)).Round(time.Second).String()
}

func waitingPageAvailability(page CrawlerWaitingPage, now time.Time) string {
	if page.AvailableAt.IsZero() {
		return "when a host slot opens"
	}
	remaining := page.AvailableAt.Sub(now)
	if remaining <= 0 {
		return "eligible now"
	}
	if remaining < time.Second {
		return "in under 1s"
	}
	seconds := (remaining + time.Second - 1) / time.Second
	return "in " + (seconds * time.Second).String()
}
