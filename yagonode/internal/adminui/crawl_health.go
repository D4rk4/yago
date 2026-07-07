package adminui

import "fmt"

// Crawl-health signals derived from the run tallies (Olston & Najork,
// "Web Crawling", FnTIR 2010): the harvest rate says how much of the fetch effort
// became index entries — a spider trap luring the crawler into endless
// near-identical or junk pages fetches a lot but indexes little, so a *low
// harvest rate* is the trap smell — and the failure rate exposes a stuck or
// blocked host. Raw duplicate volume is deliberately not a trap signal: the
// frontier counts a duplicate every time a discovered link re-points at an
// already-queued URL, which any densely intra-linked site does many times per
// fetched page, so that count is unbounded against fetched pages and says nothing
// about a trap. It is surfaced only as a bounded "link redundancy" share of all
// links the frontier resolved. Rates render once a run has fetched enough pages
// to make them meaningful.
const (
	crawlHealthMinFetched = 100
	crawlTrapHarvestFloor = 0.2
	crawlTrapFailShare    = 0.5
)

// CrawlHealth is the derived health rollup shown as monitor tiles.
type CrawlHealth struct {
	HarvestRate    string
	LinkRedundancy string
	FailRate       string
	Suspects       []string
}

// crawlHealth derives the rollup from a monitor snapshot. Empty strings hide
// the tiles until the sample is large enough.
func crawlHealth(monitor CrawlMonitor) CrawlHealth {
	health := CrawlHealth{}
	if monitor.Totals.Fetched >= crawlHealthMinFetched {
		health.HarvestRate = percentOf(monitor.Totals.Indexed, monitor.Totals.Fetched)
		health.LinkRedundancy = linkRedundancy(monitor.Totals)
		health.FailRate = percentOf(monitor.Totals.Failed, monitor.Totals.Fetched)
	}
	for _, run := range monitor.Runs {
		if reason := trapSuspicion(run); reason != "" {
			health.Suspects = append(health.Suspects, reason)
		}
	}

	return health
}

// trapSuspicion flags one running run whose tally smells like a blocked host
// (most fetches failing) or a spider trap (a low harvest rate: much fetched,
// little indexed), naming the profile so the operator knows what to steer. The
// failure check comes first because it is the more specific diagnosis for a run
// that both fails and indexes little.
func trapSuspicion(run CrawlRunView) string {
	if run.State != "running" || run.Fetched < crawlHealthMinFetched {
		return ""
	}
	fetched := float64(run.Fetched)
	if float64(run.Failed)/fetched > crawlTrapFailShare {
		return fmt.Sprintf(
			"%s: %s of fetches fail — host may be blocking or down",
			run.Profile, percentOf(run.Failed, run.Fetched),
		)
	}
	if float64(run.Indexed)/fetched < crawlTrapHarvestFloor {
		return fmt.Sprintf(
			"%s: only %s of fetched pages produced an index entry — possible spider trap or junk pages",
			run.Profile,
			percentOf(run.Indexed, run.Fetched),
		)
	}

	return ""
}

// linkRedundancy expresses how many discovered links merely re-pointed at an
// already-queued URL as a share of all links the frontier resolved (duplicates
// plus every fetched, failed, and robots-denied page), keeping it within
// [0,100%) instead of the unbounded duplicates/fetched ratio.
func linkRedundancy(totals CrawlTotals) string {
	seen := totals.Duplicates + totals.Fetched + totals.Failed + totals.RobotsDenied

	return percentOf(totals.Duplicates, seen)
}

func percentOf(part, whole uint64) string {
	if whole == 0 {
		return "0%"
	}

	return fmt.Sprintf("%.0f%%", 100*float64(part)/float64(whole))
}
