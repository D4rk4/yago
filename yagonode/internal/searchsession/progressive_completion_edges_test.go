package searchsession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type panicSearchSessionSearcher struct{}

func (panicSearchSessionSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	panic("search panic")
}

type burstProgressSearcher struct{}

func (burstProgressSearcher) Search(
	ctx context.Context,
	request searchcore.Request,
) (searchcore.Response, error) {
	partial := sampledCompletionResponse(request)
	searchcore.ReportSearchProgress(ctx, searchcore.SearchProgress{Response: partial})
	searchcore.ReportSearchProgress(ctx, searchcore.SearchProgress{Response: partial})
	return partial, nil
}

func TestStableWindowProgressiveCompletionEdgePaths(t *testing.T) {
	request := searchcore.Request{Query: "sample", Limit: 5}

	if incompleteRefresh(searchcore.Response{
		Coverage: &searchcore.SearchCoverage{Complete: false},
	}) != true {
		t.Fatal("incomplete shard coverage was not recognized")
	}
	if incompleteRefresh(searchcore.Response{
		PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceRemoteYaCy,
		}},
	}) {
		t.Fatal("non-primary remote peer failure was classified as an incomplete refresh")
	}

	searchErr := errors.New("search unavailable")
	failed := NewStableWindow(&refreshSequenceSearcher{errors: []error{searchErr}})
	_, err := failed.Search(t.Context(), searchcore.Request{Query: "miss", Offset: 2, Limit: 1})
	if !errors.Is(err, searchErr) {
		t.Fatalf("uncached deep-page error=%v", err)
	}

	cancelCause := errors.New("caller left")
	canceled, cancel := context.WithCancelCause(t.Context())
	cancel(cancelCause)
	uncached := NewStableWindow(&refreshSequenceSearcher{responses: []searchcore.Response{{
		Results: []searchcore.Result{{URL: "https://uncached.example/"}},
	}}})
	response, err := uncached.Search(
		canceled,
		searchcore.Request{Query: "uncached", Offset: 2, Limit: 1},
	)
	if !errors.Is(err, cancelCause) || len(response.Results) != 0 {
		t.Fatalf("canceled uncached page=%+v error=%v", response, err)
	}

	stable := NewStableWindow(&refreshSequenceSearcher{}).(*stableSearcher)
	if _, ok := stable.refinedFirstPage(t.Context(), sessionKey(request), request); ok {
		t.Fatal("missing full-search completion returned a refined page")
	}
	stable.fullSearches[sessionKey(request)] = &fullSearchCompletion{done: make(chan struct{})}
	if _, ok := stable.refinedFirstPage(t.Context(), sessionKey(request), request); ok {
		t.Fatal("completion without a partial response returned a refined page")
	}

	globalRequest := searchcore.Request{
		Query: request.Query, Limit: request.Limit, Source: searchcore.SourceGlobal,
	}
	localPartial := sampledCompletionResponse(globalRequest)
	localPartial.Request.Source = searchcore.SourceLocal
	globalKey := sessionKey(globalRequest)
	stable.fullSearches[globalKey] = &fullSearchCompletion{
		done: make(chan struct{}), partialResponse: &localPartial,
	}
	if _, ok := stable.refinedFirstPage(t.Context(), globalKey, globalRequest); ok {
		t.Fatal("local partial response crossed into a global search session")
	}

	if _, err := NewStableWindow(
		panicSearchSessionSearcher{},
	).Search(t.Context(), request); err == nil {
		t.Fatal("initial search panic was not reported")
	}
	burst := NewStableWindow(burstProgressSearcher{}).(*stableSearcher)
	if _, err := burst.Search(t.Context(), request); err != nil {
		t.Fatalf("burst progressive search: %v", err)
	}
	if completion := burst.activeFullSearch(sessionKey(request)); completion != nil {
		select {
		case <-completion.done:
		case <-time.After(time.Second):
			t.Fatal("burst progressive completion did not release its process-wide slot")
		}
	}
}

func TestStableWindowFinishInitialSearchBranches(t *testing.T) {
	request := searchcore.Request{Query: "partial", Limit: 5}
	key := sessionKey(request)
	plan := fullSearchRequest{
		ctx: t.Context(), key: key, request: request, pageRequest: request, searchDepth: 5,
	}
	stable := NewStableWindow(&refreshSequenceSearcher{}).(*stableSearcher)
	stable.fullSearches[key] = &fullSearchCompletion{done: make(chan struct{})}
	partial := sampledCompletionResponse(request)
	response, err := stable.finishInitialSearch(plan, initialSearchResult{response: partial}, false)
	if err != nil || response.Coverage == nil || response.Coverage.Complete {
		t.Fatalf("sampled initial result=%+v error=%v", response, err)
	}

	otherErr := errors.New("backend failed")
	if _, err := stable.finishInitialSearch(
		plan,
		initialSearchResult{err: otherErr},
		false,
	); !errors.Is(
		err,
		otherErr,
	) {
		t.Fatalf("initial backend error=%v", err)
	}
	if _, err := stable.finishInitialSearch(
		plan,
		initialSearchResult{err: context.DeadlineExceeded},
		false,
	); !errors.Is(
		err,
		context.DeadlineExceeded,
	) {
		t.Fatalf("initial deadline error=%v", err)
	}

	cause := errors.New("request canceled")
	canceled, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	plan.ctx = canceled
	response, err = stable.finishInitialSearch(plan, initialSearchResult{
		response: completedShardResponse(request),
	}, false)
	if !errors.Is(err, cause) || len(response.Results) == 0 {
		t.Fatalf("canceled initial response=%+v error=%v", response, err)
	}

	plan.ctx = t.Context()
	plan.partialResponse = searchcore.Response{}
	stable.store(key, searchcore.Response{
		Request: request,
		Results: []searchcore.Result{{URL: "https://recent.example/"}},
	}, 5)
	failure := searchcore.PartialFailure{
		Source: searchcore.PartialFailureSourceLocalSearch,
		Reason: "refresh incomplete",
	}
	response, err = stable.finishInitialSearch(
		plan,
		initialSearchResult{response: searchcore.Response{
			PartialFailures: []searchcore.PartialFailure{failure},
		}},
		false,
	)
	if err != nil || len(response.Results) != 1 || len(response.PartialFailures) != 1 {
		t.Fatalf("recent success after incomplete refresh=%+v error=%v", response, err)
	}

	empty := NewStableWindow(&refreshSequenceSearcher{}).(*stableSearcher)
	response, err = empty.finishInitialSearch(
		plan,
		initialSearchResult{response: searchcore.Response{
			PartialFailures: []searchcore.PartialFailure{failure},
		}},
		false,
	)
	if err != nil || len(response.Results) != 0 || len(response.PartialFailures) != 1 {
		t.Fatalf("uncached incomplete refresh=%+v error=%v", response, err)
	}
}

func TestStableWindowCanceledInitialSearchAndFullSearchResultBranches(t *testing.T) {
	request := searchcore.Request{Query: "partial", Limit: 5}
	key := sessionKey(request)
	plan := fullSearchRequest{
		ctx: t.Context(), key: key, request: request, pageRequest: request, searchDepth: 5,
	}
	stable := NewStableWindow(&refreshSequenceSearcher{}).(*stableSearcher)
	stable.fullSearches[key] = &fullSearchCompletion{done: make(chan struct{})}
	partial := sampledCompletionResponse(request)
	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("caller deadline")
	cancel(cause)
	response, err := stable.finishCanceledInitialSearch(
		ctx, plan, channelOf(partial), make(chan initialSearchResult), false,
	)
	if err != nil || len(response.Results) != 1 {
		t.Fatalf("queued progressive sample=%+v error=%v", response, err)
	}

	response, err = stable.finishCanceledInitialSearch(
		ctx,
		plan,
		make(chan searchcore.Response),
		channelOf(initialSearchResult{response: partial}),
		false,
	)
	if err != nil || len(response.Results) != 1 {
		t.Fatalf("completed progressive sample=%+v error=%v", response, err)
	}
	response, err = stable.finishCanceledInitialSearch(
		ctx, plan, make(chan searchcore.Response), channelOf(initialSearchResult{}), false,
	)
	if !errors.Is(err, cause) || len(response.Results) != 0 {
		t.Fatalf("completed non-progressive canceled search=%+v error=%v", response, err)
	}
	response, err = stable.finishCanceledInitialSearch(
		ctx, plan, make(chan searchcore.Response), make(chan initialSearchResult), false,
	)
	if !errors.Is(err, cause) || len(response.Results) != 0 {
		t.Fatalf("canceled search without a result=%+v error=%v", response, err)
	}
}

func TestStableWindowFullSearchResultBranches(t *testing.T) {
	request := searchcore.Request{Query: "partial", Limit: 5}
	stable := NewStableWindow(&refreshSequenceSearcher{}).(*stableSearcher)
	partial := sampledCompletionResponse(request)
	if got := stable.fullSearchResult(t.Context(), nil, partial, true); len(got.Results) != 1 {
		t.Fatalf("nil full-search completion=%+v", got)
	}
	completed := &fullSearchCompletion{
		done:     make(chan struct{}),
		response: completedShardResponse(request),
	}
	close(completed.done)
	if got := stable.fullSearchResult(
		t.Context(),
		completed,
		partial,
		true,
	); !responseIsComplete(
		got,
	) {
		t.Fatalf("completed full search=%+v", got)
	}
	incomplete := &fullSearchCompletion{done: make(chan struct{}), response: partial}
	close(incomplete.done)
	if got := stable.fullSearchResult(
		t.Context(),
		incomplete,
		partial,
		true,
	); got.Coverage == nil ||
		got.Coverage.Complete {
		t.Fatalf("incomplete full search did not preserve sample: %+v", got)
	}
	if got := stable.fullSearchResult(
		t.Context(),
		completed,
		partial,
		false,
	); !responseIsComplete(
		got,
	) {
		t.Fatalf("completed nonblocking full search=%+v", got)
	}
	if got := stable.fullSearchResult(
		t.Context(),
		incomplete,
		partial,
		false,
	); got.Coverage == nil ||
		got.Coverage.Complete {
		t.Fatalf("incomplete nonblocking full search=%+v", got)
	}
	waiting := &fullSearchCompletion{done: make(chan struct{})}
	canceledContext, cancelWait := context.WithCancel(t.Context())
	cancelWait()
	if got := stable.fullSearchResult(
		canceledContext,
		waiting,
		partial,
		true,
	); got.Coverage == nil ||
		got.Coverage.Complete {
		t.Fatalf("canceled wait did not preserve sample: %+v", got)
	}
	if _, ok := stable.fullSearchPartial(&fullSearchCompletion{}); ok {
		t.Fatal("missing partial response was reported as available")
	}
}

func TestStableWindowStartFullSearchExistingBusyAndPanicPaths(t *testing.T) {
	request := searchcore.Request{Query: "start", Limit: 5}
	partial := sampledCompletionResponse(request)
	plan := fullSearchRequest{
		ctx: t.Context(), key: "same", request: request, pageRequest: request,
		searchDepth: 5, partialResponse: partial,
	}
	stable := NewStableWindow(&refreshSequenceSearcher{}).(*stableSearcher)
	existing := &fullSearchCompletion{done: make(chan struct{})}
	stable.fullSearches["same"] = existing
	if got := stable.startFullSearch(plan); got != existing || existing.partialResponse == nil {
		t.Fatalf("existing full search=%p partial=%+v", got, existing.partialResponse)
	}
	select {
	case fullSearchCompletionSlots <- struct{}{}:
	default:
		t.Fatal("process-wide completion slot leaked from another test")
	}
	plan.key = "busy"
	if got := stable.startFullSearch(plan); got != nil {
		t.Fatalf("busy full-search slot was allocated: %p", got)
	}
	<-fullSearchCompletionSlots

	panicking := NewStableWindow(panicSearchSessionSearcher{}).(*stableSearcher)
	plan.key = "panic"
	completion := panicking.startFullSearch(plan)
	if completion == nil {
		t.Fatal("full-search completion was not allocated")
	}
	<-completion.done
	if completion.err == nil {
		t.Fatal("full-search panic was not recorded")
	}
}

func channelOf[T any](value T) <-chan T {
	channel := make(chan T, 1)
	channel <- value

	return channel
}
