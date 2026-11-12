package adminui

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildCrawlRunSectionsKeepsEveryActiveRunAndLimitsRecentHistory(t *testing.T) {
	t.Parallel()

	runs := []CrawlRunView{
		{RunID: "active-1", State: "running"},
		{RunID: "finished-1", State: "finished"},
		{RunID: "cancelled-1", State: "cancelled"},
	}
	for index := 0; index < recentCrawlHistoryLimit+3; index++ {
		runs = append(runs, CrawlRunView{
			RunID: fmt.Sprintf("finished-%02d", index+2), State: "finished",
		})
	}
	runs = append(runs, CrawlRunView{RunID: "unknown", State: "unexpected"})

	sections := buildCrawlRunSections(runs)
	if len(sections.Active) != 1 || sections.Active[0].RunID != "active-1" {
		t.Fatalf("active runs = %+v", sections.Active)
	}
	if len(sections.Recent) != recentCrawlHistoryLimit {
		t.Fatalf("recent runs = %d, want %d", len(sections.Recent), recentCrawlHistoryLimit)
	}
	if sections.Recent[0].RunID != "finished-1" || sections.Recent[1].RunID != "cancelled-1" ||
		sections.Recent[recentCrawlHistoryLimit-1].RunID != "finished-09" {
		t.Fatalf("recent run order and bounds = %+v", sections.Recent)
	}
}

func TestBuildCrawlRunSectionsKeepsEmptySectionsEmpty(t *testing.T) {
	t.Parallel()

	sections := buildCrawlRunSections([]CrawlRunView{{RunID: "unknown", State: "unexpected"}})
	if len(sections.Active) != 0 || len(sections.Recent) != 0 {
		t.Fatalf("sections = %+v, want empty for unknown states", sections)
	}
}

func TestConsoleCrawlMonitorReplacesWideHistoryTable(t *testing.T) {
	t.Parallel()

	runs := []CrawlRunView{
		{RunID: "active-a", Profile: "Active A", State: "running", Pending: 8},
		{RunID: "active-b", Profile: "Active B", State: "running", Pending: 3},
	}
	for index := 0; index < recentCrawlHistoryLimit+2; index++ {
		runs = append(runs, CrawlRunView{
			RunID:   fmt.Sprintf("finished-%02d", index),
			Profile: fmt.Sprintf("Finished %02d", index),
			State:   "finished",
		})
	}
	console := New(Options{
		Monitor: fakeMonitor{snap: CrawlMonitor{Runs: runs}},
		Control: &fakeControl{},
	})
	got := do(t, console, "/admin/crawl/monitor")
	for _, want := range []string{
		"Active A", "Active B", "Finished 00", "Finished 09",
		"Recent crawl history (up to 10)", "name=\"action\" value=\"pause\"",
		"name=\"action\" value=\"restart\"",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("monitor missing %q", want)
		}
	}
	for _, absent := range []string{"Finished 10", "Finished 11", "<table", "Page 1 of"} {
		if strings.Contains(got.body, absent) {
			t.Fatalf("monitor unexpectedly contains %q", absent)
		}
	}
}
