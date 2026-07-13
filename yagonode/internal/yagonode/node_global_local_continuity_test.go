package yagonode

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchsession"
	"github.com/D4rk4/yago/yagonode/internal/yacysearch"
)

func TestGlobalJSONDeadlineReturnsRecentLocalCoverage(t *testing.T) {
	stable := searchsession.NewStableWindow(staticSearcher{resp: searchcore.Response{
		TotalResults: 148,
		Results: []searchcore.Result{{
			Title: "DrunkLab", URL: "https://about.me/drunklab", Source: searchcore.SourceLocal,
		}},
	}})
	localRequest := searchcore.Request{
		Query: "drunklab", Source: searchcore.SourceLocal,
		ContentDomain: searchcore.ContentDomainText, Verify: searchcore.VerifyIfExist, Limit: 10,
	}
	if _, err := withParsedQuery(stable).Search(t.Context(), localRequest); err != nil {
		t.Fatal(err)
	}
	inner := &blockingInteractiveSearch{
		started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}),
	}
	searcher := withParsedQuery(searchsession.WithRecentSuccessOnIncompleteRefresh(
		interactiveBudgetFixture(inner, 20*time.Millisecond),
		stable,
	))
	mux := http.NewServeMux()
	yacysearch.MountJSON(mux, searcher)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/yacysearch.json?query=drunklab&resource=global&contentdom=text&maximumRecords=10&startRecord=0",
		nil,
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	var payload struct {
		Channels []struct {
			TotalResults string `json:"totalResults"`
			Items        []struct {
				Title string `json:"title"`
				Link  string `json:"link"`
			} `json:"items"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Channels) != 1 || payload.Channels[0].TotalResults != "148" ||
		len(payload.Channels[0].Items) != 1 ||
		payload.Channels[0].Items[0].Link != "https://about.me/drunklab" {
		t.Fatalf("payload = %#v", payload)
	}
	close(inner.release)
	select {
	case <-inner.finished:
	case <-time.After(time.Second):
		t.Fatal("interactive search did not finish")
	}
}
