package runtally

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestTallyRecordsBoundedRecentPageOutcomes(t *testing.T) {
	tally := New()
	tally.now = func() time.Time { return time.Unix(123, 0) }
	provenance := []byte("run")
	for index := range yagocrawlcontract.MaximumRecentCrawlURLOutcomes + 2 {
		tally.CommitPage(provenance, "https://example.com/page", yagocrawlcontract.CrawlRunTally{
			Indexed: uint64(index % 2),
			Fetched: 1,
		}, "")
	}
	outcomes := tally.RecentOutcomes(provenance).Chronological()
	if len(outcomes) != yagocrawlcontract.MaximumRecentCrawlURLOutcomes ||
		outcomes[0].Sequence != 3 || outcomes[len(outcomes)-1].Sequence != 66 {
		t.Fatalf("outcomes = %#v", outcomes)
	}
	if outcomes[0].ObservedAt != time.Unix(123, 0).UTC() {
		t.Fatalf("observed at = %s", outcomes[0].ObservedAt)
	}
	tally.Forget(provenance)
	if got := tally.RecentOutcomes(provenance).Chronological(); len(got) != 0 {
		t.Fatalf("forgotten outcomes = %#v", got)
	}
}

func TestTallyClassifiesPageOutcomes(t *testing.T) {
	tests := []struct {
		tally yagocrawlcontract.CrawlRunTally
		class yagocrawlcontract.CrawlURLOutcomeClass
	}{
		{
			yagocrawlcontract.CrawlRunTally{RobotsDenied: 1},
			yagocrawlcontract.CrawlURLOutcomeRobotsDenied,
		},
		{
			yagocrawlcontract.CrawlRunTally{Fetched: 1, Failed: 1},
			yagocrawlcontract.CrawlURLOutcomeFailed,
		},
		{
			yagocrawlcontract.CrawlRunTally{Fetched: 1, Indexed: 1},
			yagocrawlcontract.CrawlURLOutcomeIndexed,
		},
		{yagocrawlcontract.CrawlRunTally{Fetched: 1}, yagocrawlcontract.CrawlURLOutcomeFetched},
		{
			yagocrawlcontract.CrawlRunTally{Duplicates: 1},
			yagocrawlcontract.CrawlURLOutcomeDuplicate,
		},
		{yagocrawlcontract.CrawlRunTally{}, yagocrawlcontract.CrawlURLOutcomeSkipped},
	}
	for _, test := range tests {
		if got := crawlURLOutcomeClass(test.tally); got != test.class {
			t.Errorf("classify %+v = %q, want %q", test.tally, got, test.class)
		}
	}
}

func TestTallyTruncatesOutcomeURLAtUTF8Boundary(t *testing.T) {
	tally := New()
	rawURL := strings.Repeat("a", yagocrawlcontract.MaximumCrawlOutcomeURLBytes-1) + "界"
	tally.CommitPage([]byte("run"), rawURL, yagocrawlcontract.CrawlRunTally{Fetched: 1}, "")
	stored := tally.RecentOutcomes([]byte("run")).Chronological()[0].URL
	if len(stored) > yagocrawlcontract.MaximumCrawlOutcomeURLBytes || !utf8.ValidString(stored) {
		t.Fatalf("stored URL bytes = %d, valid = %t", len(stored), utf8.ValidString(stored))
	}
}

func TestTallyBoundsOutcomeReasonAtUTF8Boundary(t *testing.T) {
	tally := New()
	reason := strings.Repeat("a", yagocrawlcontract.MaximumCrawlOutcomeReasonBytes-1) + "界"
	tally.CommitPage(
		[]byte("run"),
		"https://example.com/",
		yagocrawlcontract.CrawlRunTally{Fetched: 1, Failed: 1},
		reason,
	)
	stored := tally.RecentOutcomes([]byte("run")).Chronological()[0].Reason
	if len(stored) > yagocrawlcontract.MaximumCrawlOutcomeReasonBytes ||
		!utf8.ValidString(stored) {
		t.Fatalf("stored reason bytes = %d, valid = %t", len(stored), utf8.ValidString(stored))
	}
}

func TestTallyRetainsKnownPageHTTPStatus(t *testing.T) {
	tally := New()
	tally.CommitPageWithStatus(
		[]byte("run"),
		"https://example.com/",
		yagocrawlcontract.CrawlRunTally{Fetched: 1, Indexed: 1},
		200,
		"",
	)
	stored := tally.RecentOutcomes([]byte("run")).Chronological()[0]
	if stored.HTTPStatus != 200 {
		t.Fatalf("HTTP status = %d, want 200", stored.HTTPStatus)
	}
}
