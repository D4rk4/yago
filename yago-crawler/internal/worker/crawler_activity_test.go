package worker

import (
	"fmt"
	"testing"
	"time"
)

func TestCrawlerActivityWindowSeparatesFetchAndIngestOutcomes(t *testing.T) {
	startedAt := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	activity := newCrawlerActivity(startedAt)
	activity.recordHostAdmission(crawlerPageRequest, "A.Example", 100*time.Millisecond, true)
	activity.recordHTTPRequestCompleted(crawlerPageRequest, "a.example", 50*time.Millisecond, 200, false)
	activity.recordHostAdmission(crawlerPageRequest, "b.example", 200*time.Millisecond, true)
	activity.recordHTTPRequestCompleted(crawlerPageRequest, "b.example", 400*time.Millisecond, 503, true)
	activity.recordHostAdmission(crawlerRobotsRequest, "a.example", 20*time.Millisecond, false)
	activity.recordIngestSend(false)
	activity.recordIngestSend(true)
	activity.recordNodeAcknowledgment(250*time.Millisecond, 0, false)
	activity.recordNodeAcknowledgment(300*time.Millisecond, 0, true)
	activity.recordNodeAcknowledgment(400*time.Millisecond, 13, false)

	window := activity.takeWindow(startedAt.Add(time.Minute))
	if window.Elapsed != time.Minute || window.Page.started != 2 || window.Page.completed != 2 ||
		window.Page.failed != 1 || window.Page.responses2xx != 1 || window.Page.responses5xx != 1 {
		t.Fatalf("page fetch window = %#v", window.Page)
	}
	if window.Robots.started != 0 || window.Robots.admissionFailures != 1 {
		t.Fatalf("robots fetch window = %#v", window.Robots)
	}
	if window.IngestMutationsSent != 1 || window.IngestSendFailures != 1 ||
		window.NodeAcksAccepted != 1 || window.NodeAcksRejected != 1 || window.NodeAckFailures != 1 {
		t.Fatalf("ingest window = %#v", window)
	}
	if len(window.Hosts) != 2 || window.Hosts[0].Hostname != "a.example" ||
		window.Hosts[0].PageRequestsStarted != 1 || window.Hosts[0].RobotsRequestsStarted != 0 ||
		window.Hosts[0].AdmissionWaitTotalMs != 120 {
		t.Fatalf("host summaries = %#v", window.Hosts)
	}
	if pagesPerMinute(10, 70, time.Minute) != 60 || pagesPerMinute(70, 10, time.Minute) != 0 ||
		pagesPerMinute(10, 70, 0) != 0 || crawlerActivityRatePerMinute(40, time.Minute) != 40 ||
		crawlerActivityRatePerMinute(40, 0) != 0 {
		t.Fatal("pages-per-minute calculation did not handle normal and reset windows")
	}
	if signedDifference(8, 5) != 3 || signedDifference(5, 8) != -3 {
		t.Fatal("pending ingest delta did not preserve its direction")
	}

	emptyWindow := activity.takeWindow(startedAt.Add(2 * time.Minute))
	if emptyWindow.Page.started != 0 || emptyWindow.NodeAcksAccepted != 0 || emptyWindow.Elapsed != time.Minute {
		t.Fatalf("activity counters were not reset: %#v", emptyWindow)
	}
}

func TestCrawlerActivityWindowBoundsHostRowsAndReportsOmissions(t *testing.T) {
	startedAt := time.Now()
	activity := newCrawlerActivity(startedAt)
	for index := range maximumCrawlerActivityHosts + 2 {
		hostname := fmt.Sprintf("host-%02d.example", index)
		activity.recordHostAdmission(crawlerPageRequest, hostname, 0, true)
	}

	window := activity.takeWindow(startedAt.Add(time.Minute))
	if len(window.Hosts) != maximumCrawlerActivityHostsLogged {
		t.Fatalf("logged host rows = %d, want %d", len(window.Hosts), maximumCrawlerActivityHostsLogged)
	}
	if window.UntrackedHostRequests != 2 ||
		window.OmittedHostSummaries != maximumCrawlerActivityHosts-maximumCrawlerActivityHostsLogged {
		t.Fatalf("omitted host counts = untracked:%d omitted:%d", window.UntrackedHostRequests, window.OmittedHostSummaries)
	}
}
