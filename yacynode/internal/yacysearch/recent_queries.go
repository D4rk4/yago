package yacysearch

import (
	"strings"
	"sync"
)

const (
	recentQueryLimit      = 64
	publicSuggestionLimit = 10
)

type recentQueries struct {
	mu      sync.Mutex
	queries []string
}

func newRecentQueries() *recentQueries {
	return &recentQueries{}
}

func (q *recentQueries) Record(raw string) {
	if q == nil {
		return
	}
	query := strings.TrimSpace(raw)
	if query == "" {
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	next := []string{query}
	for _, existing := range q.queries {
		if strings.EqualFold(existing, query) {
			continue
		}
		next = append(next, existing)
		if len(next) == recentQueryLimit {
			break
		}
	}
	q.queries = next
}

func (q *recentQueries) Suggest(rawPrefix string, limit int) []string {
	if q == nil {
		return nil
	}
	prefix := strings.TrimSpace(rawPrefix)
	if prefix == "" {
		return nil
	}
	if limit <= 0 {
		limit = publicSuggestionLimit
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	suggestions := make([]string, 0, limit)
	lowerPrefix := strings.ToLower(prefix)
	for _, query := range q.queries {
		if !strings.HasPrefix(strings.ToLower(query), lowerPrefix) {
			continue
		}
		suggestions = append(suggestions, query)
		if len(suggestions) == limit {
			break
		}
	}

	return suggestions
}
