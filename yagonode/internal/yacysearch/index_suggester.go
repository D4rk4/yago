package yacysearch

import (
	"context"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	// suggestFetchMultiplier over-fetches local results so that dropping blank or
	// duplicate titles still leaves a full suggestion list.
	suggestFetchMultiplier = 3
	// suggestionMaxRunes bounds a title so one very long page heading cannot fill
	// the dropdown; suggestions stay multi-word but readable.
	suggestionMaxRunes = 80
)

// indexSuggester turns a typed prefix into autocomplete suggestions drawn from
// the titles of matching locally-indexed documents, so the dropdown reflects
// public indexed content (whole, multi-word page titles) rather than any recorded
// query. It searches the local index only (SourceLocal) and never reaches the
// network, and the zero value (nil searcher) yields no suggestions.
type indexSuggester struct {
	search searchcore.Searcher
}

func (s indexSuggester) Suggest(ctx context.Context, rawPrefix string, limit int) []string {
	prefix := strings.TrimSpace(rawPrefix)
	if prefix == "" || s.search == nil {
		return nil
	}
	if limit <= 0 {
		limit = publicSuggestionLimit
	}

	parsed := searchcore.ParseTextQuery(prefix)
	resp, err := s.search.Search(ctx, searchcore.Request{
		Query:         prefix,
		Terms:         parsed.Terms,
		ExcludedTerms: parsed.ExcludedTerms,
		Phrases:       parsed.Phrases(),
		Source:        searchcore.SourceLocal,
		ContentDomain: searchcore.ContentDomainText,
		Verify:        searchcore.VerifyFalse,
		Limit:         limit * suggestFetchMultiplier,
	})
	if err != nil {
		return nil
	}

	return titleSuggestions(resp.Results, prefix, limit)
}

func titleSuggestions(results []searchcore.Result, prefix string, limit int) []string {
	suggestions := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, result := range results {
		title := normalizeSuggestion(result.Title)
		if title == "" || strings.EqualFold(title, prefix) {
			continue
		}
		key := strings.ToLower(title)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		suggestions = append(suggestions, title)
		if len(suggestions) == limit {
			break
		}
	}

	return suggestions
}

func normalizeSuggestion(raw string) string {
	title := strings.Join(strings.Fields(raw), " ")
	if title == "" {
		return ""
	}
	runes := []rune(title)
	if len(runes) > suggestionMaxRunes {
		title = strings.TrimSpace(string(runes[:suggestionMaxRunes]))
	}

	return title
}
