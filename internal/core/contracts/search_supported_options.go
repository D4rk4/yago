package contracts

type unsupportedSearchOption struct {
	name        string
	unsupported func(SearchQuery) bool
}

var unsupportedSearchOptions = []unsupportedSearchOption{
	{"contentdom", func(query SearchQuery) bool { return query.Filters.ContentDomain != "" }},
	{"strictContentDom", func(query SearchQuery) bool { return query.Filters.StrictContentDom }},
	{"timezoneOffset", func(query SearchQuery) bool { return query.Filters.TimezoneOffset != 0 }},
	{"modifier", func(query SearchQuery) bool { return query.Filters.Modifier != "" }},
	{"prefer", func(query SearchQuery) bool { return query.Filters.Prefer != "" }},
	{"filter", func(query SearchQuery) bool { return query.Filters.Filter != "" }},
	{"constraint", func(query SearchQuery) bool { return query.Filters.Constraint != "" }},
	{"profile", func(query SearchQuery) bool { return query.Filters.Profile != "" }},
	{"sitehost", func(query SearchQuery) bool { return query.Filters.SiteHost != "" }},
	{"sitehash", func(query SearchQuery) bool { return query.Filters.SiteHash != "" }},
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
