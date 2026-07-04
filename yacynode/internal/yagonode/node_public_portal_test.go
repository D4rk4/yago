package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yacynode/internal/searchcore"
)

type stubPortalSearcher struct {
	response searchcore.Response
	err      error
}

func (s stubPortalSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	return s.response, s.err
}

func TestPortalSourceMapsAndMarksResults(t *testing.T) {
	t.Parallel()

	searcher := stubPortalSearcher{response: searchcore.Response{
		TotalResults: 2,
		Results: []searchcore.Result{
			{Title: "local", URL: "http://a/1", DisplayURL: "a/1", Source: searchcore.SourceLocal},
			{Title: "web", URL: "http://b/2", DisplayURL: "b/2", Source: searchcore.SourceWeb},
		},
	}}

	results, err := newPortalSource(searcher).Search(context.Background(), "go")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if results.TotalResults != 2 || len(results.Results) != 2 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if results.Results[0].Marked {
		t.Fatal("local result must not be marked")
	}
	if !results.Results[1].Marked {
		t.Fatal("ddgs result must be marked")
	}
}

func TestPortalSourceWrapsError(t *testing.T) {
	t.Parallel()

	searcher := stubPortalSearcher{err: errors.New("boom")}
	if _, err := newPortalSource(searcher).Search(context.Background(), "go"); err == nil {
		t.Fatal("expected error")
	}
}
