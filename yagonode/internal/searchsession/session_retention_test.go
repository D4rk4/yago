package searchsession

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestRetainedSessionBytesAccountsCompletePayload(t *testing.T) {
	positions := make([]int, 2, 3)
	images := make([]searchcore.ResultImage, 1, 2)
	images[0] = searchcore.ResultImage{URL: "x", Alt: "x"}
	matches := make([]searchcore.QueryMatch, 1, 2)
	matches[0] = searchcore.QueryMatch{Start: 0, End: 1}
	results := make([]searchcore.Result, 1, 2)
	results[0] = searchcore.Result{
		DocumentID:         "x",
		Analyzer:           "x",
		Title:              "x",
		URL:                "x",
		ClusterID:          "x",
		RepresentativeURL:  "x",
		DisplayURL:         "x",
		Snippet:            "x",
		Source:             searchcore.Source("x"),
		Host:               "x",
		Path:               "x",
		File:               "x",
		ContentType:        "x",
		URLHash:            "x",
		Date:               "x",
		ContentDomain:      searchcore.ContentDomain("x"),
		Language:           "x",
		Author:             "x",
		Keywords:           "x",
		Publisher:          "x",
		Explanation:        "x",
		Images:             images,
		QueryMatches:       matches,
		BodyQueryMatches:   matches,
		FieldScores:        map[string]float64{"x": 1},
		FieldTermPositions: map[string]map[string][]int{"x": {"x": positions}},
	}
	failures := make([]searchcore.PartialFailure, 1, 2)
	failures[0] = searchcore.PartialFailure{Source: "x", Reason: "x"}
	terms := make([]searchcore.FacetTerm, 1, 2)
	terms[0] = searchcore.FacetTerm{Term: "x", Count: 1}
	facets := make([]searchcore.FacetGroup, 1, 2)
	facets[0] = searchcore.FacetGroup{Name: "x", Terms: terms}
	entry := &session{
		key: "x", recovered: "x", didYouMean: "x",
		results: results, failures: failures, facets: facets,
	}

	want := int(retainedSessionWidth + retainedListElementWidth)
	want += 3
	want += cap(results) * int(retainedResultWidth)
	want += 21
	want += cap(images)*int(retainedResultImageWidth) + 2
	want += cap(matches) * int(retainedQueryMatchWidth)
	want += cap(matches) * int(retainedQueryMatchWidth)
	want += retainedMapBytes + retainedMapEntryBytes + 1
	want += retainedMapBytes + retainedMapEntryBytes + 1
	want += retainedMapBytes + retainedMapEntryBytes + 1
	want += cap(positions) * int(retainedPositionValueWidth)
	want += cap(failures)*int(retainedFailureWidth) + 2
	want += cap(facets)*int(retainedFacetGroupWidth) + 1
	want += cap(terms)*int(retainedFacetTermWidth) + 1
	if got := retainedSessionBytes(entry); got != want {
		t.Fatalf("retained bytes = %d, want %d", got, want)
	}
}

func TestStableWindowDoesNotRetainOversizedSession(t *testing.T) {
	inner := &shufflingSearcher{}
	stable := WithStableWindow(inner).(*stableSearcher)
	stable.limit = 1

	first, err := stable.Search(context.Background(), searchcore.Request{
		Query: "go", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Results) != 10 || len(stable.sessions) != 0 || stable.retained != 0 ||
		stable.order.Len() != 0 {
		t.Fatalf(
			"oversized response rows/sessions/bytes/order = %d/%d/%d/%d",
			len(first.Results),
			len(stable.sessions),
			stable.retained,
			stable.order.Len(),
		)
	}
	second, err := stable.Search(context.Background(), searchcore.Request{
		Query: "go", Offset: 10, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if inner.calls != 2 || second.Results[0].Title != "call2-result10" {
		t.Fatalf("uncached deep page calls/title = %d/%q", inner.calls, second.Results[0].Title)
	}
}

func TestStableWindowByteLimitEvictsLeastRecentSession(t *testing.T) {
	stable := WithStableWindow(&shufflingSearcher{}).(*stableSearcher)
	response := searchcore.Response{Results: []searchcore.Result{{Title: "x", URL: "x"}}}
	first := stable.store("first", response, 1)
	stable.store("second", response, 1)
	stable.limit = stable.retained
	if cached, found := stable.lookup(first.key); !found || cached != first {
		t.Fatal("first session was not reusable")
	}
	stable.store("third", response, 1)
	if _, found := stable.sessions["second"]; found {
		t.Fatal("least recent session survived the byte limit")
	}
	if stable.sessions["first"] != first || stable.sessions["third"] == nil ||
		stable.retained > stable.limit {
		t.Fatalf("retained sessions/bytes = %#v/%d", stable.sessions, stable.retained)
	}
}

func TestRecentWindowRefreshesLeastRecentSession(t *testing.T) {
	stable := WithStableWindow(&shufflingSearcher{}).(*stableSearcher)
	response := searchcore.Response{Results: []searchcore.Result{{URL: "https://result.example/"}}}
	hotRequest := searchcore.Request{Query: "hot"}
	hotKey := sessionKey(hotRequest)
	stable.store(hotKey, response, 1)
	for index := 1; index < maxSessions; index++ {
		request := searchcore.Request{Query: fmt.Sprintf("cold-%d", index)}
		stable.store(sessionKey(request), response, 1)
	}
	if recent, found := stable.Recent(hotRequest); !found || len(recent.Results) != 1 {
		t.Fatalf("recent hot session = %#v, %t", recent, found)
	}
	overflow := searchcore.Request{Query: "overflow"}
	stable.store(sessionKey(overflow), response, 1)
	if stable.sessions[hotKey] == nil {
		t.Fatal("recently recovered session was evicted")
	}
	if stable.sessions[sessionKey(searchcore.Request{Query: "cold-1"})] != nil {
		t.Fatal("untouched least-recent session survived")
	}
}

func TestStableWindowExtensionReconcilesRetainedBytes(t *testing.T) {
	inner := &expandingSearcher{total: maxSessionDepth, available: maxSessionDepth}
	stable := WithStableWindow(inner).(*stableSearcher)
	ctx := context.Background()
	if _, err := stable.Search(ctx, searchcore.Request{Query: "one", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := stable.Search(ctx, searchcore.Request{Query: "two", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	oneKey := sessionKey(searchcore.Request{Query: "one"})
	twoKey := sessionKey(searchcore.Request{Query: "two"})
	projectionResponse, err := inner.Search(ctx, searchcore.Request{Limit: retrievalDepth(100)})
	if err != nil {
		t.Fatal(err)
	}
	source := stable.sessions[oneKey]
	projected := &session{
		key:         source.key,
		results:     appendUnseen(source.results, projectionResponse.Results, retrievalDepth(100)),
		failures:    source.failures,
		total:       source.total,
		searchDepth: source.searchDepth,
		recovered:   source.recovered,
		didYouMean:  source.didYouMean,
		facets:      source.facets,
		expires:     source.expires,
	}
	stable.limit = retainedSessionBytes(projected)
	if _, err := stable.Search(ctx, searchcore.Request{
		Query: "one", Offset: 50, Limit: 10,
	}); err != nil {
		t.Fatal(err)
	}
	if stable.sessions[oneKey] == nil || stable.sessions[twoKey] != nil ||
		stable.retained > stable.limit {
		t.Fatalf("extension cache/bytes = %#v/%d", stable.sessions, stable.retained)
	}
}

func TestStableWindowStorePurgesExpiredSessions(t *testing.T) {
	base := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	current := base
	previousClock := clock
	t.Cleanup(func() { clock = previousClock })
	clock = func() time.Time { return current }

	stable := WithStableWindow(&shufflingSearcher{}).(*stableSearcher)
	response := searchcore.Response{Results: []searchcore.Result{{URL: "x"}}}
	stable.store("expired", response, 1)
	current = base.Add(sessionTTL + time.Second)
	stable.store("current", response, 1)
	if stable.sessions["expired"] != nil || stable.sessions["current"] == nil ||
		stable.order.Len() != 1 {
		t.Fatalf("sessions after expiry purge = %#v", stable.sessions)
	}
}

func TestRetainedByteArithmeticSaturates(t *testing.T) {
	if got := retainedProduct(retainedMaximumInt, 2); got != retainedMaximumInt {
		t.Fatalf("product = %d", got)
	}
	if got := retainedAdd(retainedMaximumInt, 1); got != retainedMaximumInt {
		t.Fatalf("sum = %d", got)
	}
}
