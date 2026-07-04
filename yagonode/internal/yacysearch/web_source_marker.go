package yacysearch

import "github.com/D4rk4/yago/yagonode/internal/searchcore"

const webResultMarker = "[ddgs]"

// markWebResultTitle prefixes a visible [ddgs] marker onto results served by the
// optional web-search fallback so the human search surfaces never confuse them
// with owned local or federated hits. The Tavily-compatible API does not call
// this and returns the same results unmarked, keeping it a drop-in surface.
func markWebResultTitle(source searchcore.Source, title string) string {
	if source == searchcore.SourceWeb {
		return webResultMarker + " " + title
	}

	return title
}
