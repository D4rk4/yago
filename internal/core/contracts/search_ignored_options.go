package contracts

// Each option is safe to accept and ignore because the initiator re-applies it
// to our returned results, and none of them touch the RWI join, so the count we
// report stays faithful to what the initiator will keep.
var ignoredSearchOptions = []searchOptionPredicate{
	// upstream uses it only as the ranking target language, never to narrow the join
	{"language", func(query SearchQuery) bool { return query.Filters.Language != "" }},
	// ranking-only hint, never narrows the result set
	{"prefer", func(query SearchQuery) bool { return query.Filters.Prefer != "" }},
	// URL-regex filter applied at the URL stage, after the join is counted
	{"filter", func(query SearchQuery) bool { return query.Filters.Filter != "" }},
	// ranking profile carrier, not a membership filter
	{"profile", func(query SearchQuery) bool { return query.Filters.Profile != "" }},
	// initiator never transmits it; the modifier's site: token carries host narrowing into the join instead
	{"sitehost", func(query SearchQuery) bool { return query.Filters.SiteHost != "" }},
	// URL-stage filter applied after the join is counted
	{"author", func(query SearchQuery) bool { return query.Filters.Author != "" }},
	// Solr-only field with no RWI equivalent
	{"collection", func(query SearchQuery) bool { return query.Filters.Collection != "" }},
	// URL-stage filter applied after the join is counted
	{"filetype", func(query SearchQuery) bool { return query.Filters.FileType != "" }},
	// URL-stage filter applied after the join is counted
	{"protocol", func(query SearchQuery) bool { return query.Filters.Protocol != "" }},
	// presentation-time date context, not a filter
	{"timezoneOffset", func(query SearchQuery) bool { return query.Filters.TimezoneOffset != 0 }},
}

func IgnoredSearchOptions(query SearchQuery) []string {
	return matchingSearchOptions(query, ignoredSearchOptions)
}
