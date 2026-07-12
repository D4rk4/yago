package tavilyapi

import (
	"fmt"
	"strings"
	"testing"
)

func TestCoreRequestRejectsUnboundedQuery(t *testing.T) {
	cases := []string{
		strings.Repeat("я", 513),
		boundedQueryTerms(33),
	}
	for _, query := range cases {
		_, err := coreRequest(SearchRequest{Query: query})
		if err == nil || !isBadRequest(err) {
			t.Fatalf("query with %d runes error = %v", len([]rune(query)), err)
		}
	}
}

func boundedQueryTerms(count int) string {
	terms := make([]string, count)
	for index := range terms {
		terms[index] = fmt.Sprintf("term%d", index)
	}

	return strings.Join(terms, " ")
}
