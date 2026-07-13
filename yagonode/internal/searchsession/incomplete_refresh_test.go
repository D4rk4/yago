package searchsession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type refreshSequenceSearcher struct {
	responses []searchcore.Response
	errors    []error
	calls     int
}

type cancelingExtensionSearcher struct {
	cancel context.CancelFunc
	calls  int
}

func (s *cancelingExtensionSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	s.calls++
	results := make([]searchcore.Result, 50)
	for index := range results {
		results[index].URL = "https://cancel.example/" + string(rune('a'+index%26))
	}
	if s.calls > 1 {
		s.cancel()
	}

	return searchcore.Response{TotalResults: 100, Results: results}, nil
}

func (s *refreshSequenceSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	position := s.calls
	s.calls++
	var response searchcore.Response
	if position < len(s.responses) {
		response = s.responses[position]
	}
	if position < len(s.errors) {
		return response, s.errors[position]
	}

	return response, nil
}

func TestStableWindowKeepsRecentSuccessOnIncompletePageOneRefresh(t *testing.T) {
	failure := searchcore.PartialFailure{
		Source: searchcore.PartialFailureSourceExactStage,
		Reason: "deadline",
	}
	inner := &refreshSequenceSearcher{responses: []searchcore.Response{
		{
			TotalResults: 1,
			Results:      []searchcore.Result{{URL: "https://drunklab.example/"}},
			PartialFailures: []searchcore.PartialFailure{{
				Source: "peer", Reason: "late",
			}},
		},
		{PartialFailures: []searchcore.PartialFailure{failure, failure}},
	}}
	stable := NewStableWindow(inner)
	req := searchcore.Request{Query: "drunklab", Limit: 10}
	if _, err := stable.Search(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	response, err := stable.Search(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].URL != "https://drunklab.example/" ||
		len(response.PartialFailures) != 2 || response.PartialFailures[1] != failure {
		t.Fatalf("response = %#v", response)
	}
	recent, ok := stable.Recent(req)
	if !ok || len(recent.PartialFailures) != 1 || recent.PartialFailures[0].Source != "peer" {
		t.Fatalf("recent = %#v, found = %t", recent, ok)
	}
}

func TestStableWindowHonestEmptyReplacesRecentSuccess(t *testing.T) {
	inner := &refreshSequenceSearcher{responses: []searchcore.Response{
		{Results: []searchcore.Result{{URL: "https://previous.example/"}}},
		{PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceRemoteYaCy,
			Reason: "no known peers",
		}}},
	}}
	stable := NewStableWindow(inner)
	req := searchcore.Request{Query: "gone", Limit: 10}
	if _, err := stable.Search(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	response, err := stable.Search(t.Context(), req)
	if err != nil || len(response.Results) != 0 || len(response.PartialFailures) != 1 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
	if recent, ok := stable.Recent(req); ok || len(recent.Results) != 0 {
		t.Fatalf("recent = %#v, found = %t", recent, ok)
	}
}

func TestStableWindowKeepsLocalSuccessOnWebOnlyInfrastructureRefresh(t *testing.T) {
	inner := &refreshSequenceSearcher{responses: []searchcore.Response{
		{Results: []searchcore.Result{{
			URL:    "https://drunklab.example/",
			Source: searchcore.SourceLocal,
		}}},
		{
			Results: []searchcore.Result{{
				URL:    "https://web.example/",
				Source: searchcore.SourceWeb,
			}},
			PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceLocalExactStage,
				Reason: "deadline",
			}},
		},
	}}
	stable := NewStableWindow(inner)
	req := searchcore.Request{Query: "drunklab", Limit: 10}
	if _, err := stable.Search(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	response, err := stable.Search(t.Context(), req)
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].URL != "https://drunklab.example/" ||
		len(response.PartialFailures) != 1 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
	recent, ok := stable.Recent(req)
	if !ok || len(recent.Results) != 1 ||
		recent.Results[0].URL != "https://drunklab.example/" ||
		len(recent.PartialFailures) != 0 {
		t.Fatalf("recent = %#v, found = %t", recent, ok)
	}
}

func TestStableWindowUsesRecentSuccessWhenWebProviderFailsWithoutPrimaryResults(t *testing.T) {
	inner := &refreshSequenceSearcher{responses: []searchcore.Response{
		{Results: []searchcore.Result{{URL: "https://previous.example/"}}},
		{PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceWeb,
			Reason: "provider failed",
		}}},
	}}
	stable := NewStableWindow(inner)
	req := searchcore.Request{Query: "provider", Limit: 10}
	if _, err := stable.Search(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	response, err := stable.Search(t.Context(), req)
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].URL != "https://previous.example/" ||
		len(response.PartialFailures) != 1 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestStableWindowStoresPrimaryResultsWhenParallelWebProviderFails(t *testing.T) {
	inner := &refreshSequenceSearcher{responses: []searchcore.Response{
		{Results: []searchcore.Result{{URL: "https://previous.example/"}}},
		{
			Results: []searchcore.Result{{
				URL:    "https://current.example/",
				Source: searchcore.SourceLocal,
			}},
			PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceWeb,
				Reason: "provider failed",
			}},
		},
	}}
	stable := NewStableWindow(inner)
	req := searchcore.Request{Query: "always", Limit: 10}
	if _, err := stable.Search(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	response, err := stable.Search(t.Context(), req)
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].URL != "https://current.example/" ||
		len(response.PartialFailures) != 1 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
	recent, ok := stable.Recent(req)
	if !ok || len(recent.Results) != 1 ||
		recent.Results[0].URL != "https://current.example/" ||
		len(recent.PartialFailures) != 1 {
		t.Fatalf("recent = %#v, found = %t", recent, ok)
	}
}

func TestStableWindowDoesNotStoreFirstIncompleteRefresh(t *testing.T) {
	stable := NewStableWindow(&refreshSequenceSearcher{responses: []searchcore.Response{{
		PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceExactStage,
			Reason: "deadline",
		}},
	}}})
	req := searchcore.Request{Query: "cold", Limit: 10}
	response, err := stable.Search(t.Context(), req)
	if err != nil || len(response.PartialFailures) != 1 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
	if _, ok := stable.Recent(req); ok {
		t.Fatal("incomplete refresh was cached")
	}
}

func TestStableWindowDoesNotStoreResponseAfterCancellation(t *testing.T) {
	stable := NewStableWindow(&refreshSequenceSearcher{responses: []searchcore.Response{{
		Results: []searchcore.Result{{URL: "https://late.example/"}},
	}}})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := searchcore.Request{Query: "late", Limit: 10}
	_, err := stable.Search(ctx, req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if _, ok := stable.Recent(req); ok {
		t.Fatal("late response was cached")
	}
}

func TestStableWindowDoesNotReuseExpiredSuccessForIncompleteRefresh(t *testing.T) {
	base := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	current := base
	previousClock := clock
	clock = func() time.Time { return current }
	t.Cleanup(func() { clock = previousClock })
	inner := &refreshSequenceSearcher{responses: []searchcore.Response{
		{Results: []searchcore.Result{{URL: "https://expired.example/"}}},
		{PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceExactStage,
			Reason: "deadline",
		}}},
		{PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceExactStage,
			Reason: "deadline",
		}}},
	}}
	stable := NewStableWindow(inner)
	req := searchcore.Request{Query: "expired", Limit: 10}
	if _, err := stable.Search(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	current = base.Add(sessionTTL / 2)
	response, err := stable.Search(t.Context(), req)
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].URL != "https://expired.example/" {
		t.Fatalf("response before expiry = %#v, error = %v", response, err)
	}
	current = base.Add(sessionTTL + time.Second)
	response, err = stable.Search(t.Context(), req)
	if err != nil || len(response.Results) != 0 || len(response.PartialFailures) != 1 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestStableWindowIncompleteExtensionDoesNotCollapseSession(t *testing.T) {
	results := make([]searchcore.Result, 50)
	for index := range results {
		results[index].URL = "https://result.example/" + string(rune('a'+index%26))
	}
	inner := &refreshSequenceSearcher{responses: []searchcore.Response{
		{TotalResults: 100, Results: results},
		{PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceExactStage,
			Reason: "deadline",
		}}},
	}}
	stable := NewStableWindow(inner)
	req := searchcore.Request{Query: "pages", Limit: 10}
	if _, err := stable.Search(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	response, err := stable.Search(t.Context(), searchcore.Request{
		Query: "pages", Offset: 50, Limit: 10,
	})
	if err != nil || response.TotalResults != 100 || len(response.Results) != 0 ||
		len(response.PartialFailures) != 1 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestStableWindowCanceledExtensionDoesNotMutateSession(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	inner := &cancelingExtensionSearcher{cancel: cancel}
	stable := NewStableWindow(inner)
	req := searchcore.Request{Query: "cancel extension", Limit: 10}
	if _, err := stable.Search(ctx, req); err != nil {
		t.Fatal(err)
	}
	_, err := stable.Search(ctx, searchcore.Request{
		Query: "cancel extension", Offset: 50, Limit: 10,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestRecentSuccessDecoratorUsesSharedSessionForOuterFailure(t *testing.T) {
	stable := NewStableWindow(&refreshSequenceSearcher{responses: []searchcore.Response{{
		Results: []searchcore.Result{{URL: "https://drunklab.example/"}},
	}}})
	req := searchcore.Request{Query: "drunklab", Limit: 10}
	if _, err := stable.Search(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	outerFailure := searchcore.PartialFailure{
		Source: searchcore.PartialFailureSourceLocalSearch,
		Reason: "deadline",
	}
	inner := &refreshSequenceSearcher{responses: []searchcore.Response{{
		PartialFailures: []searchcore.PartialFailure{outerFailure},
	}}, errors: []error{context.DeadlineExceeded}}
	response, err := WithRecentSuccessOnIncompleteRefresh(inner, stable).Search(t.Context(), req)
	if err != nil || len(response.Results) != 1 ||
		len(response.PartialFailures) != 1 || response.PartialFailures[0] != outerFailure {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestRecentSuccessDecoratorPreservesErrorsCancellationAndHonestMisses(t *testing.T) {
	want := errors.New("failed")
	failing := &refreshSequenceSearcher{errors: []error{want}}
	decorated := WithRecentSuccessOnIncompleteRefresh(failing, noRecentWindow{})
	if _, err := decorated.Search(
		t.Context(),
		searchcore.Request{Query: "failed"},
	); !errors.Is(
		err,
		want,
	) {
		t.Fatalf("error = %v", err)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	incomplete := &refreshSequenceSearcher{responses: []searchcore.Response{{
		PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceLocalSearch,
			Reason: "deadline",
		}},
	}}}
	response, err := WithRecentSuccessOnIncompleteRefresh(incomplete, noRecentWindow{}).Search(
		canceled,
		searchcore.Request{Query: "canceled"},
	)
	if err != nil || len(response.PartialFailures) != 1 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}

	honest := &refreshSequenceSearcher{responses: []searchcore.Response{{}}}
	response, err = WithRecentSuccessOnIncompleteRefresh(honest, noRecentWindow{}).Search(
		t.Context(),
		searchcore.Request{Query: "honest"},
	)
	if err != nil || len(response.Results) != 0 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}

	uncached := &refreshSequenceSearcher{responses: []searchcore.Response{{
		PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceExactStage,
			Reason: "deadline",
		}},
	}}}
	response, err = WithRecentSuccessOnIncompleteRefresh(uncached, noRecentWindow{}).Search(
		t.Context(),
		searchcore.Request{Query: "uncached"},
	)
	if err != nil || len(response.PartialFailures) != 1 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}

	if got := WithRecentSuccessOnIncompleteRefresh(honest, nil); got != honest {
		t.Fatal("nil recent window changed the searcher")
	}
}

type noRecentWindow struct{}

func (noRecentWindow) Recent(searchcore.Request) (searchcore.Response, bool) {
	return searchcore.Response{}, false
}
