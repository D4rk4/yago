package crawlbroker

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestCrawlRunLimitTranslationRejectsIncompleteOverflowAndInvalidValues(t *testing.T) {
	perHost := int64(250)
	perRun := uint64(900)
	if gotHost, gotRun, known, err := crawlRunLimitsFromProto(
		&perHost,
		&perRun,
	); err != nil || !known || gotHost != 250 ||
		gotRun != 900 {
		t.Fatalf("translated limits = %d/%d/%t/%v", gotHost, gotRun, known, err)
	}
	if _, _, _, err := crawlRunLimitsFromProto(nil, &perRun); err == nil {
		t.Fatal("incomplete crawl run limits were accepted")
	}
	tooLarge := uint64(^uint(0)>>1) + 1
	if _, _, _, err := crawlRunLimitsFromProto(&perHost, &tooLarge); err == nil {
		t.Fatal("platform-overflowing crawl run limit was accepted")
	}
	invalidHost := int64(-2)
	if _, _, _, err := crawlRunLimitsFromProto(&invalidHost, &perRun); err == nil {
		t.Fatal("invalid per-host crawl run limit was accepted")
	}
}

func TestCrawlURLOutcomeTranslationBoundsAndValidatesWorkerEvidence(t *testing.T) {
	observed := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	encoded := []*crawlrpc.CrawlURLOutcome{{
		Sequence: 1, Url: "https://example.com/", OutcomeClass: "failed",
		ObservedAtUnixMilliseconds: observed.UnixMilli(), HttpStatus: 503,
		Reason: "page fetch failed",
	}}
	history, err := crawlURLOutcomeHistoryFromProto(encoded, testWorkerSessionID)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := history.Chronological()
	if len(outcomes) != 1 || outcomes[0].WorkerSessionID != testWorkerSessionID ||
		outcomes[0].Class != yagocrawlcontract.CrawlURLOutcomeFailed ||
		outcomes[0].HTTPStatus != 503 || outcomes[0].Reason != "page fetch failed" {
		t.Fatalf("translated outcomes = %+v", outcomes)
	}
	oversized := make(
		[]*crawlrpc.CrawlURLOutcome,
		yagocrawlcontract.MaximumRecentCrawlURLOutcomes+1,
	)
	if _, err := crawlURLOutcomeHistoryFromProto(oversized, testWorkerSessionID); err == nil {
		t.Fatal("oversized outcome history was accepted")
	}
	if _, err := crawlURLOutcomeHistoryFromProto(
		[]*crawlrpc.CrawlURLOutcome{nil},
		testWorkerSessionID,
	); err == nil {
		t.Fatal("nil outcome was accepted")
	}
	encoded[0].Sequence = 0
	if _, err := crawlURLOutcomeHistoryFromProto(encoded, testWorkerSessionID); err == nil {
		t.Fatal("invalid outcome was accepted")
	}
}
