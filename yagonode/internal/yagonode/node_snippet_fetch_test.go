package yagonode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/snippetfetch"
)

func TestBuildSnippetEnricherHonorsToggleAndFetchesText(t *testing.T) {
	if buildSnippetEnricher(nodeConfig{}, http.DefaultClient) != nil {
		t.Fatal("disabled toggle must yield no enricher")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(
			[]byte(
				"<html><body><p>Что такое осень — это небо, плачущее небо под ногами.</p></body></html>",
			),
		)
	}))
	defer server.Close()

	enricher := buildSnippetEnricher(nodeConfig{PeerSnippetFetch: true}, server.Client())
	if enricher == nil {
		t.Fatal("enabled toggle must yield an enricher")
	}
	search := snippetfetch.WithSnippetEnrichment(staticSearcher{resp: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title:   "ДДТ",
			URL:     server.URL,
			Snippet: "ДДТ",
			Source:  searchcore.SourceRemote,
		}},
	}}, enricher)
	resp, err := search.Search(t.Context(), searchcore.Request{Terms: []string{"осень"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !strings.Contains(resp.Results[0].Snippet, "осень") {
		t.Fatalf("fetched snippet missing the term: %q", resp.Results[0].Snippet)
	}
}

type staticSearcher struct{ resp searchcore.Response }

func (s staticSearcher) Search(
	_ context.Context,
	_ searchcore.Request,
) (searchcore.Response, error) {
	return s.resp, nil
}
