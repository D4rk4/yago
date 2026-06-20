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
			Prefer:           "p",
			Filter:           "f",
			SiteHost:         "example.com",
			Author:           "a",
			Collection:       "c",
			FileType:         "pdf",
			Protocol:         "https",
		},
	})
	if len(got) != 0 {
		t.Fatalf("unsupported = %v, want none", got)
	}
}

func TestUnsupportedSearchOptionsAllowsLanguageModifier(t *testing.T) {
	got := UnsupportedSearchOptions(SearchQuery{
		Filters: SearchFilters{Modifier: "/language/de filetype:pdf author:foo"},
	})
	if len(got) != 0 {
		t.Fatalf("unsupported = %v, want none", got)
	}
}

func TestUnsupportedSearchOptionsRejectsSiteModifier(t *testing.T) {
	got := UnsupportedSearchOptions(SearchQuery{
		Filters: SearchFilters{Modifier: "site:example.com /language/de"},
	})
	if len(got) != 1 || got[0] != "modifier" {
		t.Fatalf("unsupported = %v, want [modifier]", got)
	}
}
