package search

type optionPredicate struct {
	name    string
	present func(searchQuery) bool
}

var ignoredOptions = []optionPredicate{
	{"language", func(query searchQuery) bool { return query.searchFilters.Language != "" }},
	{"prefer", func(query searchQuery) bool { return query.searchFilters.Prefer != "" }},
	{"filter", func(query searchQuery) bool { return query.searchFilters.Filter != "" }},
	{"profile", func(query searchQuery) bool { return query.searchFilters.Profile != "" }},
	{"sitehost", func(query searchQuery) bool { return query.searchFilters.SiteHost != "" }},
	{"author", func(query searchQuery) bool { return query.searchFilters.Author != "" }},
	{"collection", func(query searchQuery) bool { return query.searchFilters.Collection != "" }},
	{"filetype", func(query searchQuery) bool { return query.searchFilters.FileType != "" }},
	{"protocol", func(query searchQuery) bool { return query.searchFilters.Protocol != "" }},
	{
		"timezoneOffset",
		func(query searchQuery) bool { return query.searchFilters.TimezoneOffset != 0 },
	},
}

func ignoredOptionNames(query searchQuery) []string {
	var names []string
	for _, option := range ignoredOptions {
		if option.present(query) {
			names = append(names, option.name)
		}
	}

	return names
}
