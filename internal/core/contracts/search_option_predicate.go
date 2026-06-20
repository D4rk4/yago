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
