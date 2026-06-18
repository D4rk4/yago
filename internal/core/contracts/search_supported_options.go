package contracts

type unsupportedSearchOption struct {
	name        string
	unsupported func(SearchQuery) bool
}

var unsupportedSearchOptions = []unsupportedSearchOption{
	// rejected: its embedded /language/ token filters the RWI join, so silently ignoring it would inflate the returned count
	{"modifier", func(query SearchQuery) bool { return query.Filters.Modifier != "" }},
}

func UnsupportedSearchOptions(query SearchQuery) []string {
	var unsupported []string
	for _, option := range unsupportedSearchOptions {
		if option.unsupported(query) {
			unsupported = append(unsupported, option.name)
		}
	}
	return unsupported
}
