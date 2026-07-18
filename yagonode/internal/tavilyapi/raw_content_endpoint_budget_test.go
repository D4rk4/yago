package tavilyapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestCrawlEndpointRejectsOversizedAggregateBeforeSuccessHeader(t *testing.T) {
	const url = "https://oversized.example/"
	maximum := maximumCrawlTextBytes(url)
	endpoint := crawlEndpoint{
		access: SearchAccessPolicy{BearerToken: crawlTestKey},
		fetcher: singlePageFetcher{page: CrawledPage{
			Text: strings.Repeat("x", maximum+1),
		}},
		now:          time.Now,
		workDuration: time.Second,
	}
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathCrawl,
		strings.NewReader(`{"url":"`+url+`","limit":`+"1"+`}`),
	)
	request.Header.Set("Authorization", "Bearer "+crawlTestKey)
	result := httptest.NewRecorder()
	endpoint.ServeHTTP(result, request)
	if result.Code != http.StatusRequestEntityTooLarge ||
		!strings.Contains(result.Body.String(), "raw content response exceeds resource limit") {
		t.Fatalf("result=%d body=%s", result.Code, result.Body.String())
	}
}

func TestRawSearchRejectsOversizedAggregate(t *testing.T) {
	firstURL := "https://raw.example/first"
	secondURL := "https://raw.example/second"
	text := strings.Repeat("x", 9<<20)
	search := &fakeSearcher{response: searchcore.Response{Results: []searchcore.Result{
		{Title: "first", URL: firstURL, Snippet: "first"},
		{Title: "second", URL: secondURL, Snippet: "second"},
	}}}
	documents := &fakeDocuments{rows: map[string]documentstore.Document{
		firstURL:  {ExtractedText: text},
		secondURL: {ExtractedText: text},
	}}
	result := postRawSearch(
		t,
		newSearchEndpoint(
			search,
			documents,
			SearchAccessPolicy{BearerToken: searchTestKey},
			nil,
		),
		`{"query":"raw","max_results":2,"include_raw_content":true}`,
	)
	if result.Code != http.StatusRequestEntityTooLarge || search.calls != 1 ||
		!strings.Contains(result.Body.String(), "raw content response exceeds resource limit") {
		t.Fatalf("result=%d calls=%d body=%s", result.Code, search.calls, result.Body.String())
	}
}

func TestExtractEndpointRejectsExhaustedAggregateBeforeSuccessHeader(t *testing.T) {
	const (
		id        = "budget-id"
		firstURL  = "https://extract.example/first"
		secondURL = "https://extract.example/second"
	)
	maximum := maximumExtractTextBytesForCount(id, firstURL, 2)
	endpoint := extractEndpoint{
		documents: &fakeDocuments{rows: map[string]documentstore.Document{
			firstURL:  {ExtractedText: strings.Repeat("x", maximum)},
			secondURL: {ExtractedText: "second"},
		}},
		access:       SearchAccessPolicy{BearerToken: extractTestKey},
		now:          time.Now,
		workDuration: time.Second,
	}
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathExtract,
		strings.NewReader(`{"urls":["`+firstURL+`","`+secondURL+`"]}`),
	)
	request.Header.Set("Authorization", "Bearer "+extractTestKey)
	request.Header.Set(requestIDHeader, id)
	result := httptest.NewRecorder()
	endpoint.ServeHTTP(result, request)
	if result.Code != http.StatusRequestEntityTooLarge ||
		!strings.Contains(result.Body.String(), "raw content response exceeds resource limit") {
		t.Fatalf("result=%d body=%s", result.Code, result.Body.String())
	}
}
