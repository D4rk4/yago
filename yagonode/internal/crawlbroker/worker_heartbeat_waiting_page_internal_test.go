package crawlbroker

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestWaitingCrawlerPagesFromHeartbeatValidatesAndCopiesPageStatus(t *testing.T) {
	input := []*crawlrpc.WaitingCrawlPage{{
		RunId:                          "run-a",
		Url:                            "https://example.org/next",
		State:                          "deferred",
		Reason:                         "retry delay",
		AvailableAfterUnixMilliseconds: 1_800_000_000_000,
	}}
	got, valid := waitingCrawlerPagesFromHeartbeat(input)
	if !valid || len(got) != 1 || got[0].RunID != "run-a" ||
		got[0].URL != input[0].GetUrl() || got[0].State != "deferred" ||
		got[0].Reason != "retry delay" || got[0].AvailableAt.IsZero() {
		t.Fatalf("waiting pages = %#v, valid %t", got, valid)
	}
	input[0].Url = "https://changed.example/"
	if got[0].URL != "https://example.org/next" {
		t.Fatalf("waiting page snapshot aliases heartbeat input: %#v", got[0])
	}
}

func TestWaitingCrawlerPagesFromHeartbeatRejectsInvalidSamples(t *testing.T) {
	for index, pages := range [][]*crawlrpc.WaitingCrawlPage{
		{{RunId: "run-a", Url: "https://example.org/page", State: "fetching", Reason: "queued"}},
		{{RunId: "run-a", Url: "https://example.org/page", State: "queued", Reason: "unknown"}},
		{{RunId: "", Url: "https://example.org/page", State: "queued", Reason: "queued"}},
		{{RunId: "run-a", Url: "https://user:password@example.org/page", State: "queued", Reason: "queued"}},
		{{RunId: "run-a", Url: "https://example.org/page#fragment", State: "queued", Reason: "queued"}},
		{{RunId: "run-a", Url: "https://example.org/page", State: "queued", Reason: "queued", AvailableAfterUnixMilliseconds: -1}},
		{{RunId: "run-a", Url: "https://example.org/page", State: "queued", Reason: "queued"}, nil},
	} {
		if _, valid := waitingCrawlerPagesFromHeartbeat(pages); valid {
			t.Fatalf("case %d accepted an invalid sample", index)
		}
	}
	tooMany := make(
		[]*crawlrpc.WaitingCrawlPage,
		yagocrawlcontract.MaximumCrawlerWaitingPageReports+1,
	)
	for index := range tooMany {
		tooMany[index] = &crawlrpc.WaitingCrawlPage{
			RunId:  "run-a",
			Url:    "https://example.org/page",
			State:  "queued",
			Reason: "queued",
		}
	}
	if _, valid := waitingCrawlerPagesFromHeartbeat(tooMany); valid {
		t.Fatal("accepted a sample over the report limit")
	}
	tooManyForRun := make(
		[]*crawlrpc.WaitingCrawlPage,
		yagocrawlcontract.MaximumCrawlerWaitingPagesPerRun+1,
	)
	for index := range tooManyForRun {
		tooManyForRun[index] = &crawlrpc.WaitingCrawlPage{
			RunId:  "run-a",
			Url:    "https://example.org/page",
			State:  "queued",
			Reason: "queued",
		}
	}
	if _, valid := waitingCrawlerPagesFromHeartbeat(tooManyForRun); valid {
		t.Fatal("accepted more waiting pages for a run than the sample limit")
	}
}
