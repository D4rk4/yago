package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type recordingSearcher struct{ got searchcore.Request }

func (r *recordingSearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	r.got = req

	return searchcore.Response{}, nil
}

// TestWithParsedQueryFillsTermsFromRawQuery proves the fix for human/API surfaces
// that submit only a raw query: the shared searcher parses word-hash terms so the
// remote DHT fan-out no longer reports "no query terms", while callers that
// already parsed the query keep their terms.
func TestWithParsedQueryFillsTermsFromRawQuery(t *testing.T) {
	t.Parallel()

	rec := &recordingSearcher{}
	searcher := withParsedQuery(rec)
	ctx := context.Background()

	// A raw query with no terms is parsed into word-hash terms.
	if _, err := searcher.Search(ctx, searchcore.Request{Query: "anthropic"}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rec.got.Terms) == 0 {
		t.Fatal("terms were not filled from the raw query")
	}

	// A request that already carries terms (e.g. /yacysearch, Tavily) is untouched.
	if _, err := searcher.Search(ctx, searchcore.Request{
		Query: "ignored", Terms: []string{"kept"},
	}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rec.got.Terms) != 1 || rec.got.Terms[0] != "kept" {
		t.Fatalf("preset terms overwritten: %v", rec.got.Terms)
	}

	// A blank query has nothing to parse, so terms stay empty.
	if _, err := searcher.Search(ctx, searchcore.Request{Query: "   "}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rec.got.Terms) != 0 {
		t.Fatalf("blank query produced terms: %v", rec.got.Terms)
	}
}
