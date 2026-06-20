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
	// its /language/ token feeds the RWI join via JoinLanguage; only a site: host stays unsupported because narrowing the join by host needs URL hashing this node does not do
	{
		"modifier",
		func(query SearchQuery) bool { return ParseSearchModifier(query.Filters.Modifier).SiteHost != "" },
	},
}

func UnsupportedSearchOptions(query SearchQuery) []string {
	return matchingSearchOptions(query, unsupportedSearchOptions)
}
