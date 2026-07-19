package yagonode

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/publicportal"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestPublicPortalRendersResultReasonsAsDisclosure(t *testing.T) {
	searcher := &stubPortalSearcher{response: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title: "Evidence", URL: "https://example.org/", Source: searchcore.SourceLocal,
			Evidence: searchcore.NewRankingEvidence(searchcore.RankingSignalValue{
				Signal: searchcore.SignalTitleScore, Value: 1,
			}),
		}},
	}}
	handler := publicportal.New(newPortalSource(searcher), false)
	request := httptest.NewRequestWithContext(t.Context(), "GET", "/?q=evidence", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	page := response.Body.String()
	for _, want := range []string{
		"Why this result?", "Matched the local full-text index.", "The query matched the title.",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("portal does not contain %q: %s", want, page)
		}
	}
}
