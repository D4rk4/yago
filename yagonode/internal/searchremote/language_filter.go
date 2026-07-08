package searchremote

import (
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

// rowLanguage reads the peer-reported document language from a URL metadata
// row's plain "lang" property, so a remote result carries the page's real
// language instead of echoing the query filter.
func rowLanguage(row yagomodel.URIMetadataRow) string {
	return strings.ToLower(strings.TrimSpace(row.Properties["lang"]))
}

// languageFiltered drops remote rows whose reported language contradicts the
// query's language: operator (SEARCH-41). Rows without a reported language are
// kept — a missing value is unknown, not a mismatch — so peers that omit the
// property keep working. Languages compare by their two-letter prefix, which
// reconciles ISO 639-1 ("ru") with 639-2 ("rus") spellings across peers.
func languageFiltered(req searchcore.Request, results []searchcore.Result) []searchcore.Result {
	want := languageKey(req.Language)
	if want == "" {
		return results
	}
	kept := make([]searchcore.Result, 0, len(results))
	for _, result := range results {
		have := languageKey(result.Language)
		if have != "" && have != want {
			continue
		}
		kept = append(kept, result)
	}

	return kept
}

func languageKey(language string) string {
	normalized := strings.ToLower(strings.TrimSpace(language))
	if len(normalized) > 2 {
		return normalized[:2]
	}

	return normalized
}
