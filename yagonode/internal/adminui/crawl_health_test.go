package adminui

import (
	"strings"
	"testing"
)

// TestCrawlHealthDerivesRates pins OPS-09: the monitor derives harvest, link-
// redundancy, and failure rates once the sample is large enough, and stays
// silent on tiny samples where percentages mislead.
func TestCrawlHealthDerivesRates(t *testing.T) {
	health := crawlHealth(CrawlMonitor{Totals: CrawlTotals{
		Fetched: 200, Indexed: 150, Duplicates: 40, Failed: 10,
	}})
	// LinkRedundancy = 40 / (40 duplicates + 200 fetched + 10 failed + 0 robots) = 16%.
	if health.HarvestRate != "75%" || health.LinkRedundancy != "16%" || health.FailRate != "5%" {
		t.Fatalf("rates = %+v", health)
	}

	small := crawlHealth(CrawlMonitor{Totals: CrawlTotals{Fetched: 10, Indexed: 1}})
	if small.HarvestRate != "" || small.LinkRedundancy != "" {
		t.Fatalf("small sample must hide rates: %+v", small)
	}
}

// TestLinkRedundancyStaysBounded is the regression guard for the >100% bug: a run
// that re-discovers a queued URL far more often than it fetches pages must report
// a bounded share, never the old unbounded duplicates/fetched ratio.
func TestLinkRedundancyStaysBounded(t *testing.T) {
	health := crawlHealth(CrawlMonitor{Totals: CrawlTotals{Fetched: 100, Duplicates: 9900}})
	if health.LinkRedundancy != "99%" {
		t.Fatalf("link redundancy = %q, want 99%% (bounded)", health.LinkRedundancy)
	}
}

// TestCrawlHealthFlagsSpiderTraps pins the corrected trap heuristics: a running
// run with a low harvest rate smells like a trap, one dominated by failures like
// a blocking host, while a densely-linked but healthy run — huge duplicate volume
// yet a high harvest rate — is NOT flagged (the false positive that produced the
// >100% "duplicates" alerts). Finished runs and small samples are left alone.
func TestCrawlHealthFlagsSpiderTraps(t *testing.T) {
	health := crawlHealth(CrawlMonitor{Runs: []CrawlRunView{
		{Profile: "trap-site", State: "running", Fetched: 400, Indexed: 20},
		{Profile: "walled-site", State: "running", Fetched: 300, Failed: 200},
		{Profile: "dense-site", State: "running", Fetched: 200, Indexed: 190, Duplicates: 40000},
		{Profile: "healthy", State: "running", Fetched: 500, Indexed: 450, Duplicates: 10},
		{Profile: "done-junk", State: "finished", Fetched: 400, Indexed: 0},
		{Profile: "tiny", State: "running", Fetched: 20, Indexed: 0},
	}})
	if len(health.Suspects) != 2 {
		t.Fatalf("suspects = %v", health.Suspects)
	}
	if !strings.Contains(health.Suspects[0], "trap-site") ||
		!strings.Contains(health.Suspects[0], "spider trap") {
		t.Fatalf("trap suspect = %q", health.Suspects[0])
	}
	if !strings.Contains(health.Suspects[1], "walled-site") ||
		!strings.Contains(health.Suspects[1], "blocking") {
		t.Fatalf("blocked suspect = %q", health.Suspects[1])
	}
	for _, suspect := range health.Suspects {
		if strings.Contains(suspect, "dense-site") {
			t.Fatalf("densely-linked healthy run must not be flagged: %q", suspect)
		}
	}
}

func TestPercentOfZeroWhole(t *testing.T) {
	if got := percentOf(5, 0); got != "0%" {
		t.Fatalf("percentOf(5,0) = %q", got)
	}
}
