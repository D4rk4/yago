package yagocrawlcontract_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func crawlURLOutcome(sequence uint32) yagocrawlcontract.CrawlURLOutcome {
	return yagocrawlcontract.CrawlURLOutcome{
		Sequence:   uint64(sequence),
		URL:        fmt.Sprintf("https://example.com/%d", sequence),
		Class:      yagocrawlcontract.CrawlURLOutcomeIndexed,
		ObservedAt: time.Unix(int64(sequence), 0).UTC(),
	}
}

func TestCrawlURLOutcomeHistoryBoundsAndOrdersEntries(t *testing.T) {
	var history yagocrawlcontract.CrawlURLOutcomeHistory
	for sequence := uint32(1); sequence <= yagocrawlcontract.MaximumRecentCrawlURLOutcomes+3; sequence++ {
		history.Append(crawlURLOutcome(sequence))
	}
	chronological := history.Chronological()
	if len(chronological) != yagocrawlcontract.MaximumRecentCrawlURLOutcomes ||
		chronological[0].Sequence != 4 ||
		chronological[len(chronological)-1].Sequence != 67 {
		t.Fatalf(
			"chronological outcomes = first %d last %d length %d",
			chronological[0].Sequence,
			chronological[len(chronological)-1].Sequence,
			len(chronological),
		)
	}
	newest := history.NewestFirst()
	if newest[0].Sequence != 67 || newest[len(newest)-1].Sequence != 4 {
		t.Fatalf(
			"newest outcomes = first %d last %d",
			newest[0].Sequence,
			newest[len(newest)-1].Sequence,
		)
	}
}

func TestCrawlURLOutcomeHistoryMergesBySessionAndSequence(t *testing.T) {
	first, err := yagocrawlcontract.NewCrawlURLOutcomeHistory([]yagocrawlcontract.CrawlURLOutcome{
		crawlURLOutcome(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	first = first.WithWorkerSessionID("session-one")
	duplicate := crawlURLOutcome(1)
	duplicate.URL = "https://example.com/replayed"
	second, err := yagocrawlcontract.NewCrawlURLOutcomeHistory([]yagocrawlcontract.CrawlURLOutcome{
		duplicate,
		crawlURLOutcome(2),
	})
	if err != nil {
		t.Fatal(err)
	}
	second = second.WithWorkerSessionID("session-one")
	merged := first.Merge(second).Chronological()
	if len(merged) != 2 || merged[0].URL != "https://example.com/1" || merged[1].Sequence != 2 {
		t.Fatalf("merged outcomes = %#v", merged)
	}
	otherSession := first.WithWorkerSessionID("session-two")
	if got := first.Merge(otherSession).Chronological(); len(got) != 2 {
		t.Fatalf("cross-session outcomes = %#v", got)
	}
}

func TestCrawlURLOutcomeHistoryJSONIsCompactAndRoundTrips(t *testing.T) {
	history, err := yagocrawlcontract.NewCrawlURLOutcomeHistory([]yagocrawlcontract.CrawlURLOutcome{
		crawlURLOutcome(1),
		crawlURLOutcome(2),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), `"sequence"`) != 2 {
		t.Fatalf("encoded history = %s", raw)
	}
	var decoded yagocrawlcontract.CrawlURLOutcomeHistory
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Chronological(), history.Chronological()) {
		t.Fatalf("decoded history = %#v", decoded.Chronological())
	}
	if decoded != history {
		t.Fatal("history must remain comparable")
	}
	if !decoded.Valid() {
		t.Fatal("round-tripped history is invalid")
	}
}

func TestCrawlURLOutcomeValidationRejectsWireExpansion(t *testing.T) {
	outcome := crawlURLOutcome(1)
	cases := []func(*yagocrawlcontract.CrawlURLOutcome){
		func(value *yagocrawlcontract.CrawlURLOutcome) { value.Sequence = 0 },
		func(value *yagocrawlcontract.CrawlURLOutcome) {
			value.URL = strings.Repeat("u", yagocrawlcontract.MaximumCrawlOutcomeURLBytes+1)
		},
		func(value *yagocrawlcontract.CrawlURLOutcome) { value.Class = "other" },
		func(value *yagocrawlcontract.CrawlURLOutcome) { value.ObservedAt = time.Time{} },
		func(value *yagocrawlcontract.CrawlURLOutcome) { value.HTTPStatus = 1000 },
		func(value *yagocrawlcontract.CrawlURLOutcome) {
			value.Reason = strings.Repeat("r", yagocrawlcontract.MaximumCrawlOutcomeReasonBytes+1)
		},
		func(value *yagocrawlcontract.CrawlURLOutcome) {
			value.WorkerSessionID = strings.Repeat(
				"s",
				yagocrawlcontract.MaximumCrawlerSessionIdentityBytes+1,
			)
		},
	}
	for index, mutate := range cases {
		candidate := outcome
		mutate(&candidate)
		if candidate.Valid() {
			t.Errorf("case %d accepted %#v", index, candidate)
		}
	}
}

func TestCrawlURLOutcomeHistoryRejectsInvalidAndOversizedPayloads(t *testing.T) {
	invalid := crawlURLOutcome(1)
	invalid.URL = ""
	if _, err := yagocrawlcontract.NewCrawlURLOutcomeHistory(
		[]yagocrawlcontract.CrawlURLOutcome{invalid},
	); err == nil {
		t.Fatal("invalid outcome was accepted")
	}
	oversized := make(
		[]yagocrawlcontract.CrawlURLOutcome,
		yagocrawlcontract.MaximumRecentCrawlURLOutcomes+1,
	)
	if _, err := yagocrawlcontract.NewCrawlURLOutcomeHistory(oversized); err == nil {
		t.Fatal("oversized outcome history was accepted")
	}
	var history yagocrawlcontract.CrawlURLOutcomeHistory
	history.Append(invalid)
	if history.Valid() {
		t.Fatal("history containing an invalid outcome was accepted")
	}
}

func TestCrawlURLOutcomeHistoryJSONRejectsUnknownAndInvalidValues(t *testing.T) {
	var history yagocrawlcontract.CrawlURLOutcomeHistory
	for _, raw := range []string{
		`[{"sequence":1,"url":"https://example.com/","class":"indexed","observedAt":"2026-07-18T12:00:00Z","unknown":true}]`,
		`[{"sequence":0,"url":"https://example.com/","class":"indexed","observedAt":"2026-07-18T12:00:00Z"}]`,
	} {
		if err := json.Unmarshal([]byte(raw), &history); err == nil {
			t.Fatalf("invalid history was accepted: %s", raw)
		}
	}
}

func TestCrawlURLOutcomeHistoryMarshalPreservesTimeEncodingFailure(t *testing.T) {
	outcome := crawlURLOutcome(1)
	outcome.ObservedAt = time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
	var history yagocrawlcontract.CrawlURLOutcomeHistory
	history.Append(outcome)

	raw, err := history.MarshalJSON()
	if err == nil {
		t.Fatalf("encoded invalid timestamp as %s", raw)
	}
	if raw != nil {
		t.Fatalf("failed encoding returned %q", raw)
	}
	var marshalError *json.MarshalerError
	if !errors.As(err, &marshalError) {
		t.Fatalf("encoding error type = %T", err)
	}
	if !strings.Contains(err.Error(), "encode crawl URL outcome history") {
		t.Fatalf("encoding error = %v", err)
	}
}
