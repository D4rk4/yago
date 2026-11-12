package yagonode

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type localDeadlineFallbackProbe struct {
	response searchcore.Response
	err      error
	wait     bool
	calls    int
	deadline time.Time
	cancel   context.CancelFunc
}

type progressiveDeadlineFallbackIndex struct{ stubSearchIndex }

func (progressiveDeadlineFallbackIndex) SearchProgressively(
	context.Context,
	searchindex.SearchRequest,
	func(searchindex.SearchProgress),
) (searchindex.SearchResultSet, error) {
	return searchindex.SearchResultSet{}, nil
}

func TestPublicLocalSearchRetrievalWrapsConfiguredIndex(t *testing.T) {
	primary := &localDeadlineFallbackProbe{}
	withoutIndex := publicLocalSearchRetrieval(publicSearchAssembly{}, primary)
	if withoutIndex != primary {
		t.Fatalf("retrieval without an index = %T, want primary", withoutIndex)
	}

	assembly := publicSearchAssembly{storage: nodeStorage{searchIndex: stubSearchIndex{}}}
	withoutProgressiveSupport := publicLocalSearchRetrieval(assembly, primary)
	if withoutProgressiveSupport != primary {
		t.Fatalf(
			"retrieval without progressive index support = %T, want primary",
			withoutProgressiveSupport,
		)
	}

	assembly.storage.searchIndex = progressiveDeadlineFallbackIndex{}
	withIndex := publicLocalSearchRetrieval(assembly, primary)
	fallback, ok := withIndex.(localSearchDeadlineFallback)
	if !ok || fallback.primary != primary || fallback.fallback == nil {
		t.Fatalf("retrieval with an index = %#v, want primary plus progressive fallback", withIndex)
	}
}

func (s *localDeadlineFallbackProbe) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.calls++
	s.deadline, _ = ctx.Deadline()
	if s.cancel != nil {
		s.cancel()
	}
	if s.wait {
		<-ctx.Done()
		return s.response, fmt.Errorf("probe search: %w", ctx.Err())
	}
	if s.err != nil {
		return s.response, s.err
	}
	s.response.Request = req
	return s.response, nil
}

func TestLocalSearchDeadlineFallbackKeepsFullTextWhenItFinishes(t *testing.T) {
	primary := &localDeadlineFallbackProbe{
		response: searchcore.Response{Results: []searchcore.Result{{URL: "primary"}}},
	}
	fallback := &localDeadlineFallbackProbe{}
	response, err := (localSearchDeadlineFallback{
		primary: primary, fallback: fallback,
	}).Search(
		requestDeadlineContext(t, time.Second),
		plainLocalFallbackRequest(),
	)
	if err != nil || primary.calls != 1 || fallback.calls != 0 ||
		len(response.Results) != 1 || response.Results[0].URL != "primary" {
		t.Fatalf(
			"response=%#v primary=%d fallback=%d err=%v",
			response,
			primary.calls,
			fallback.calls,
			err,
		)
	}
}

func TestLocalSearchDeadlineFallbackHonorsCallerCancellationDuringPrimary(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	primary := &localDeadlineFallbackProbe{
		response: searchcore.Response{
			Results: []searchcore.Result{{URL: "https://primary.example/"}},
		},
		err: context.Canceled, cancel: cancel,
	}
	fallback := &localDeadlineFallbackProbe{}
	response, err := (localSearchDeadlineFallback{
		primary: primary, fallback: fallback,
	}).Search(ctx, plainLocalFallbackRequest())
	if !errors.Is(err, context.Canceled) || primary.calls != 1 || fallback.calls != 0 ||
		len(response.Results) != 1 {
		t.Fatalf(
			"response=%#v primary=%d fallback=%d error=%v",
			response, primary.calls, fallback.calls, err,
		)
	}
}

func TestLocalSearchDeadlineFallbackReturnsMarkedPartialResults(t *testing.T) {
	primary := &localDeadlineFallbackProbe{err: context.DeadlineExceeded}
	fallback := &localDeadlineFallbackProbe{
		response: searchcore.Response{
			TotalResults: 1,
			Results: []searchcore.Result{
				{Title: "Needle Observatory", URL: "https://observatory.example/"},
			},
			Coverage: &searchcore.SearchCoverage{
				CompletedShards: 3, SampledShards: 2, TotalShards: 8, Complete: false,
			},
		},
	}
	request := plainLocalFallbackRequest()
	response, err := (localSearchDeadlineFallback{
		primary: primary, fallback: fallback,
	}).Search(requestDeadlineContext(t, 150*time.Millisecond), request)
	if err != nil || primary.calls != 1 || fallback.calls != 1 ||
		!primary.deadline.Equal(fallback.deadline) || response.Request.Query != request.Query ||
		len(response.Results) != 1 || response.Results[0].URL != "https://observatory.example/" ||
		len(response.PartialFailures) != 0 || response.Coverage == nil ||
		response.Coverage.SampledShards != 2 {
		t.Fatalf(
			"response=%#v primary=%d fallback=%d err=%v",
			response,
			primary.calls,
			fallback.calls,
			err,
		)
	}
}

func TestLocalSearchProgressiveStreamStartsRandomizedShardWalk(t *testing.T) {
	primary := &localDeadlineFallbackProbe{
		response: searchcore.Response{Results: []searchcore.Result{{URL: "exact"}}},
	}
	fallback := &localDeadlineFallbackProbe{
		response: searchcore.Response{
			Results: []searchcore.Result{{URL: "randomized"}},
			Coverage: &searchcore.SearchCoverage{
				CompletedShards: 1, TotalShards: 8, Complete: false,
			},
		},
	}
	ctx := searchcore.WithSearchProgressObserver(
		t.Context(), func(searchcore.SearchProgress) {},
	)
	request := plainLocalFallbackRequest()
	request.RankingFeatures = true
	request.Verify = searchcore.VerifyIfExist
	response, err := (localSearchDeadlineFallback{
		primary: primary, fallback: fallback,
	}).Search(ctx, request)
	if err != nil || primary.calls != 0 || fallback.calls != 1 ||
		len(response.Results) != 1 || response.Results[0].URL != "randomized" ||
		response.Coverage == nil || response.Coverage.CompletedShards != 1 ||
		len(response.PartialFailures) != 0 {
		t.Fatalf(
			"response=%#v primary=%d fallback=%d err=%v",
			response, primary.calls, fallback.calls, err,
		)
	}
}

func TestLocalSearchFullShardCompletionBypassesRandomizedFallback(t *testing.T) {
	primary := &localDeadlineFallbackProbe{
		response: searchcore.Response{Results: []searchcore.Result{{URL: "primary-full"}}},
	}
	fallback := &localDeadlineFallbackProbe{
		response: searchcore.Response{Results: []searchcore.Result{{URL: "random-sample"}}},
	}
	ctx := searchcore.WithSearchProgressObserver(
		searchcore.WithFullShardSearch(t.Context()),
		func(searchcore.SearchProgress) {},
	)
	response, err := (localSearchDeadlineFallback{
		primary: primary, fallback: fallback,
	}).Search(ctx, plainLocalFallbackRequest())
	if err != nil || primary.calls != 1 || fallback.calls != 0 ||
		len(response.Results) != 1 || response.Results[0].URL != "primary-full" {
		t.Fatalf("response=%+v primary=%d fallback=%d error=%v",
			response, primary.calls, fallback.calls, err)
	}
}

func TestLocalSearchDeadlineFallbackKeepsSampledResultsWithoutTimeoutFailure(t *testing.T) {
	primary := &localDeadlineFallbackProbe{}
	fallback := &localDeadlineFallbackProbe{
		response: searchcore.Response{
			Results: []searchcore.Result{{Title: "Sample", URL: "https://sample.example/"}},
			Coverage: &searchcore.SearchCoverage{
				CompletedShards: 5, SampledShards: 3, TotalShards: 8, Complete: false,
			},
		},
		err: context.DeadlineExceeded,
	}
	ctx := searchcore.WithSearchProgressObserver(t.Context(), func(searchcore.SearchProgress) {})
	response, err := (localSearchDeadlineFallback{
		primary: primary, fallback: fallback,
	}).Search(ctx, plainLocalFallbackRequest())
	if err != nil || len(response.Results) != 1 || len(response.PartialFailures) != 0 ||
		response.Coverage == nil || response.Coverage.SampledShards != 3 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestLocalSearchDeadlineFallbackKeepsPartialShardResultsOnDeadline(t *testing.T) {
	primary := &localDeadlineFallbackProbe{}
	fallback := &localDeadlineFallbackProbe{
		response: searchcore.Response{
			Results: []searchcore.Result{{
				Title: "Partial shard result", URL: "https://observatory.example/partial",
			}},
			Coverage: &searchcore.SearchCoverage{
				CompletedShards: 5, TotalShards: 8, Complete: false,
			},
		},
		err: context.DeadlineExceeded,
	}
	ctx := searchcore.WithSearchProgressObserver(
		t.Context(), func(searchcore.SearchProgress) {},
	)
	request := plainLocalFallbackRequest()
	response, err := (localSearchDeadlineFallback{
		primary: primary, fallback: fallback,
	}).Search(ctx, request)
	if err != nil || primary.calls != 0 || fallback.calls != 1 ||
		response.Request.Query != request.Query || len(response.Results) != 1 ||
		response.Results[0].URL != "https://observatory.example/partial" ||
		response.Coverage == nil || response.Coverage.CompletedShards != 5 ||
		len(response.PartialFailures) != 1 ||
		response.PartialFailures[0] != localSearchDeadlinePartialFailure() {
		t.Fatalf(
			"response=%#v primary=%d fallback=%d err=%v",
			response, primary.calls, fallback.calls, err,
		)
	}
}

func TestLocalSearchDeadlineFallbackUsesRandomizedFallbackAfterPartialPrimary(t *testing.T) {
	primary := &localDeadlineFallbackProbe{
		response: searchcore.Response{
			Results: []searchcore.Result{{URL: "https://fixed-order.example/"}},
			PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceLocalSearch,
				Reason: "partial local result set",
			}},
		},
		err: context.DeadlineExceeded,
	}
	fallback := &localDeadlineFallbackProbe{
		response: searchcore.Response{
			Results: []searchcore.Result{{URL: "https://randomized.example/"}},
			Coverage: &searchcore.SearchCoverage{
				CompletedShards: 2,
				SampledShards:   2,
				TotalShards:     8,
				Complete:        false,
			},
		},
	}
	response, err := (localSearchDeadlineFallback{
		primary: primary, fallback: fallback,
	}).Search(
		requestDeadlineContext(t, 150*time.Millisecond),
		plainLocalFallbackRequest(),
	)
	if err != nil || fallback.calls != 1 || len(response.Results) != 1 ||
		response.Results[0].URL != "https://randomized.example/" ||
		response.Coverage == nil || response.Coverage.CompletedShards != 2 ||
		len(response.PartialFailures) != 0 {
		t.Fatalf("response=%#v fallback=%d err=%v", response, fallback.calls, err)
	}
}

func TestLocalSearchDeadlineFallbackKeepsCompleteShardCoverageComplete(t *testing.T) {
	primary := &localDeadlineFallbackProbe{err: context.DeadlineExceeded}
	fallback := &localDeadlineFallbackProbe{
		response: searchcore.Response{
			Results: []searchcore.Result{{URL: "https://observatory.example/"}},
			Coverage: &searchcore.SearchCoverage{
				CompletedShards: 8, TotalShards: 8, Complete: true,
			},
		},
	}
	response, err := (localSearchDeadlineFallback{
		primary: primary, fallback: fallback,
	}).Search(
		requestDeadlineContext(t, 150*time.Millisecond),
		plainLocalFallbackRequest(),
	)
	if err != nil || response.Coverage == nil || !response.Coverage.Complete ||
		len(response.PartialFailures) != 0 {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}

func TestLocalSearchDeadlineFallbackRejectsUnsupportedRequests(t *testing.T) {
	requests := []searchcore.Request{
		{
			Query: "Harborlight skyline dawn tide map",
			Terms: []string{"Harborlight", "skyline", "dawn", "tide", "map"},
		},
		{Query: "Harborlight", Terms: []string{"Harborlight"}, Phrases: []string{"Harborlight"}},
		{Query: "Harborlight", Terms: []string{"Harborlight"}, ExcludedTerms: []string{"spam"}},
		{Query: "Harborlight", Terms: []string{"Harborlight"}, Offset: 10},
		{Query: "Harborlight", Terms: []string{"Harborlight"}, SiteHost: "example.test"},
		{Query: "Harborlight", Terms: []string{"Harborlight"}, Language: "fr"},
		{
			Query:         "Harborlight",
			Terms:         []string{"Harborlight"},
			ContentDomain: searchcore.ContentDomainImage,
		},
		{Query: "Harborlight!", Terms: []string{"Harborlight"}},
	}
	for _, request := range requests {
		primary := &localDeadlineFallbackProbe{err: context.DeadlineExceeded}
		fallback := &localDeadlineFallbackProbe{}
		response, err := (localSearchDeadlineFallback{
			primary: primary, fallback: fallback,
		}).Search(requestDeadlineContext(t, time.Second), request)
		if !errors.Is(err, context.DeadlineExceeded) || fallback.calls != 0 ||
			len(response.PartialFailures) != 0 {
			t.Fatalf(
				"request=%#v response=%#v fallback=%d err=%v",
				request,
				response,
				fallback.calls,
				err,
			)
		}
	}
}

func TestProgressiveFallbackAcceptsShortPlainQueries(t *testing.T) {
	requests := []searchcore.Request{
		{
			Query: "Harborlight", Terms: []string{"Harborlight"},
			RankingFeatures: true, Verify: searchcore.VerifyIfExist,
		},
		{Query: "Harborlight skyline", Terms: []string{"Harborlight", "skyline"}},
		{
			Query: "Harborlight skyline dawn tide",
			Terms: []string{"Harborlight", "skyline", "dawn", "tide"},
		},
	}
	for _, request := range requests {
		if !progressiveFallbackEligible(request) {
			t.Fatalf("plain short query was rejected: %#v", request)
		}
	}
	if progressiveFallbackEligible(searchcore.Request{
		Query: "Harborlight skyline", Terms: []string{"Harborlight"},
	}) {
		t.Fatal("query whose parsed terms do not match its text was accepted")
	}
}

func TestLocalSearchDeadlineFallbackMarksResultsWhenProgressiveSearchFails(t *testing.T) {
	primary := &localDeadlineFallbackProbe{err: context.DeadlineExceeded}
	fallback := &localDeadlineFallbackProbe{err: errors.New("title query failed")}
	response, err := (localSearchDeadlineFallback{
		primary: primary, fallback: fallback,
	}).Search(requestDeadlineContext(t, 80*time.Millisecond), plainLocalFallbackRequest())
	if err != nil || primary.calls != 1 || fallback.calls != 1 ||
		len(response.Results) != 0 || len(response.PartialFailures) != 1 {
		t.Fatalf(
			"response=%#v primary=%d fallback=%d err=%v",
			response,
			primary.calls,
			fallback.calls,
			err,
		)
	}
}

func TestLocalSearchDeadlineFallbackDoesNotStartWithoutParentDeadline(t *testing.T) {
	primary := &localDeadlineFallbackProbe{err: context.DeadlineExceeded}
	fallback := &localDeadlineFallbackProbe{}
	response, err := (localSearchDeadlineFallback{
		primary: primary, fallback: fallback,
	}).Search(context.Background(), plainLocalFallbackRequest())
	if !errors.Is(err, context.DeadlineExceeded) || primary.calls != 1 || fallback.calls != 0 ||
		len(response.PartialFailures) != 0 {
		t.Fatalf(
			"response=%#v primary=%d fallback=%d err=%v",
			response,
			primary.calls,
			fallback.calls,
			err,
		)
	}
}

func TestLocalSearchDeadlineFallbackHandlesPartialAndExpiredBudgets(t *testing.T) {
	request := plainLocalFallbackRequest()
	partialPrimary := &localDeadlineFallbackProbe{
		response: searchcore.Response{
			Results:  []searchcore.Result{{URL: "https://primary.example/"}},
			Coverage: &searchcore.SearchCoverage{Complete: false},
		},
	}
	fallback := &localDeadlineFallbackProbe{
		response: searchcore.Response{
			Results: []searchcore.Result{{URL: "https://sample.example/"}},
		},
	}
	response, err := (localSearchDeadlineFallback{
		primary: partialPrimary, fallback: fallback,
	}).Search(t.Context(), request)
	if err != nil || partialPrimary.calls != 1 || fallback.calls != 1 ||
		len(response.Results) != 1 || response.Results[0].URL != "https://sample.example/" {
		t.Fatalf("no-deadline partial fallback=%+v err=%v", response, err)
	}

	primaryFailure := errors.New("local backend failed")
	partialPrimary = &localDeadlineFallbackProbe{
		response: searchcore.Response{
			Results: []searchcore.Result{{URL: "https://partial.example/"}},
		},
		err: primaryFailure,
	}
	response, err = (localSearchDeadlineFallback{
		primary: partialPrimary, fallback: &localDeadlineFallbackProbe{},
	}).Search(requestDeadlineContext(t, time.Second), request)
	if err != nil || len(response.Results) != 1 || len(response.PartialFailures) != 1 {
		t.Fatalf("partial primary result=%+v error=%v", response, err)
	}

	_, err = (localSearchDeadlineFallback{
		primary:  &localDeadlineFallbackProbe{err: primaryFailure},
		fallback: &localDeadlineFallbackProbe{},
	}).Search(requestDeadlineContext(t, time.Second), request)
	if !errors.Is(err, primaryFailure) {
		t.Fatalf("empty primary backend failure=%v", err)
	}

	canceled, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	fallback = &localDeadlineFallbackProbe{}
	_, err = (localSearchDeadlineFallback{
		primary: &localDeadlineFallbackProbe{}, fallback: fallback,
	}).Search(canceled, request)
	if !errors.Is(err, context.DeadlineExceeded) || fallback.calls != 0 {
		t.Fatalf("expired caller budget error=%v fallback calls=%d", err, fallback.calls)
	}
}

func TestLocalSearchNeedsRandomFallbackForIncompleteCoverage(t *testing.T) {
	if !localSearchNeedsRandomFallback(searchcore.Response{
		Coverage: &searchcore.SearchCoverage{Complete: false},
	}) {
		t.Fatal("incomplete coverage did not request randomized fallback")
	}
	if localSearchNeedsRandomFallback(searchcore.Response{
		Coverage: &searchcore.SearchCoverage{Complete: true},
	}) {
		t.Fatal("complete coverage requested randomized fallback")
	}
	if !localSearchNeedsRandomFallback(searchcore.Response{
		PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceLocalSearch,
			Reason: "local search failed",
		}},
	}) {
		t.Fatal("local-search failure did not request randomized fallback")
	}
	if localSearchNeedsRandomFallback(searchcore.Response{
		PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceRemoteYaCy,
			Reason: "peer search failed",
		}},
	}) {
		t.Fatal("remote peer failure incorrectly requested local randomized fallback")
	}
}

func requestDeadlineContext(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	t.Cleanup(cancel)

	return ctx
}

func plainLocalFallbackRequest() searchcore.Request {
	return searchcore.Request{
		Query:         "Needle",
		Terms:         []string{"Needle"},
		Source:        searchcore.SourceLocal,
		Limit:         10,
		ContentDomain: searchcore.ContentDomainText,
	}
}
