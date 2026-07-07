package adminui

import (
	"strings"
	"testing"
)

// TestCrawlHealthDerivesRates pins OPS-09: the monitor derives harvest,
// duplicate, and failure rates once the sample is large enough, and stays
// silent on tiny samples where percentages mislead.
func TestCrawlHealthDerivesRates(t *testing.T) {
	health := crawlHealth(CrawlMonitor{Totals: CrawlTotals{
		Fetched: 200, Indexed: 150, Duplicates: 20, Failed: 10,
	}})
	if health.HarvestRate != "75%" || health.DupRate != "10%" || health.FailRate != "5%" {
		t.Fatalf("rates = %+v", health)
	}

	small := crawlHealth(CrawlMonitor{Totals: CrawlTotals{Fetched: 10, Indexed: 1}})
	if small.HarvestRate != "" || small.DupRate != "" {
		t.Fatalf("small sample must hide rates: %+v", small)
	}
}

// TestCrawlHealthFlagsSpiderTraps pins the trap heuristics: a running run
// dominated by duplicates smells like a trap, one dominated by failures like a
// blocking host; finished runs and small samples are left alone.
func TestCrawlHealthFlagsSpiderTraps(t *testing.T) {
	health := crawlHealth(CrawlMonitor{Runs: []CrawlRunView{
		{Profile: "loop-site", State: "running", Fetched: 400, Duplicates: 200},
		{Profile: "walled-site", State: "running", Fetched: 300, Failed: 200},
		{Profile: "healthy", State: "running", Fetched: 500, Indexed: 450, Duplicates: 10},
		{Profile: "done-dups", State: "finished", Fetched: 400, Duplicates: 300},
		{Profile: "tiny", State: "running", Fetched: 20, Duplicates: 19},
	}})
	if len(health.Suspects) != 2 {
		t.Fatalf("suspects = %v", health.Suspects)
	}
	if !strings.Contains(health.Suspects[0], "loop-site") ||
		!strings.Contains(health.Suspects[0], "spider trap") {
		t.Fatalf("trap suspect = %q", health.Suspects[0])
	}
	if !strings.Contains(health.Suspects[1], "walled-site") ||
		!strings.Contains(health.Suspects[1], "blocking") {
		t.Fatalf("blocked suspect = %q", health.Suspects[1])
	}
}

func TestPercentOfZeroWhole(t *testing.T) {
	if got := percentOf(5, 0); got != "0%" {
		t.Fatalf("percentOf(5,0) = %q", got)
	}
}
