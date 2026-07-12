package searchcore

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParsePublicTextQueryBoundsRunes(t *testing.T) {
	accepted := strings.Repeat("я", maximumPublicQueryRunes)
	parsed, err := ParsePublicTextQuery(accepted)
	if err != nil {
		t.Fatalf("ParsePublicTextQuery: %v", err)
	}
	if len(parsed.Terms) != 1 || parsed.Terms[0] != accepted {
		t.Fatalf("terms = %#v", parsed.Terms)
	}

	_, err = ParsePublicTextQuery(accepted + "я")
	if !errors.Is(err, errPublicQueryTooLong) {
		t.Fatalf("error = %v", err)
	}
}

func TestParsePublicTextQueryBoundsTerms(t *testing.T) {
	accepted := publicQueryTerms(maximumPublicQueryTerms)
	parsed, err := ParsePublicTextQuery(strings.Join(accepted, " "))
	if err != nil {
		t.Fatalf("ParsePublicTextQuery: %v", err)
	}
	if len(parsed.Terms)+len(parsed.ExcludedTerms) != maximumPublicQueryTerms {
		t.Fatalf("term total = %d", len(parsed.Terms)+len(parsed.ExcludedTerms))
	}

	_, err = ParsePublicTextQuery(strings.Join(publicQueryTerms(maximumPublicQueryTerms+1), " "))
	if !errors.Is(err, errPublicQueryHasTooManyTerms) {
		t.Fatalf("error = %v", err)
	}
}

func TestParsePublicRequestBoundsPreparsedInput(t *testing.T) {
	parsed, err := ParsePublicRequest(Request{Query: "alpha beta"})
	if err != nil {
		t.Fatalf("ParsePublicRequest: %v", err)
	}
	if len(parsed.Terms) != 2 {
		t.Fatalf("terms = %#v", parsed.Terms)
	}

	_, err = ParsePublicRequest(Request{Query: strings.Repeat("x", maximumPublicQueryRunes+1)})
	if !errors.Is(err, errPublicQueryTooLong) {
		t.Fatalf("rune error = %v", err)
	}

	_, err = ParsePublicRequest(Request{
		Query:          "bounded",
		SubmittedQuery: strings.Repeat("x", maximumPublicQueryRunes+1),
	})
	if !errors.Is(err, errPublicQueryTooLong) {
		t.Fatalf("submitted query rune error = %v", err)
	}

	_, err = ParsePublicRequest(Request{
		Query: "bounded",
		Terms: publicQueryTerms(maximumPublicQueryTerms + 1),
	})
	if !errors.Is(err, errPublicQueryHasTooManyTerms) {
		t.Fatalf("term error = %v", err)
	}
}

func TestNormalizePublicRequestRejectsUnboundedQuery(t *testing.T) {
	_, err := NormalizePublicRequest(Request{
		Query: strings.Repeat("я", maximumPublicQueryRunes+1),
	}, DefaultPublicLimit)
	if !errors.Is(err, errPublicQueryTooLong) {
		t.Fatalf("error = %v", err)
	}
}

func publicQueryTerms(count int) []string {
	terms := make([]string, count)
	for index := range terms {
		terms[index] = fmt.Sprintf("term%d", index)
		if index%2 != 0 {
			terms[index] = "-" + terms[index]
		}
	}

	return terms
}
