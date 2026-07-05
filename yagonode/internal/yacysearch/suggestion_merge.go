package yacysearch

import "strings"

// mergeSuggestions concatenates suggestion groups in priority order (local-index
// titles first, recorded recent queries second), dropping case-insensitive
// duplicates and capping the result at limit. It always returns a non-nil slice
// so the suggestions endpoints encode an empty JSON array rather than null.
func mergeSuggestions(limit int, groups ...[]string) []string {
	if limit <= 0 {
		limit = publicSuggestionLimit
	}
	merged := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, group := range groups {
		for _, suggestion := range group {
			if len(merged) == limit {
				return merged
			}
			key := strings.ToLower(suggestion)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, suggestion)
		}
	}

	return merged
}
