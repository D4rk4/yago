package contracts

import (
	"testing"
)

func TestUnsupportedSearchOptionsAllowsSupportedSubset(t *testing.T) {
	got := UnsupportedSearchOptions(SearchQuery{
		Words:       nil,
		Exclude:     nil,
		URLs:        nil,
		MaxResults:  10,
		MaxDistance: 4,
		Filters: SearchFilters{
			Language:         "en",
			Partitions:       30,
			ContentDomain:    "image",
			StrictContentDom: true,
			Constraint:       "______",
			SiteHash:         "abcdef",
			TimezoneOffset:   120,
			Profile:          "p",
		},
	})
	if len(got) != 0 {
		t.Fatalf("unsupported = %v, want none", got)
	}
}

func TestUnsupportedSearchOptionsRejectsUnsupported(t *testing.T) {
	got := UnsupportedSearchOptions(SearchQuery{
		Filters: SearchFilters{
			Modifier: "m",
			Prefer:   "p",
			Filter:   "f",
			SiteHost: "example.com",
			Author:   "a",
		},
	})
	if len(got) != 5 {
		t.Fatalf("unsupported = %v, want 5 options", got)
	}
}
