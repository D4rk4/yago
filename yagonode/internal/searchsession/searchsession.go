// Package searchsession keeps paging stable by caching assembled result prefixes
// and extending them in bounded windows when a deeper page needs more results.
package searchsession

import (
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	sessionDepth             = 50
	maxSessionDepth          = 500
	sessionTTL               = 5 * time.Minute
	maxSessions              = 128
	sessionCacheMaximumBytes = 32 << 20
)

// clock feeds session expiry; tests substitute a scripted time.
var clock = time.Now

type session struct {
	windowMu    sync.RWMutex
	key         string
	results     []searchcore.Result
	failures    []searchcore.PartialFailure
	total       int
	searchDepth int
	recovered   string
	didYouMean  string
	facets      []searchcore.FacetGroup
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

func NewStableWindow(inner searchcore.Searcher) StableWindow {
	return &stableSearcher{
		inner:    inner,
		sessions: map[string]*session{},
		order:    list.New(),
		limit:    sessionCacheMaximumBytes,
	}
}

func (s *stableSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	key := sessionKey(req)
	if req.Offset > 0 {
		if cached, ok := s.lookup(key); ok {
			if err := s.extend(ctx, cached, req); err != nil {
				return searchcore.Response{}, fmt.Errorf("extend session search: %w", err)
			}

			return cached.respond(req), nil
		}
	}

	deep := req
	deep.Offset = 0
	deep.Limit = requestedSearchDepth(req)
	resp, err := s.inner.Search(ctx, deep)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("session search: %w", err)
	}
	if cause := context.Cause(ctx); cause != nil {
		return searchcore.Response{}, fmt.Errorf("session search: %w", cause)
	}
	if req.Offset == 0 && incompleteRefresh(resp) {
		if recent, ok := s.Recent(req); ok {
			return responseWithRefreshFailures(recent, resp.PartialFailures), nil
		}

		return resp, nil
	}
	stored := s.store(key, resp, deep.Limit)
	if req.Offset > 0 {
		if err := s.extend(ctx, stored, req); err != nil {
			return searchcore.Response{}, fmt.Errorf("extend new session search: %w", err)
		}
	}

	return stored.respond(req), nil
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
	now := clock()
	key = strings.Clone(key)
	resp.Results = boundedResults(resp.Results, searchDepth)
	entry := &session{
		key:         key,
		results:     resp.Results,
		failures:    cloneSessionFailures(resp.PartialFailures),
		total:       advertisedTotal(resp),
		searchDepth: searchDepth,
		recovered:   strings.Clone(resp.Recovered),
		didYouMean:  strings.Clone(resp.DidYouMean),
		facets:      cloneSessionFacets(resp.Facets),
		expires:     now.Add(sessionTTL),
	}
	entry.retained = retainedSessionBytes(entry)

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
	e.windowMu.RLock()
	defer e.windowMu.RUnlock()
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
		Results:         cloneSessionResults(e.results[start:end]),
		PartialFailures: cloneSessionFailures(e.failures),
		Recovered:       strings.Clone(e.recovered),
		DidYouMean:      strings.Clone(e.didYouMean),
		Facets:          cloneSessionFacets(e.facets),
	}
}

// sessionKey canonicalizes every request field that changes the result set;
// paging fields stay out so all pages of one query share a session.
func sessionKey(req searchcore.Request) string {
	req.Offset = 0
	req.Limit = 0
	encoded, _ := json.Marshal(req)

	return string(encoded)
}
