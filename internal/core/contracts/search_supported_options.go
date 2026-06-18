package contracts

type searchOptionPredicate struct {
	name    string
	present func(SearchQuery) bool
}

func matchingSearchOptions(query SearchQuery, options []searchOptionPredicate) []string {
	var names []string
	for _, option := range options {
		if option.present(query) {
			names = append(names, option.name)
		}
	}
	return names
}

var unsupportedSearchOptions = []searchOptionPredicate{
	// rejected: its embedded /language/ token filters the RWI join, so silently ignoring it would inflate the returned count
	{"modifier", func(query SearchQuery) bool { return query.Filters.Modifier != "" }},
}

func UnsupportedSearchOptions(query SearchQuery) []string {
	return matchingSearchOptions(query, unsupportedSearchOptions)
}
