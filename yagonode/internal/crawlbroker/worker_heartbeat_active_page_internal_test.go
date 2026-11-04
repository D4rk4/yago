package crawlbroker

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestActiveCrawlerPagesFromHeartbeatValidatesAndCopiesPageStatus(t *testing.T) {
	input := []*crawlrpc.ActiveCrawlPage{{
		Url:                 "https://example.org/page",
		Stage:               "http_fetch",
		ElapsedMilliseconds: 3200,
	}}
	got, valid := activeCrawlerPagesFromHeartbeat(input)
	if !valid || len(got) != 1 || got[0].URL != input[0].GetUrl() ||
		got[0].Stage != input[0].GetStage() || got[0].ElapsedMilliseconds != 3200 {
		t.Fatalf("active pages = %#v, valid %t", got, valid)
	}
	input[0].Url = "https://changed.example/"
	if got[0].URL != "https://example.org/page" {
		t.Fatalf("active page snapshot aliases heartbeat input: %#v", got[0])
	}
}

func TestActiveCrawlerPagesFromHeartbeatRejectsInvalidStatus(t *testing.T) {
	cases := [][]*crawlrpc.ActiveCrawlPage{
		{{Url: "https://example.org/page", Stage: "unknown"}},
		{{Url: "https://user:password@example.org/page", Stage: "http_fetch"}},
		{{Url: "https://example.org/page#fragment", Stage: "http_fetch"}},
		{{Url: "https://example.org/page", Stage: "http_fetch"}, nil},
	}
	for index, pages := range cases {
		if _, valid := activeCrawlerPagesFromHeartbeat(pages); valid {
			t.Fatalf("case %d accepted invalid active pages", index)
		}
	}
	tooMany := make([]*crawlrpc.ActiveCrawlPage, yagocrawlcontract.MaximumFetchWorkerConcurrency+1)
	for index := range tooMany {
		tooMany[index] = &crawlrpc.ActiveCrawlPage{
			Url:   "https://example.org/page",
			Stage: "http_fetch",
		}
	}
	if _, valid := activeCrawlerPagesFromHeartbeat(tooMany); valid {
		t.Fatal("accepted more active pages than the worker concurrency ceiling")
	}
}

func TestRecordHeartbeatActivePagesAppearsOnlyInCurrentWorkerSnapshot(t *testing.T) {
	registry := newControlRegistry()
	registry.register("worker-a", "session-a")
	active := uint32(1)
	registry.recordHeartbeatFetchActivity("worker-a", "session-a", &active, nil)
	registry.recordHeartbeatActivePages("worker-a", "session-a", []CrawlerActivePage{{
		URL:   "https://example.org/page",
		Stage: "node_ingest",
	}})
	waiting := []CrawlerWaitingPage{{
		RunID: "run-a", URL: "https://example.org/next", State: "queued", Reason: "queued",
	}}
	registry.recordHeartbeatWaitingPages("worker-a", "session-a", waiting)
	snapshots := registry.WorkerSnapshots()
	if len(snapshots) != 1 || len(snapshots[0].ActivePages) != 1 ||
		snapshots[0].ActivePages[0].URL != "https://example.org/page" ||
		snapshots[0].ActivePages[0].Stage != "node_ingest" ||
		len(snapshots[0].WaitingPages) != 1 ||
		snapshots[0].WaitingPages[0].URL != "https://example.org/next" {
		t.Fatalf("worker snapshot = %#v", snapshots)
	}
	waiting[0].URL = "https://changed.example/"
	if snapshots[0].WaitingPages[0].URL != "https://example.org/next" {
		t.Fatalf("waiting page snapshot aliases heartbeat input: %#v", snapshots[0].WaitingPages[0])
	}
}
