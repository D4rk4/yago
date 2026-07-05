package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type stubPortalSearcher struct {
	response   searchcore.Response
	err        error
	gotRequest searchcore.Request
}

func (s *stubPortalSearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.gotRequest = req

	return s.response, s.err
}

func TestPortalSourceMapsAndMarksResults(t *testing.T) {
	t.Parallel()

	searcher := &stubPortalSearcher{response: searchcore.Response{
		TotalResults: 2,
		Results: []searchcore.Result{
			{Title: "local", URL: "http://a/1", DisplayURL: "a/1", Source: searchcore.SourceLocal},
			{Title: "web", URL: "http://b/2", DisplayURL: "b/2", Source: searchcore.SourceWeb},
		},
	}}

	results, err := newPortalSource(searcher).Search(context.Background(), "go", 20, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if results.TotalResults != 2 || len(results.Results) != 2 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if searcher.gotRequest.Offset != 20 || searcher.gotRequest.Limit != 10 {
		t.Fatalf("offset=%d limit=%d, want the window forwarded to the searcher",
			searcher.gotRequest.Offset, searcher.gotRequest.Limit)
	}
	if searcher.gotRequest.Source != searchcore.SourceGlobal {
		t.Fatalf("source = %q, want global", searcher.gotRequest.Source)
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

	searcher := &stubPortalSearcher{err: errors.New("boom")}
	if _, err := newPortalSource(searcher).Search(context.Background(), "go", 0, 10); err == nil {
		t.Fatal("expected error")
	}
}
