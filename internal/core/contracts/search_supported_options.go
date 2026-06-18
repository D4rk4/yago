package contracts

type unsupportedSearchOption struct {
	name        string
	unsupported func(SearchQuery) bool
}

var unsupportedSearchOptions = []unsupportedSearchOption{
	{"modifier", func(query SearchQuery) bool { return query.Filters.Modifier != "" }},
	{"prefer", func(query SearchQuery) bool { return query.Filters.Prefer != "" }},
	{"filter", func(query SearchQuery) bool { return query.Filters.Filter != "" }},
	{"sitehost", func(query SearchQuery) bool { return query.Filters.SiteHost != "" }},
	{"author", func(query SearchQuery) bool { return query.Filters.Author != "" }},
	{"collection", func(query SearchQuery) bool { return query.Filters.Collection != "" }},
	{"filetype", func(query SearchQuery) bool { return query.Filters.FileType != "" }},
	{"protocol", func(query SearchQuery) bool { return query.Filters.Protocol != "" }},
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
