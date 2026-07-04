package adminui

import (
	"context"
	"html/template"
)

// CrawlRunView is one row of the crawl monitor: a run the node currently knows
// about, with its per-outcome tally and how long it has been running.
type CrawlRunView struct {
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
}

// CrawlMonitor is the crawl monitor snapshot: the known runs newest-first plus the
// broker's outstanding order backlog.
type CrawlMonitor struct {
	Runs         []CrawlRunView
	QueuePending int
	QueueLeased  int
}

// CrawlMonitorSource supplies the crawl monitor snapshot on each request. A nil
// provider hides the monitor from the Crawler section.
type CrawlMonitorSource interface {
	Monitor(ctx context.Context) CrawlMonitor
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
