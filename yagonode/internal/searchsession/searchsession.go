// Package searchsession keeps paging stable by caching assembled result prefixes
// and extending them in bounded windows when a deeper page needs more results.
package searchsession

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	sessionDepth    = 50
	maxSessionDepth = searchcore.MaximumPublicResultHorizon
	// DefaultSessionTTL is how long a materialized result window stays
	// navigable. It is operator-tunable because it is pure node-local retention
	// policy: a longer window lets a reader page further back at the cost of
	// held memory, bounded independently by sessionCacheMaximumBytes.
	DefaultSessionTTL                      = 5 * time.Minute
	maxSessions                            = 128
	sessionCacheMaximumBytes               = 32 << 20
	fullSearchCompletionBudget             = 30 * time.Second
	maximumConcurrentFullSearchCompletions = 1
)

// clock feeds session expiry; tests substitute a scripted time.
var clock = time.Now

var fullSearchCompletionSlots = make(chan struct{}, maximumConcurrentFullSearchCompletions)

type session struct {
	extension   chan struct{}
	visible     atomic.Pointer[sessionWindow]
	key         string
	results     []searchcore.Result
	failures    []searchcore.PartialFailure
	total       int
	exhausted   bool
	searchDepth int
	recovered   string
	didYouMean  string
	facets      []searchcore.FacetGroup
	coverage    *searchcore.SearchCoverage
	refined     bool
	expires     time.Time
	element     *list.Element
	retained    int
}

type stableSearcher struct {
	inner searchcore.Searcher

	mu       sync.Mutex
	sessions map[string]*session
	order    *list.List
	retained int
	limit    int
	// retention is read per stored session so a console change to the window
	// lifetime applies to the next search rather than after a restart.
	retention    func() time.Duration
	fullSearches map[string]*fullSearchCompletion
}

type fullSearchCompletion struct {
	done            chan struct{}
	response        searchcore.Response
	partialResponse *searchcore.Response
	err             error
}

type initialSearchResult struct {
	response searchcore.Response
	err      error
}

type fullSearchRequest struct {
	ctx             context.Context
	key             string
	request         searchcore.Request
	pageRequest     searchcore.Request
	searchDepth     int
	partialResponse searchcore.Response
}

type RecentWindow interface {
	Recent(searchcore.Request) (searchcore.Response, bool)
}

type StableWindow interface {
	searchcore.Searcher
	RecentWindow
}

// WithStableWindow decorates the searcher with the per-query session cache.
func WithStableWindow(inner searchcore.Searcher) searchcore.Searcher {
	return NewStableWindow(inner)
}

func NewStableWindow(inner searchcore.Searcher, retention ...func() time.Duration) StableWindow {
	return &stableSearcher{
		inner:        inner,
		sessions:     map[string]*session{},
		order:        list.New(),
		limit:        sessionCacheMaximumBytes,
		retention:    selectSessionRetention(retention),
		fullSearches: make(map[string]*fullSearchCompletion),
	}
}

// selectSessionRetention resolves the live retention source; callers that pass
// none keep the default so existing constructions are unchanged.
func selectSessionRetention(sources []func() time.Duration) func() time.Duration {
	if len(sources) == 0 || sources[0] == nil {
		return func() time.Duration { return DefaultSessionTTL }
	}

	return func() time.Duration {
		if value := sources[0](); value > 0 {
			return value
		}

		return DefaultSessionTTL
	}
}

func (s *stableSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	key := sessionKey(req)
	if req.Offset == 0 {
		if response, ok := s.refinedFirstPage(ctx, key, req); ok {
			return response, nil
		}
	}
	if req.Offset > 0 {
		if cached, ok := s.lookup(key); ok {
			if err := s.extend(ctx, cached, req); err != nil {
				return cached.respond(req), fmt.Errorf("extend session search: %w", err)
			}

			return cached.respond(req), nil
		}
	}

	targetDepth := requestedSearchDepth(req)
	deep := req
	deep.Offset = 0
	deep.Limit = retrievalDepth(targetDepth)
	if req.Offset == 0 {
		return s.searchFirstPage(ctx, key, deep, req, targetDepth)
	}
	resp, err := s.inner.Search(ctx, deep)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("session search: %w", err)
	}
	if cause := context.Cause(ctx); cause != nil {
		return uncachedSessionPage(resp, req), fmt.Errorf("session search: %w", cause)
	}
	stored := s.store(key, resp, targetDepth)
	if req.Offset > 0 {
		if err := s.extend(ctx, stored, req); err != nil {
			return stored.respond(req), fmt.Errorf("extend new session search: %w", err)
		}
	}

	return stored.respond(req), nil
}

func (s *stableSearcher) refinedFirstPage(
	ctx context.Context,
	key string,
	request searchcore.Request,
) (searchcore.Response, bool) {
	if cached, ok := s.lookup(key); ok && cached.refined {
		return cached.respond(request), true
	}
	completion := s.activeFullSearch(key)
	if completion == nil {
		return searchcore.Response{}, false
	}
	partial, ok := s.fullSearchPartial(completion)
	if !ok {
		return searchcore.Response{}, false
	}
	if partial.Request.Source != request.Source {
		return searchcore.Response{}, false
	}
	searchcore.ReportSearchProgress(ctx, searchProgressFor(partial))

	return s.fullSearchResult(
		ctx,
		completion,
		partial,
		searchcore.SearchProgressObserverFrom(ctx) != nil,
	), true
}

func (s *stableSearcher) searchFirstPage(
	ctx context.Context,
	key string,
	request searchcore.Request,
	pageRequest searchcore.Request,
	searchDepth int,
) (searchcore.Response, error) {
	observer := searchcore.SearchProgressObserverFrom(ctx)
	plan := fullSearchRequest{
		ctx:         ctx,
		key:         key,
		request:     request,
		pageRequest: pageRequest,
		searchDepth: searchDepth,
	}
	initialContext, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan initialSearchResult, 1)
	samples := make(chan searchcore.Response, 1)
	progressContext := searchcore.WithSearchProgressObserver(
		initialContext,
		func(progress searchcore.SearchProgress) {
			if observer != nil {
				observer(progress)
			}
			if !progressivePartialCoverage(progress.Response) {
				return
			}
			partial := uncachedSessionPage(progress.Response, pageRequest)
			select {
			case samples <- partial:
			default:
			}
		},
	)
	go func() {
		results <- s.runInitialSearch(progressContext, request)
	}()
	for {
		select {
		case partial := <-samples:
			cancel()
			return s.returnPartialSearch(plan, partial, observer != nil)
		case outcome := <-results:
			return s.finishInitialSearch(plan, outcome, observer != nil)
		case <-ctx.Done():
			cancel()
			return s.finishCanceledInitialSearch(ctx, plan, samples, results, observer != nil)
		}
	}
}

func (s *stableSearcher) runInitialSearch(
	ctx context.Context,
	request searchcore.Request,
) (outcome initialSearchResult) {
	defer func() {
		if recover() != nil {
			outcome.err = fmt.Errorf("initial search panicked")
		}
	}()
	outcome.response, outcome.err = s.inner.Search(ctx, request)

	return outcome
}

func (s *stableSearcher) returnPartialSearch(
	plan fullSearchRequest,
	partial searchcore.Response,
	wait bool,
) (searchcore.Response, error) {
	plan.partialResponse = partial
	completion := s.startFullSearch(plan)

	return s.fullSearchResult(plan.ctx, completion, partial, wait), nil
}

func (s *stableSearcher) finishInitialSearch(
	plan fullSearchRequest,
	outcome initialSearchResult,
	waitForCompletion bool,
) (searchcore.Response, error) {
	if outcome.err != nil && !errors.Is(outcome.err, context.DeadlineExceeded) {
		return searchcore.Response{}, fmt.Errorf("session search: %w", outcome.err)
	}
	if progressivePartialCoverage(outcome.response) {
		partial := uncachedSessionPage(outcome.response, plan.pageRequest)
		return s.returnPartialSearch(plan, partial, waitForCompletion)
	}
	if cause := context.Cause(plan.ctx); cause != nil {
		return uncachedSessionPage(outcome.response, plan.pageRequest),
			fmt.Errorf("session search: %w", cause)
	}
	if outcome.err != nil {
		return searchcore.Response{}, fmt.Errorf("session search: %w", outcome.err)
	}
	if incompleteRefresh(outcome.response) {
		if recent, ok := s.Recent(plan.pageRequest); ok {
			return responseWithRefreshFailures(
				recent,
				outcome.response.PartialFailures,
			), nil
		}
		return uncachedSessionPage(outcome.response, plan.pageRequest), nil
	}
	stored := s.store(plan.key, outcome.response, plan.searchDepth)

	return stored.respond(plan.pageRequest), nil
}

func (s *stableSearcher) finishCanceledInitialSearch(
	ctx context.Context,
	plan fullSearchRequest,
	samples <-chan searchcore.Response,
	results <-chan initialSearchResult,
	waitForCompletion bool,
) (searchcore.Response, error) {
	select {
	case partial := <-samples:
		return s.returnPartialSearch(plan, partial, waitForCompletion)
	case outcome := <-results:
		if progressivePartialCoverage(outcome.response) {
			partial := uncachedSessionPage(outcome.response, plan.pageRequest)
			return s.returnPartialSearch(plan, partial, waitForCompletion)
		}
		return uncachedSessionPage(outcome.response, plan.pageRequest),
			fmt.Errorf("session search: %w", context.Cause(ctx))
	default:
		return searchcore.Response{}, fmt.Errorf("session search: %w", context.Cause(ctx))
	}
}

func (s *stableSearcher) fullSearchResult(
	ctx context.Context,
	completion *fullSearchCompletion,
	partial searchcore.Response,
	wait bool,
) searchcore.Response {
	if completion == nil {
		return partial
	}
	if wait {
		select {
		case <-completion.done:
			if completion.err == nil && responseIsComplete(completion.response) &&
				completion.response.Request.Source == partial.Request.Source {
				return completion.response
			}
			return partial
		case <-ctx.Done():
		}
	} else {
		select {
		case <-completion.done:
			if completion.err == nil && responseIsComplete(completion.response) &&
				completion.response.Request.Source == partial.Request.Source {
				return completion.response
			}
		default:
		}
	}

	return partial
}

func (s *stableSearcher) activeFullSearch(key string) *fullSearchCompletion {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.fullSearches[key]
}

func (s *stableSearcher) fullSearchPartial(
	completion *fullSearchCompletion,
) (searchcore.Response, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if completion.partialResponse == nil {
		return searchcore.Response{}, false
	}

	return *sessionResponseCopy(*completion.partialResponse), true
}

func sessionResponseCopy(response searchcore.Response) *searchcore.Response {
	cloned := uncachedSessionPage(response, response.Request)

	return &cloned
}

func searchProgressFor(response searchcore.Response) searchcore.SearchProgress {
	progress := searchcore.SearchProgress{Response: response}
	if response.Coverage != nil {
		progress.CompletedShards = response.Coverage.CompletedShards
		progress.SampledShards = response.Coverage.SampledShards
		progress.TotalShards = response.Coverage.TotalShards
		progress.Complete = response.Coverage.Complete
	}

	return progress
}

func progressivePartialCoverage(response searchcore.Response) bool {
	return response.Coverage != nil && !response.Coverage.Complete &&
		response.Coverage.CompletedShards > 0 && response.Coverage.SampledShards > 0 &&
		len(response.Results) > 0
}

func (s *stableSearcher) startFullSearch(plan fullSearchRequest) *fullSearchCompletion {
	ctx := plan.ctx
	key := plan.key
	request := plan.request
	s.mu.Lock()
	if current, found := s.fullSearches[key]; found {
		current.partialResponse = sessionResponseCopy(plan.partialResponse)
		s.mu.Unlock()

		return current
	}
	select {
	case fullSearchCompletionSlots <- struct{}{}:
	default:
		s.mu.Unlock()

		return nil
	}
	completion := &fullSearchCompletion{
		done:            make(chan struct{}),
		partialResponse: sessionResponseCopy(plan.partialResponse),
	}
	s.fullSearches[key] = completion
	s.mu.Unlock()

	background := context.WithoutCancel(ctx)
	background = searchcore.WithoutSearchProgressObserver(background)
	background = searchcore.WithFullShardSearch(background)
	background, cancel := context.WithTimeout(background, fullSearchCompletionBudget)
	go func() {
		defer func() {
			if recover() != nil {
				completion.err = fmt.Errorf("full search completion panicked")
			}
			cancel()
			<-fullSearchCompletionSlots
			s.mu.Lock()
			delete(s.fullSearches, key)
			s.mu.Unlock()
			close(completion.done)
		}()
		request.Source = searchcore.SourceLocal
		request.AllowWebFallback = false
		response, err := s.inner.Search(background, request)
		if err == nil && responseIsComplete(response) {
			response.Request = request
			localKey := sessionKey(request)
			localPageRequest := plan.pageRequest
			localPageRequest.Source = searchcore.SourceLocal
			localPageRequest.AllowWebFallback = false
			stored := s.storeRefined(localKey, response, plan.searchDepth)
			response = stored.respond(localPageRequest)
		}
		completion.response = response
		completion.err = err
	}()

	return completion
}

func responseIsComplete(response searchcore.Response) bool {
	return len(response.PartialFailures) == 0 &&
		(response.Coverage == nil || response.Coverage.Complete)
}

func (s *stableSearcher) Recent(req searchcore.Request) (searchcore.Response, bool) {
	entry, ok := s.lookup(sessionKey(req))
	if !ok {
		return searchcore.Response{}, false
	}
	response := entry.respond(req)
	if len(response.Results) == 0 {
		return searchcore.Response{}, false
	}

	return response, true
}

func (s *stableSearcher) lookup(key string) (*session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked(clock())
	entry, ok := s.sessions[key]
	if !ok {
		return nil, false
	}
	s.order.MoveToFront(entry.element)

	return entry, true
}

func (s *stableSearcher) store(key string, resp searchcore.Response, searchDepth int) *session {
	return s.storeWithCompletion(key, resp, searchDepth, false)
}

func (s *stableSearcher) storeRefined(
	key string,
	resp searchcore.Response,
	searchDepth int,
) *session {
	return s.storeWithCompletion(key, resp, searchDepth, true)
}

func (s *stableSearcher) storeWithCompletion(
	key string,
	resp searchcore.Response,
	searchDepth int,
	refined bool,
) *session {
	now := clock()
	key = strings.Clone(key)
	resp.Results = boundedResults(resp.Results, retrievalDepth(searchDepth))
	// A failure that only describes the caller's query does not leave coverage
	// unsettled, so it must not keep a complete answer from being marked
	// exhausted -- an unexhausted session is itself enough to make every empty
	// page read as an answer the node cannot vouch for.
	exhausted := resp.LostSourceFailures() == 0 && resp.TotalResults <= len(resp.Results)
	total := advertisedTotal(resp)
	if exhausted {
		total = len(resp.Results)
	}
	entry := &session{
		extension:   make(chan struct{}, 1),
		key:         key,
		results:     resp.Results,
		failures:    cloneSessionFailures(resp.PartialFailures),
		total:       total,
		exhausted:   exhausted,
		searchDepth: searchDepth,
		recovered:   strings.Clone(resp.Recovered),
		didYouMean:  strings.Clone(resp.DidYouMean),
		facets:      cloneSessionFacets(resp.Facets),
		coverage:    cloneSessionCoverage(resp.Coverage),
		refined:     refined && responseIsComplete(resp),
		expires:     now.Add(s.retention()),
	}
	entry.retained = retainedSessionBytes(entry)
	entry.replaceVisibleWindowLocked()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked(now)
	if existing, ok := s.sessions[key]; ok {
		s.removeLocked(existing)
	}
	if entry.retained > s.limit {
		return entry
	}
	entry.element = s.order.PushFront(entry)
	s.sessions[key] = entry
	s.retained += entry.retained
	s.enforceRetentionLocked()

	return entry
}

func (s *stableSearcher) removeLocked(entry *session) {
	s.order.Remove(entry.element)
	delete(s.sessions, entry.key)
	s.retained -= entry.retained
}

func (e *session) respond(req searchcore.Request) searchcore.Response {
	return e.visible.Load().respond(req)
}

// sessionKey canonicalizes every request field that changes the result set;
// paging fields stay out so all pages of one query share a session.
func sessionKey(req searchcore.Request) string {
	req.Offset = 0
	req.Limit = 0
	encoded, _ := json.Marshal(req)

	return string(encoded)
}
