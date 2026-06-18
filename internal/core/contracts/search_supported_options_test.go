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
		Filters:     SearchFilters{Language: "en", Partitions: 30},
	})
	if len(got) != 0 {
		t.Fatalf("unsupported = %v, want none", got)
	}
}
