package websearch

import (
	"strings"
	"unicode/utf8"
)

const (
	defaultCacheBytes     = 4 << 20
	maxCachedResults      = 20
	maxCachedTitleBytes   = 1 << 10
	maxCachedURLBytes     = 8 << 10
	maxCachedSnippetBytes = 8 << 10
)

func (p *DDGSProvider) cachedResultLimit() int {
	if p.maxResults > 0 && p.maxResults < maxCachedResults {
		return p.maxResults
	}

	return maxCachedResults
}

func normalizeResults(results []Result, limit int) []Result {
	normalized := make([]Result, 0, min(len(results), limit))
	for _, result := range results {
		if len(normalized) == limit {
			break
		}
		if len(result.URL) > maxCachedURLBytes {
			continue
		}
		normalized = append(normalized, Result{
			Title:   boundedText(result.Title, maxCachedTitleBytes),
			URL:     strings.Clone(result.URL),
			Snippet: boundedText(result.Snippet, maxCachedSnippetBytes),
		})
	}

	return normalized
}

func boundedText(value string, limit int) string {
	if len(value) <= limit {
		return strings.Clone(value)
	}
	end := limit
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}

	return strings.Clone(value[:end])
}
