package documentsearch

import "github.com/D4rk4/yago/yagoproto"

type ignoredOption struct {
	name    string
	present func(yagoproto.SearchRequest) bool
}

var ignoredOptions = []ignoredOption{
	{"prefer", func(r yagoproto.SearchRequest) bool { return r.Prefer != "" }},
	{"profile", func(r yagoproto.SearchRequest) bool { return r.Profile != "" }},
	{"collection", func(r yagoproto.SearchRequest) bool { return r.Collection != "" }},
	{"timezoneOffset", func(r yagoproto.SearchRequest) bool { return r.TimezoneOffset != 0 }},
}

func ignoredOptionNames(req yagoproto.SearchRequest) []string {
	var names []string
	for _, option := range ignoredOptions {
		if option.present(req) {
			names = append(names, option.name)
		}
	}

	return names
}
