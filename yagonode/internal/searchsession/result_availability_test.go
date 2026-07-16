package searchsession

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestStableWindowReportsMaterializedLookahead(t *testing.T) {
	t.Parallel()

	inner := &expandingSearcher{total: maxSessionDepth, available: maxSessionDepth}
	response, err := NewStableWindow(inner).Search(
		t.Context(),
		searchcore.Request{Query: "go", Limit: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.Availability.Materialized != retrievalDepth(sessionDepth) ||
		response.Availability.Exhausted {
		t.Fatalf("availability = %+v", response.Availability)
	}
}

func TestStableWindowReportsAuthoritativeExhaustion(t *testing.T) {
	t.Parallel()

	response, err := NewStableWindow(&expandingSearcher{total: 21, available: 21}).Search(
		t.Context(),
		searchcore.Request{Query: "go", Limit: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.Availability.Materialized != 21 || !response.Availability.Exhausted {
		t.Fatalf("availability = %+v", response.Availability)
	}
}

func TestStableWindowBoundsBeforeProvingExhaustion(t *testing.T) {
	t.Parallel()

	results := make([]searchcore.Result, retrievalDepth(sessionDepth)+1)
	for index := range results {
		results[index].URL = string(rune('a' + index))
	}
	response, err := NewStableWindow(&refreshSequenceSearcher{responses: []searchcore.Response{{
		TotalResults: len(results),
		Results:      results,
	}}}).Search(t.Context(), searchcore.Request{Query: "go", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if response.Availability.Materialized != retrievalDepth(sessionDepth) ||
		response.Availability.Exhausted || response.TotalResults != len(results) {
		t.Fatalf("response = %+v", response)
	}
}

func TestStableWindowKeepsPartialNoProgressUnexhausted(t *testing.T) {
	t.Parallel()

	results := make([]searchcore.Result, 8)
	for index := range results {
		results[index].URL = string(rune('a' + index))
	}
	failure := searchcore.PartialFailure{
		Source: searchcore.PartialFailureSourceRemoteStage,
		Reason: "deadline",
	}
	inner := &refreshSequenceSearcher{responses: []searchcore.Response{
		{
			TotalResults:    119,
			Results:         results,
			PartialFailures: []searchcore.PartialFailure{failure},
		},
		{
			TotalResults:    119,
			Results:         results,
			PartialFailures: []searchcore.PartialFailure{failure},
		},
	}}
	response, err := NewStableWindow(inner).Search(
		t.Context(),
		searchcore.Request{Query: "go", Offset: 10, Limit: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.TotalResults != 119 || response.Availability.Materialized != 8 ||
		response.Availability.Exhausted || len(response.PartialFailures) != 1 {
		t.Fatalf("response = %+v", response)
	}
}

func TestStableWindowCompleteExtensionClearsEarlierFailures(t *testing.T) {
	t.Parallel()

	results := make([]searchcore.Result, 8)
	for index := range results {
		results[index].URL = string(rune('a' + index))
	}
	inner := &refreshSequenceSearcher{responses: []searchcore.Response{
		{
			TotalResults: 119,
			Results:      results,
			PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceRemoteStage,
				Reason: "deadline",
			}},
		},
		{TotalResults: len(results), Results: results},
	}}
	stable := NewStableWindow(inner)
	if _, err := stable.Search(
		t.Context(),
		searchcore.Request{Query: "go", Limit: 10},
	); err != nil {
		t.Fatal(err)
	}
	response, err := stable.Search(
		t.Context(),
		searchcore.Request{Query: "go", Offset: 10, Limit: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.TotalResults != len(results) || !response.Availability.Exhausted ||
		len(response.PartialFailures) != 0 {
		t.Fatalf("response = %+v", response)
	}
}

func TestStableWindowSafetyCapIsNotExhaustion(t *testing.T) {
	t.Parallel()

	response, err := NewStableWindow(&expandingSearcher{
		total: maxSessionDepth * 2, available: maxSessionDepth,
	}).Search(t.Context(), searchcore.Request{
		Query: "go", Offset: maxSessionDepth - 10, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Availability.Materialized != maxSessionDepth ||
		response.Availability.Exhausted {
		t.Fatalf("availability = %+v", response.Availability)
	}
}
