package adminui

import (
	"context"
	"html/template"
)

// CrawlRunView is one row of the crawl monitor: a run the node currently knows
// about, with its per-outcome tally and how long it has been running.
type CrawlRunView struct {
	RunID        string
	Profile      string
	Worker       string
	State        string
	Fetched      uint64
	Indexed      uint64
	Failed       uint64
	RobotsDenied uint64
	Duplicates   uint64
	Pending      uint64
	Runtime      string
	// PagesPerMinute is the last operator-applied fetch-rate cap (0 = none), used
	// to pre-fill the monitor's rate control.
	PagesPerMinute uint32
}

// CrawlTotals is the crawl results/rejections rollup across the known runs: the
// pages that produced a result (fetched, indexed) and those that were rejected
// (failed, denied by robots, or skipped as duplicates).
type CrawlTotals struct {
	Fetched      uint64
	Indexed      uint64
	Failed       uint64
	RobotsDenied uint64
	Duplicates   uint64
}

// CrawlMonitor is the crawl monitor snapshot: the known runs newest-first, the
// results/rejections rollup across them, and the broker's outstanding order
// backlog.
type CrawlMonitor struct {
	Runs         []CrawlRunView
	Totals       CrawlTotals
	QueuePending int
	QueueLeased  int
}

// CrawlMonitorSource supplies the crawl monitor snapshot on each request. A nil
// provider hides the monitor from the Crawler section.
type CrawlMonitorSource interface {
	Monitor(ctx context.Context) CrawlMonitor
}

// CrawlControlRequest is an operator's steer of one crawl run, keyed by the run's
// identifier as shown in the monitor. PagesPerMinute carries the target rate for a
// set-rate action (zero lifts the throttle) and is ignored by other actions.
type CrawlControlRequest struct {
	RunID          string
	Action         string
	PagesPerMinute uint32
}

// CrawlControlSource steers a running crawl on the operator's behalf. A nil
// provider leaves the monitor read-only.
type CrawlControlSource interface {
	Control(ctx context.Context, req CrawlControlRequest) error
}

var crawlFuncs = template.FuncMap{"stateTag": crawlStateTag}

// crawlStateTag maps a crawl run state to an existing tag modifier, leaving a
// finished or unknown state on the neutral base tag.
func crawlStateTag(state string) string {
	switch state {
	case "running":
		return "info"
	case "cancelled":
		return "warn"
	default:
		return ""
	}
}
