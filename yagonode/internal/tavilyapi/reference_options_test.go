package tavilyapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestExtractReferenceOptions(t *testing.T) {
	rows := map[string]documentstore.Document{
		"https://content.example/page": {
			Title: "Content",
			ExtractedText: "Needle appears in the first useful sentence. " +
				"This sentence is unrelated. Needle appears in the second useful sentence.",
		},
	}
	rec := postExtract(
		t,
		extractHandler(rows),
		`{"urls":"https://content.example/page","query":"needle",`+
			`"chunks_per_source":2,"include_usage":true}`,
		extractTestKey,
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	response := decodeExtract(t, rec)
	if response.Usage == nil || response.Usage.Credits != 0 ||
		len(response.Results) != 1 ||
		strings.Count(response.Results[0].RawContent, "Needle appears") != 2 ||
		!strings.Contains(response.Results[0].RawContent, " [...] ") {
		t.Fatalf("response = %#v", response)
	}
}

func TestRelevantContentDefaultsToThreeChunks(t *testing.T) {
	text := "Needle appears in the first sufficiently long sentence. " +
		"Needle appears in the second sufficiently long sentence. " +
		"Needle appears in the third sufficiently long sentence. " +
		"Needle appears in the fourth sufficiently long sentence."
	content := requestedExtractContent(ExtractRequest{Query: "needle"}, text)
	if strings.Count(content, "Needle appears") != defaultRelevantChunks ||
		strings.Contains(content, "fourth") {
		t.Fatalf("default relevant content = %q", content)
	}
}

func TestBoundedImageURLs(t *testing.T) {
	images := []string{" ", "one", "two", "three", "four", "five", "six"}
	bounded := boundedImageURLs(images)
	if len(bounded) != maxResultImages || bounded[0] != "one" || bounded[4] != "five" {
		t.Fatalf("bounded images = %#v", bounded)
	}
}

func TestExtractReferenceOptionValidation(t *testing.T) {
	for _, body := range []string{
		`{"urls":"https://content.example/page","chunks_per_source":2}`,
		`{"urls":"https://content.example/page","query":"needle","chunks_per_source":6}`,
		`{"urls":"https://content.example/page","timeout":0.9}`,
		`{"urls":"https://content.example/page","timeout":60.1}`,
	} {
		rec := postExtract(t, extractHandler(nil), body, extractTestKey)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestCrawlReferenceOptions(t *testing.T) {
	site := testSite()
	home := site["https://site.example/"]
	home.Images = []string{"https://site.example/one.png", "https://site.example/two.png"}
	site["https://site.example/"] = home
	mux := http.NewServeMux()
	MountCrawl(mux, SearchAccessPolicy{BearerToken: crawlTestKey}, site)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathCrawl,
		strings.NewReader(`{"url":"https://site.example/","max_depth":5,`+
			`"max_breadth":500,"limit":7,"instructions":"welcome",`+
			`"chunks_per_source":1,"include_images":true,"extract_depth":"advanced",`+
			`"timeout":10,"include_usage":true}`),
	)
	req.Header.Set("Authorization", "Bearer "+crawlTestKey)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response CrawlResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.Usage == nil || response.Usage.Credits != 0 ||
		len(response.Results) == 0 || len(response.Results[0].Images) != 2 ||
		strings.HasPrefix(response.Results[0].RawContent, "# ") {
		t.Fatalf("response=%#v", response)
	}
}

func TestCrawlReferenceOptionValidation(t *testing.T) {
	for _, body := range []string{
		`{"url":"https://site.example/","max_depth":0}`,
		`{"url":"https://site.example/","max_depth":6}`,
		`{"url":"https://site.example/","max_breadth":501}`,
		`{"url":"https://site.example/","timeout":9.9}`,
		`{"url":"https://site.example/","timeout":150.1}`,
		`{"url":"https://site.example/","chunks_per_source":2}`,
		`{"url":"https://site.example/","instructions":"docs","chunks_per_source":6}`,
		`{"url":"https://site.example/","extract_depth":"deep"}`,
	} {
		rec := doCrawl(t, PathCrawl, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestCrawlAndMapDefaultWireShape(t *testing.T) {
	for _, path := range []string{PathCrawl, PathMap} {
		rec := doCrawl(t, path, `{"url":"https://site.example/","limit":1}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("path=%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("path=%s decode=%v", path, err)
		}
		if len(envelope) != 4 || envelope["base_url"] == nil || envelope["results"] == nil ||
			envelope["response_time"] == nil || envelope["request_id"] == nil {
			t.Fatalf("path=%s envelope=%v", path, envelope)
		}
		if path == PathCrawl && !strings.Contains(rec.Body.String(), `"raw_content":"# Home`) {
			t.Fatalf("crawl default is not markdown: %s", rec.Body.String())
		}
	}
}

func TestCrawlAcceptsBareHostAndCapsLargePositiveLimit(t *testing.T) {
	rec := doCrawl(t, PathMap, `{"url":"site.example","limit":201}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response MapResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.BaseURL != "https://site.example/" || len(response.Results) == 0 ||
		len(response.Results) > crawlMaxLimit {
		t.Fatalf("response=%#v", response)
	}
	limit := 201
	bounds, err := crawlBounds(CrawlRequest{Limit: &limit})
	if err != nil || bounds.limit != crawlMaxLimit {
		t.Fatalf("bounds=%#v error=%v", bounds, err)
	}
}

func TestCrawlRequestedEmptyImagesArePresent(t *testing.T) {
	rec := doCrawl(
		t,
		PathCrawl,
		`{"url":"https://single.example/preauthorized","limit":1,"include_images":true}`,
	)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"images":[]`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTavilyRawSurfacesUseDetailOnlyAuthErrors(t *testing.T) {
	mux := http.NewServeMux()
	MountCrawl(mux, SearchAccessPolicy{BearerToken: crawlTestKey}, testSite())
	for _, test := range []struct {
		path    string
		handler http.Handler
	}{
		{path: PathExtract, handler: extractHandler(nil)},
		{path: PathCrawl, handler: mux},
		{path: PathMap, handler: mux},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(
			t.Context(), http.MethodPost, test.path, strings.NewReader(`{}`),
		)
		test.handler.ServeHTTP(rec, req)
		var envelope map[string]json.RawMessage
		if rec.Code != http.StatusUnauthorized ||
			json.Unmarshal(rec.Body.Bytes(), &envelope) != nil ||
			len(envelope) != 1 || envelope["detail"] == nil {
			t.Fatalf("path=%s status=%d body=%s", test.path, rec.Code, rec.Body.String())
		}
	}
}
