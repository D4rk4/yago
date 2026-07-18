package yagonode

import (
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestQueryFailureSourcesAreUniqueOrderedAndBounded(t *testing.T) {
	failures := make([]searchcore.PartialFailure, 0, 12)
	failures = append(failures,
		searchcore.PartialFailure{Source: "exact"},
		searchcore.PartialFailure{Source: "exact"},
	)
	for index := range 10 {
		failures = append(failures, searchcore.PartialFailure{
			Source: string(rune('a' + index)),
		})
	}
	got := queryFailureSources(failures)
	want := []string{"exact", "a", "b", "c", "d", "e", "f", "g"}
	if !slices.Equal(got, want) {
		t.Fatalf("sources = %v, want %v", got, want)
	}
}

func TestQueryLogAttributesOmitFailureFieldsForCompleteSearch(t *testing.T) {
	attributes := queryLogAttributes(
		searchcore.Request{Query: "private"},
		searchcore.Response{TotalResults: 2},
		false,
	)
	if len(attributes) != 2 {
		t.Fatalf("attributes = %#v", attributes)
	}
}
