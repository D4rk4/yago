package yacysearch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestMarkWebResultTitle(t *testing.T) {
	if got := markWebResultTitle(searchcore.SourceWeb, "Title"); got != "[ddgs] Title" {
		t.Errorf("web = %q, want %q", got, "[ddgs] Title")
	}
	if got := markWebResultTitle(searchcore.SourceLocal, "Title"); got != "Title" {
		t.Errorf("local = %q, want unchanged", got)
	}
}

func TestJSONEndpointMarksWebFallbackResults(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title:  "Web Hit",
			URL:    "https://web.example/page",
			Source: searchcore.SourceWeb,
		}},
	}}
	mux := http.NewServeMux()
	Mount(mux, search, false)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://node.test/yacysearch.json?query=gap",
		nil,
	)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var out jsonResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Channels) != 1 || len(out.Channels[0].Items) != 1 {
		t.Fatalf("response = %#v", out)
	}
	if title := out.Channels[0].Items[0].Title; !strings.HasPrefix(title, "[ddgs] ") {
		t.Errorf("web result title = %q, want [ddgs] prefix", title)
	}
}
