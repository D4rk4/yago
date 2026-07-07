package adminui

import "fmt"

// Crawl-health signals derived from the run tallies (Olston & Najork,
// "Web Crawling", FnTIR 2010): harvest rate says how much of the fetch effort
// became index entries, the duplicate rate is the classic spider-trap smell
// (a trap serves endless near-identical pages), and the failure rate exposes
// a stuck or blocked crawl. Rates render only once a run has fetched enough
// pages to make them meaningful.
const (
	crawlHealthMinFetched = 100
	crawlTrapDupShare     = 0.3
	crawlTrapFailShare    = 0.5
)

// CrawlHealth is the derived health rollup shown as monitor tiles.
type CrawlHealth struct {
	HarvestRate string
	DupRate     string
	FailRate    string
	Suspects    []string
}

// crawlHealth derives the rollup from a monitor snapshot. Empty strings hide
// the tiles until the sample is large enough.
func crawlHealth(monitor CrawlMonitor) CrawlHealth {
	health := CrawlHealth{}
	if monitor.Totals.Fetched >= crawlHealthMinFetched {
		health.HarvestRate = percentOf(monitor.Totals.Indexed, monitor.Totals.Fetched)
		health.DupRate = percentOf(monitor.Totals.Duplicates, monitor.Totals.Fetched)
		health.FailRate = percentOf(monitor.Totals.Failed, monitor.Totals.Fetched)
	}
	for _, run := range monitor.Runs {
		if reason := trapSuspicion(run); reason != "" {
			health.Suspects = append(health.Suspects, reason)
		}
	}

	return health
}

// trapSuspicion flags one run whose tally looks like a spider trap or a
// blocked host, with the profile name so the operator knows what to steer.
func trapSuspicion(run CrawlRunView) string {
	if run.State != "running" || run.Fetched < crawlHealthMinFetched {
		return ""
	}
	fetched := float64(run.Fetched)
	if float64(run.Duplicates)/fetched > crawlTrapDupShare {
		return fmt.Sprintf(
			"%s: %s of fetched pages are duplicates — possible spider trap",
			run.Profile, percentOf(run.Duplicates, run.Fetched),
		)
	}
	if float64(run.Failed)/fetched > crawlTrapFailShare {
		return fmt.Sprintf(
			"%s: %s of fetches fail — host may be blocking or down",
			run.Profile, percentOf(run.Failed, run.Fetched),
		)
	}

	return ""
}

func percentOf(part, whole uint64) string {
	if whole == 0 {
		return "0%"
	}

	return fmt.Sprintf("%.0f%%", 100*float64(part)/float64(whole))
}
