package contracts

import (
	"slices"
	"testing"
)

func TestIgnoredSearchOptionsReportsAcceptedFilters(t *testing.T) {
	got := IgnoredSearchOptions(SearchQuery{
		Filters: SearchFilters{
			Language:       "en",
			Prefer:         "p",
			Filter:         "f",
			Profile:        "pr",
			SiteHost:       "example.com",
			Author:         "a",
			Collection:     "c",
			FileType:       "pdf",
			Protocol:       "https",
			TimezoneOffset: 120,
		},
	})
	want := []string{
		"language",
		"prefer",
		"filter",
		"profile",
		"sitehost",
		"author",
		"collection",
		"filetype",
		"protocol",
		"timezoneOffset",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("ignored = %v, want %v", got, want)
	}
}

func TestIgnoredSearchOptionsEmptyWhenUnset(t *testing.T) {
	if got := IgnoredSearchOptions(SearchQuery{}); len(got) != 0 {
		t.Fatalf("ignored = %v, want none", got)
	}
}
