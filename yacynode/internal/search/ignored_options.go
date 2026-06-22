package search

type optionPredicate struct {
	name    string
	present func(Query) bool
}

var ignoredOptions = []optionPredicate{
	{"language", func(query Query) bool { return query.Filters.Language != "" }},
	{"prefer", func(query Query) bool { return query.Filters.Prefer != "" }},
	{"filter", func(query Query) bool { return query.Filters.Filter != "" }},
	{"profile", func(query Query) bool { return query.Filters.Profile != "" }},
	{"sitehost", func(query Query) bool { return query.Filters.SiteHost != "" }},
	{"author", func(query Query) bool { return query.Filters.Author != "" }},
	{"collection", func(query Query) bool { return query.Filters.Collection != "" }},
	{"filetype", func(query Query) bool { return query.Filters.FileType != "" }},
	{"protocol", func(query Query) bool { return query.Filters.Protocol != "" }},
	{"timezoneOffset", func(query Query) bool { return query.Filters.TimezoneOffset != 0 }},
}

func ignoredOptionNames(query Query) []string {
	var names []string
	for _, option := range ignoredOptions {
		if option.present(query) {
			names = append(names, option.name)
		}
	}

	return names
}
