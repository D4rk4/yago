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
	}}, enricher, remoteTextEvidence)
	resp, err := search.Search(t.Context(), searchcore.Request{Terms: []string{"осень"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !strings.Contains(resp.Results[0].Snippet, "осень") {
		t.Fatalf("fetched snippet missing the term: %q", resp.Results[0].Snippet)
	}
}

func TestBuildSnippetEnricherUsesMorphologicalBodyEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		subject := "психопатах"
		if r.URL.Path == "/match" {
			subject = "псилобатах"
		}
		_, _ = w.Write([]byte(
			"<html><body>Летопись рассказывает о " + subject + " и походах.</body></html>",
		))
	}))
	defer server.Close()

	for _, test := range []struct {
		name string
		path string
		want int
	}{
		{name: "morphology", path: "/match", want: 1},
		{name: "different stem", path: "/mismatch", want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			search := snippetfetch.WithSnippetEnrichment(staticSearcher{resp: searchcore.Response{
				TotalResults: 1,
				Results: []searchcore.Result{{
					Title: "Военная летопись", URL: server.URL + test.path,
					Snippet: "Военная летопись", Source: searchcore.SourceRemote,
					Language: "ru",
				}},
			}}, buildSnippetEnricher(nodeConfig{PeerSnippetFetch: true}, server.Client()), remoteTextEvidence)
			response, err := search.Search(t.Context(), searchcore.Request{
				Terms: []string{"псилобаты"},
			})
			if err != nil || len(response.Results) != test.want ||
				response.TotalResults != test.want {
				t.Fatalf("response = %#v, err = %v", response, err)
			}
			if test.want == 1 && !strings.Contains(response.Results[0].Snippet, "псилобатах") {
				t.Fatalf("snippet = %q", response.Results[0].Snippet)
			}
		})
	}
}

type staticSearcher struct{ resp searchcore.Response }

func (s staticSearcher) Search(
	_ context.Context,
	_ searchcore.Request,
) (searchcore.Response, error) {
	return s.resp, nil
}
