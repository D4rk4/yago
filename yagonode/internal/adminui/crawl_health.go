package adminui

import "fmt"

const (
	crawlHealthMinFetched = 100
	crawlHealthMinOutcome = 100
	crawlTrapHarvestFloor = 0.2
	crawlTrapFailShare    = 0.5
	crawlHostFailureAlert = "%s: %s of fetched-or-failed page outcomes failed — " +
		"inspect crawler logs; repeated host availability failures retire that host automatically"
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
	}
	if crawlPopulationAtLeast(
		crawlHealthMinOutcome,
		monitor.Totals.Fetched,
		monitor.Totals.Failed,
	) {
		health.FailRate = crawlPopulationPercent(
			monitor.Totals.Failed,
			monitor.Totals.Fetched,
			monitor.Totals.Failed,
		)
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
	if run.State != "running" {
		return ""
	}
	if crawlPopulationAtLeast(crawlHealthMinOutcome, run.Fetched, run.Failed) &&
		crawlPopulationShare(run.Failed, run.Fetched, run.Failed) > crawlTrapFailShare {
		return fmt.Sprintf(
			crawlHostFailureAlert,
			run.Profile, crawlPopulationPercent(run.Failed, run.Fetched, run.Failed),
		)
	}
	if run.Fetched < crawlHealthMinFetched {
		return ""
	}
	fetched := float64(run.Fetched)
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
	return crawlPopulationPercent(
		totals.Duplicates,
		totals.Duplicates,
		totals.Fetched,
		totals.Failed,
		totals.RobotsDenied,
	)
}

func percentOf(part, whole uint64) string {
	if whole == 0 {
		return "0%"
	}
	if part > whole {
		return "Unavailable"
	}
	if part == whole {
		return "100%"
	}

	return crawlPopulationPercent(part, whole)
}
