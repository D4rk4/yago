// Package searchsession keeps paging stable: like YaCy's SearchEventCache, the
// first page of a query runs one deep federated search and caches the assembled
// result list; later pages slice that cached list instead of re-running the
// fan-out (whose random DHT peer sample would reshuffle every page). The
// reported total is the number of results actually collected and pageable, so
// pagers never promise pages that would come up empty.
package searchsession

import (
	"container/list"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	// sessionDepth is how many assembled results one search session holds — the
	// paging horizon (e.g. 20 portal pages of 10).
	sessionDepth = 200
	sessionTTL   = 5 * time.Minute
	maxSessions  = 128
)

// clock feeds session expiry; tests substitute a scripted time.
var clock = time.Now

type session struct {
	key        string
	results    []searchcore.Result
	failures   []searchcore.PartialFailure
	total      int
	recovered  string
	didYouMean string
	facets     []searchcore.FacetGroup
	expires    time.Time
	element    *list.Element
}

type stableSearcher struct {
	inner searchcore.Searcher

	mu       sync.Mutex
	sessions map[string]*session
	order    *list.List
}

// WithStableWindow decorates the searcher with the per-query session cache.
func WithStableWindow(inner searchcore.Searcher) searchcore.Searcher {
	return &stableSearcher{
		inner:    inner,
		sessions: map[string]*session{},
		order:    list.New(),
	}
}

func (s *stableSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	key := sessionKey(req)
	// A fresh query (page one) always re-runs the search so a repeated query
	// sees new content; deeper pages serve the session that page one built.
	if req.Offset > 0 {
		if cached, ok := s.lookup(key); ok {
			return cached.respond(req), nil
		}
	}

	deep := req
	deep.Offset = 0
	deep.Limit = sessionDepth
	resp, err := s.inner.Search(ctx, deep)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("session search: %w", err)
	}
	stored := s.store(key, resp)

	return stored.respond(req), nil
}

func (s *stableSearcher) lookup(key string) (*session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.sessions[key]
	if !ok {
		return nil, false
	}
	if clock().After(entry.expires) {
		s.removeLocked(entry)

		return nil, false
	}
	s.order.MoveToFront(entry.element)

	return entry, true
}

func (s *stableSearcher) store(key string, resp searchcore.Response) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.sessions[key]; ok {
		s.removeLocked(existing)
	}
	entry := &session{
		key:        key,
		results:    resp.Results,
		failures:   resp.PartialFailures,
		total:      len(resp.Results),
		recovered:  resp.Recovered,
		didYouMean: resp.DidYouMean,
		facets:     resp.Facets,
		expires:    clock().Add(sessionTTL),
	}
	entry.element = s.order.PushFront(entry)
	s.sessions[key] = entry
	for len(s.sessions) > maxSessions {
		oldest, _ := s.order.Back().Value.(*session)
		s.removeLocked(oldest)
	}

	return entry
}

func (s *stableSearcher) removeLocked(entry *session) {
	s.order.Remove(entry.element)
	delete(s.sessions, entry.key)
}

// respond slices the session window for the request and reports the honest
// pageable total, so no pager link ever leads to an empty page.
func (e *session) respond(req searchcore.Request) searchcore.Response {
	limit := req.Limit
	if limit <= 0 {
		limit = searchcore.DefaultPublicLimit
	}
	start := req.Offset
	if start > len(e.results) {
		start = len(e.results)
	}
	end := start + limit
	if end > len(e.results) {
		end = len(e.results)
	}

	return searchcore.Response{
		Request:         req,
		TotalResults:    e.total,
		Results:         e.results[start:end],
		PartialFailures: e.failures,
		Recovered:       e.recovered,
		DidYouMean:      e.didYouMean,
		Facets:          e.facets,
	}
}

// sessionKey canonicalizes every request field that changes the result set;
// paging fields stay out so all pages of one query share a session.
func sessionKey(req searchcore.Request) string {
	var key strings.Builder
	fmt.Fprintf(&key, "%s|%s|%s|%s|", req.Query, req.Source, req.ContentDomain, req.Language)
	fmt.Fprintf(&key, "%s|%s|%s|%s|%s|", req.SiteHost, req.InURL, req.TLD, req.FileType, req.Author)
	fmt.Fprintf(&key, "%s|%s|%s|", req.URLMaskFilter, req.PreferMaskFilter, req.Navigation)
	fmt.Fprintf(&key, "%v|%v|%v|%v|", req.SortByDate, req.Near, req.Fuzzy, req.AllowWebFallback)
	fmt.Fprintf(&key, "%d|%d|", req.MinDate.Unix(), req.MaxDate.Unix())
	fmt.Fprintf(&key, "%s|%s|%s",
		strings.Join(req.Terms, " "),
		strings.Join(req.ExcludedTerms, " "),
		strings.Join(req.Phrases, " "),
	)

	return key.String()
}
