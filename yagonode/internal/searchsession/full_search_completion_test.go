package searchsession

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type completingShardSearcher struct {
	calls        atomic.Int32
	fullStarted  chan struct{}
	fullRelease  chan struct{}
	fullOnce     sync.Once
	fullResponse searchcore.Response
	fullErr      error
}

type heldProgressiveWalk struct {
	fullStarted    chan struct{}
	fullRelease    chan struct{}
	initialStopped chan struct{}
	fullOnce       sync.Once
}

type boundedFullSearcher struct {
	started chan time.Time
	release chan struct{}
	calls   atomic.Int32
}

func (s *boundedFullSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	if !searchcore.FullShardSearchRequired(ctx) ||
		searchcore.SearchProgressObserverFrom(ctx) != nil ||
		req.Source != searchcore.SourceLocal || req.AllowWebFallback {
		return searchcore.Response{}, errors.New("full search context was not isolated")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return searchcore.Response{}, errors.New("full search deadline is missing")
	}
	s.calls.Add(1)
	s.started <- deadline
	<-s.release

	return completedShardResponse(req), nil
}

func (s *heldProgressiveWalk) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	if searchcore.FullShardSearchRequired(ctx) {
		s.fullOnce.Do(func() { close(s.fullStarted) })
		select {
		case <-s.fullRelease:
			return completedShardResponse(req), nil
		case <-ctx.Done():
			return searchcore.Response{}, fmt.Errorf(
				"full test search stopped: %w",
				context.Cause(ctx),
			)
		}
	}
	empty := searchcore.Response{
		Request: req,
		Coverage: &searchcore.SearchCoverage{
			CompletedShards: 1, SampledShards: 1, TotalShards: 8,
		},
	}
	searchcore.ReportSearchProgress(ctx, searchcore.SearchProgress{
		Response: empty, CompletedShards: 1, SampledShards: 1, TotalShards: 8,
	})
	partial := sampledCompletionResponse(req)
	partial.Coverage.CompletedShards = 2
	partial.Coverage.SampledShards = 2
	searchcore.ReportSearchProgress(ctx, searchcore.SearchProgress{
		Response: partial, CompletedShards: 2, SampledShards: 2, TotalShards: 8,
	})
	<-ctx.Done()
	close(s.initialStopped)

	return partial, fmt.Errorf("sampled test search stopped: %w", context.Cause(ctx))
}

func (s *completingShardSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.calls.Add(1)
	if searchcore.FullShardSearchRequired(ctx) {
		s.fullOnce.Do(func() { close(s.fullStarted) })
		select {
		case <-s.fullRelease:
			response := s.fullResponse
			response.Request = req
			return response, s.fullErr
		case <-ctx.Done():
			return searchcore.Response{}, fmt.Errorf(
				"full test search stopped: %w",
				context.Cause(ctx),
			)
		}
	}
	partial := sampledCompletionResponse(req)
	searchcore.ReportSearchProgress(ctx, searchcore.SearchProgress{
		Response:        partial,
		CompletedShards: 8,
		SampledShards:   8,
		TotalShards:     8,
	})

	return partial, nil
}

func sampledCompletionResponse(req searchcore.Request) searchcore.Response {
	return searchcore.Response{
		Request:      req,
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title: "Sampled result", URL: "https://sample.example/result",
		}},
		Coverage: &searchcore.SearchCoverage{
			CompletedShards: 8, SampledShards: 8, TotalShards: 8, Complete: false,
		},
	}
}

func completedShardResponse(req searchcore.Request) searchcore.Response {
	return searchcore.Response{
		Request:      req,
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title: "Full result", URL: "https://full.example/result",
		}},
		Coverage: &searchcore.SearchCoverage{
			CompletedShards: 8, TotalShards: 8, Complete: true,
		},
	}
}

func TestStableWindowRefinesSampleAndReusesCompletedSearch(t *testing.T) {
	inner := &completingShardSearcher{
		fullStarted: make(chan struct{}),
		fullRelease: make(chan struct{}),
		fullResponse: searchcore.Response{
			TotalResults: 1,
			Results: []searchcore.Result{{
				Title: "Full result", URL: "https://full.example/result",
			}},
			Coverage: &searchcore.SearchCoverage{
				CompletedShards: 8, TotalShards: 8, Complete: true,
			},
		},
	}
	stable := NewStableWindow(inner)
	request := searchcore.Request{Query: "sample", Limit: 10, Source: searchcore.SourceLocal}
	first, err := stable.Search(t.Context(), request)
	if err != nil || len(first.Results) != 1 || first.Results[0].Title != "Sampled result" ||
		first.Coverage == nil || first.Coverage.Complete || len(first.PartialFailures) != 0 {
		t.Fatalf("first response=%+v error=%v", first, err)
	}
	select {
	case <-inner.fullStarted:
	case <-time.After(time.Second):
		t.Fatal("full shard search did not start")
	}
	key := sessionKey(request)
	stableSearcher := stable.(*stableSearcher)
	stableSearcher.mu.Lock()
	completion := stableSearcher.fullSearches[key]
	stableSearcher.mu.Unlock()
	if completion == nil {
		t.Fatal("full search completion was not registered")
	}

	second, err := stable.Search(t.Context(), request)
	if err != nil || len(second.Results) != 1 || second.Results[0].Title != "Sampled result" {
		t.Fatalf("concurrent response=%+v error=%v", second, err)
	}
	if got := inner.calls.Load(); got != 2 {
		t.Fatalf(
			"search calls while full search is held = %d, want one sample and one full search",
			got,
		)
	}

	close(inner.fullRelease)
	select {
	case <-completion.done:
	case <-time.After(time.Second):
		t.Fatal("full shard search did not finish")
	}

	repeated, err := stable.Search(t.Context(), request)
	if err != nil || len(repeated.Results) != 1 || repeated.Results[0].Title != "Full result" ||
		repeated.Coverage == nil || !repeated.Coverage.Complete || len(repeated.PartialFailures) != 0 {
		t.Fatalf("cached response=%+v error=%v", repeated, err)
	}
	if got := inner.calls.Load(); got != 2 {
		t.Fatalf("repeated query ran the searcher again: calls=%d", got)
	}
}

func TestStableWindowReturnsFirstShardSampleBeforeProgressiveWalkEnds(t *testing.T) {
	inner := &heldProgressiveWalk{
		fullStarted:    make(chan struct{}),
		fullRelease:    make(chan struct{}),
		initialStopped: make(chan struct{}),
	}
	stable := NewStableWindow(inner)
	request := searchcore.Request{Query: "responsive", Limit: 10, Source: searchcore.SourceLocal}
	result := make(chan searchcore.Response, 1)
	searchErr := make(chan error, 1)
	go func() {
		response, err := stable.Search(t.Context(), request)
		result <- response
		searchErr <- err
	}()
	select {
	case <-inner.fullStarted:
	case <-time.After(time.Second):
		t.Fatal("full shard refinement did not start after the first sample")
	}
	select {
	case response := <-result:
		if err := <-searchErr; err != nil || len(response.Results) != 1 ||
			response.Results[0].Title != "Sampled result" ||
			response.Coverage == nil || response.Coverage.Complete ||
			len(response.PartialFailures) != 0 {
			t.Fatalf("first-shard response=%+v error=%v", response, err)
		}
	case <-time.After(time.Second):
		t.Fatal("search waited for the full shard result instead of returning the sample")
	}
	select {
	case <-inner.initialStopped:
	case <-time.After(time.Second):
		t.Fatal("the redundant progressive shard walk did not stop")
	}
	key := sessionKey(request)
	stableSearcher := stable.(*stableSearcher)
	stableSearcher.mu.Lock()
	completion := stableSearcher.fullSearches[key]
	stableSearcher.mu.Unlock()
	if completion == nil {
		t.Fatal("full search completion was not registered")
	}
	close(inner.fullRelease)
	select {
	case <-completion.done:
	case <-time.After(time.Second):
		t.Fatal("full shard refinement did not finish")
	}
}

func TestStableWindowDoesNotCacheIncompleteFullSearch(t *testing.T) {
	inner := &completingShardSearcher{
		fullStarted:  make(chan struct{}),
		fullRelease:  make(chan struct{}),
		fullResponse: sampledCompletionResponse(searchcore.Request{}),
	}
	stable := NewStableWindow(inner)
	request := searchcore.Request{Query: "partial", Limit: 10, Source: searchcore.SourceLocal}
	if _, err := stable.Search(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-inner.fullStarted:
	case <-time.After(time.Second):
		t.Fatal("full shard search did not start")
	}
	key := sessionKey(request)
	stableSearcher := stable.(*stableSearcher)
	stableSearcher.mu.Lock()
	completion := stableSearcher.fullSearches[key]
	stableSearcher.mu.Unlock()
	if completion == nil {
		t.Fatal("full search completion was not registered")
	}
	close(inner.fullRelease)
	select {
	case <-completion.done:
	case <-time.After(time.Second):
		t.Fatal("incomplete full shard search did not finish")
	}
	if _, cached := stable.Recent(request); cached {
		t.Fatal("incomplete full search entered the session cache")
	}
	if got := inner.calls.Load(); got != 2 {
		t.Fatalf("calls after incomplete completion = %d, want 2", got)
	}
}

func TestStableWindowStreamingWaitsForFullSearchWhenItFinishes(t *testing.T) {
	inner := &completingShardSearcher{
		fullStarted:  make(chan struct{}),
		fullRelease:  make(chan struct{}),
		fullResponse: completedShardResponse(searchcore.Request{}),
	}
	stable := NewStableWindow(inner)
	request := searchcore.Request{Query: "streamed", Limit: 10, Source: searchcore.SourceLocal}
	ctx := searchcore.WithSearchProgressObserver(t.Context(), func(searchcore.SearchProgress) {})
	result := make(chan searchcore.Response, 1)
	searchErr := make(chan error, 1)
	go func() {
		response, err := stable.Search(ctx, request)
		result <- response
		searchErr <- err
	}()
	select {
	case <-inner.fullStarted:
	case <-time.After(time.Second):
		t.Fatal("streaming search did not start full shard refinement")
	}
	close(inner.fullRelease)
	select {
	case response := <-result:
		if err := <-searchErr; err != nil || len(response.Results) != 1 ||
			response.Results[0].Title != "Full result" || response.Coverage == nil ||
			!response.Coverage.Complete {
			t.Fatalf("streaming response=%+v error=%v", response, err)
		}
	case <-time.After(time.Second):
		t.Fatal("streaming search did not return the completed result")
	}
}

func TestDetachedCompletionStaysLocalAndDoesNotReplaceGlobalPartial(t *testing.T) {
	inner := &completingShardSearcher{
		fullStarted: make(chan struct{}),
		fullRelease: make(chan struct{}),
		fullResponse: searchcore.Response{
			TotalResults: 1,
			Results: []searchcore.Result{{
				Title: "Local full result", URL: "https://local.example/result",
			}},
			Coverage: &searchcore.SearchCoverage{
				CompletedShards: 8, TotalShards: 8, Complete: true,
			},
		},
	}
	stable := NewStableWindow(inner).(*stableSearcher)
	globalRequest := searchcore.Request{
		Query: "scoped", Limit: 10, Source: searchcore.SourceGlobal,
		AllowWebFallback: true,
	}
	partial, err := stable.Search(t.Context(), globalRequest)
	if err != nil || len(partial.Results) != 1 ||
		partial.Request.Source != searchcore.SourceGlobal {
		t.Fatalf("initial global response=%+v error=%v", partial, err)
	}
	select {
	case <-inner.fullStarted:
	case <-time.After(time.Second):
		t.Fatal("bounded local completion did not start")
	}
	key := sessionKey(globalRequest)
	stable.mu.Lock()
	completion := stable.fullSearches[key]
	stable.mu.Unlock()
	if completion == nil {
		t.Fatal("local completion was not registered")
	}
	close(inner.fullRelease)
	select {
	case <-completion.done:
	case <-time.After(time.Second):
		t.Fatal("local completion did not finish")
	}
	if completion.response.Request.Source != searchcore.SourceLocal ||
		completion.response.Request.AllowWebFallback {
		t.Fatalf("detached completion scope = %+v", completion.response.Request)
	}

	if got := stable.fullSearchResult(
		t.Context(),
		completion,
		partial,
		false,
	); len(got.Results) != 1 ||
		got.Results[0].Title != "Sampled result" ||
		got.Request.Source != searchcore.SourceGlobal {
		t.Fatalf("local completion replaced global partial: %+v", got)
	}
	localRequest := globalRequest
	localRequest.Source = searchcore.SourceLocal
	localRequest.AllowWebFallback = false
	local, ok := stable.Recent(localRequest)
	if !ok || len(local.Results) != 1 || local.Results[0].Title != "Local full result" ||
		local.Request.Source != searchcore.SourceLocal {
		t.Fatalf("local cached completion=%+v found=%v", local, ok)
	}
}

func TestStableWindowLimitsAndBoundsFullSearchCompletions(t *testing.T) {
	inner := &boundedFullSearcher{
		started: make(chan time.Time, 2),
		release: make(chan struct{}),
	}
	stable := NewStableWindow(inner).(*stableSearcher)
	request := searchcore.Request{Query: "bounded", Limit: 10, Source: searchcore.SourceLocal}
	partial := sampledCompletionResponse(request)
	start := func(key string) *fullSearchCompletion {
		return stable.startFullSearch(fullSearchRequest{
			ctx: t.Context(), key: key, request: request, pageRequest: request,
			searchDepth: 10, partialResponse: partial,
		})
	}
	assertBoundedDeadline := func() {
		t.Helper()
		select {
		case deadline := <-inner.started:
			remaining := time.Until(deadline)
			if remaining <= 0 || remaining > fullSearchCompletionBudget {
				t.Fatalf("full-search budget = %s, want a positive duration no greater than %s",
					remaining, fullSearchCompletionBudget)
			}
		case <-time.After(time.Second):
			t.Fatal("full search did not start")
		}
	}
	awaitCompletion := func(completion *fullSearchCompletion) {
		t.Helper()
		select {
		case <-completion.done:
		case <-time.After(time.Second):
			t.Fatal("full search did not finish")
		}
	}

	first := start("first")
	if first == nil {
		t.Fatal("first full search was refused")
	}
	assertBoundedDeadline()
	otherSearcher := NewStableWindow(inner).(*stableSearcher)
	if completion := otherSearcher.startFullSearch(fullSearchRequest{
		ctx: t.Context(), key: "other-searcher", request: request,
		pageRequest: request, searchDepth: 10, partialResponse: partial,
	}); completion != nil {
		t.Fatal("second searcher bypassed the process-wide completion limit")
	}
	if second := start("second"); second != nil {
		t.Fatal("second concurrent full search exceeded the one-task limit")
	}
	if calls := inner.calls.Load(); calls != 1 {
		t.Fatalf("full searches admitted while saturated = %d, want 1", calls)
	}
	close(inner.release)
	awaitCompletion(first)

	third := start("after-release")
	if third == nil {
		t.Fatal("full-search capacity did not recover after completion")
	}
	assertBoundedDeadline()
	awaitCompletion(third)
	if calls := inner.calls.Load(); calls != 2 {
		t.Fatalf("full searches after capacity recovery = %d, want 2", calls)
	}
}
