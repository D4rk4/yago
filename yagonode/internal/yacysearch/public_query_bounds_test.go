package yacysearch

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagoproto"
)

func TestSearchRequestFromValuesRejectsUnboundedQuery(t *testing.T) {
	cases := []string{
		strings.Repeat("я", 513),
		boundedQueryTerms(33),
	}
	for _, query := range cases {
		_, err := searchRequestFromValues(url.Values{yagoproto.FieldQuery: {query}})
		if err == nil {
			t.Fatalf("query with %d runes succeeded", len([]rune(query)))
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
