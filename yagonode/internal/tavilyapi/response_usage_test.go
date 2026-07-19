package tavilyapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestSearchUsageAccountsForExecutedDepth(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantCredits int
		wantCalls   int
	}{
		{
			name:        "basic",
			body:        `{"query":"usage","include_usage":true}`,
			wantCredits: 1,
			wantCalls:   1,
		},
		{
			name:        "advanced",
			body:        `{"query":"usage","search_depth":"advanced","include_usage":true}`,
			wantCredits: 2,
			wantCalls:   1,
		},
		{
			name:        "short circuited",
			body:        `{"query":"usage","max_results":0,"include_usage":true}`,
			wantCredits: 0,
			wantCalls:   0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			search := &fakeSearcher{}
			response := requestSearchUsage(t, test.body, search)
			if response.Usage == nil || response.Usage.Credits != test.wantCredits ||
				search.calls != test.wantCalls {
				t.Fatalf(
					"usage=%#v calls=%d, want credits=%d calls=%d",
					response.Usage,
					search.calls,
					test.wantCredits,
					test.wantCalls,
				)
			}
		})
	}
}

func TestExtractUsageAccountsForSuccessfulExtractionBuckets(t *testing.T) {
	documents, urls := successfulExtractionDocuments(5)
	tests := []struct {
		name        string
		urls        []string
		depth       string
		wantCredits int
	}{
		{name: "four basic", urls: urls[:4], wantCredits: 0},
		{name: "five basic", urls: urls, wantCredits: 1},
		{name: "five advanced", urls: urls, depth: "advanced", wantCredits: 2},
		{
			name:        "failed extraction excluded",
			urls:        append(append([]string{}, urls[:4]...), "https://usage.example/missing"),
			wantCredits: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(ExtractRequest{
				URLs: urlList(test.urls), ExtractDepth: test.depth, IncludeUsage: true,
			})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			recorder := postExtract(
				t,
				extractHandler(documents),
				string(body),
				extractTestKey,
			)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			response := decodeExtract(t, recorder)
			if response.Usage == nil || response.Usage.Credits != test.wantCredits {
				t.Fatalf("usage=%#v, want credits=%d", response.Usage, test.wantCredits)
			}
		})
	}
}

func TestMapUsageAccountsForSuccessfulPageBuckets(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantCredits int
	}{
		{
			name:        "nine pages",
			body:        `{"url":"https://usage.example/","limit":9,"include_usage":true}`,
			wantCredits: 0,
		},
		{
			name:        "ten pages",
			body:        `{"url":"https://usage.example/","limit":10,"include_usage":true}`,
			wantCredits: 1,
		},
		{
			name: "ten pages with instructions",
			body: `{"url":"https://usage.example/","limit":10,` +
				`"instructions":"page","include_usage":true}`,
			wantCredits: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usage := requestCrawlUsage(t, PathMap, test.body, successfulPageSite(10))
			if usage == nil || usage.Credits != test.wantCredits {
				t.Fatalf("usage=%#v, want credits=%d", usage, test.wantCredits)
			}
		})
	}
}

func TestCrawlUsageComposesMappingAndExtraction(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantCredits int
	}{
		{
			name:        "basic",
			body:        `{"url":"https://usage.example/","limit":10,"include_usage":true}`,
			wantCredits: 3,
		},
		{
			name: "advanced extraction",
			body: `{"url":"https://usage.example/","limit":10,` +
				`"extract_depth":"advanced","include_usage":true}`,
			wantCredits: 5,
		},
		{
			name: "instructions",
			body: `{"url":"https://usage.example/","limit":10,` +
				`"instructions":"page","include_usage":true}`,
			wantCredits: 4,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usage := requestCrawlUsage(t, PathCrawl, test.body, successfulPageSite(10))
			if usage == nil || usage.Credits != test.wantCredits {
				t.Fatalf("usage=%#v, want credits=%d", usage, test.wantCredits)
			}
		})
	}
}

func requestSearchUsage(
	t *testing.T,
	body string,
	search *fakeSearcher,
) SearchResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, PathSearch, strings.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer "+searchTestKey)
	newTestSearchEndpoint(search, nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	return decodeSearchResponse(t, recorder)
}

func successfulExtractionDocuments(total int) (map[string]documentstore.Document, []string) {
	documents := make(map[string]documentstore.Document, total)
	urls := make([]string, 0, total)
	for page := range total {
		pageURL := fmt.Sprintf("https://usage.example/%d", page)
		documents[pageURL] = documentstore.Document{ExtractedText: "page content"}
		urls = append(urls, pageURL)
	}

	return documents, urls
}

func successfulPageSite(total int) sitePages {
	baseURL := "https://usage.example/"
	links := make([]string, 0, total-1)
	pages := sitePages{
		baseURL: {Title: "Page zero", Text: "page content"},
	}
	for page := 1; page < total; page++ {
		pageURL := fmt.Sprintf("https://usage.example/%d", page)
		links = append(links, pageURL)
		pages[pageURL] = CrawledPage{Title: fmt.Sprintf("Page %d", page), Text: "page content"}
	}
	home := pages[baseURL]
	home.Links = links
	pages[baseURL] = home

	return pages
}

func requestCrawlUsage(
	t *testing.T,
	path string,
	body string,
	pages sitePages,
) *SearchUsage {
	t.Helper()
	mux := http.NewServeMux()
	MountCrawl(mux, SearchAccessPolicy{BearerToken: crawlTestKey}, pages)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, path, strings.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer "+crawlTestKey)
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if path == PathMap {
		var response MapResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode map response: %v", err)
		}

		return response.Usage
	}
	var response CrawlResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode crawl response: %v", err)
	}

	return response.Usage
}
