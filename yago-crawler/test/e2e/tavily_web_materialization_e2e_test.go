//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const tavilyFixtureKey = "local-tavily-acceptance-key"

type tavilyDockerAcceptance struct {
	t       *testing.T
	ctx     context.Context
	client  *http.Client
	fixture webFallbackFixture
	network string
	node    nodeBroker
}

type tavilyFixtureSearchResponse struct {
	Query        string                      `json:"query"`
	Results      []tavilyFixtureSearchResult `json:"results"`
	ResponseTime float64                     `json:"response_time"`
	RequestID    string                      `json:"request_id"`
}

type tavilyFixtureSearchResult struct {
	Title      string          `json:"title"`
	URL        string          `json:"url"`
	Content    string          `json:"content"`
	RawContent json.RawMessage `json:"raw_content"`
	Score      float64         `json:"score"`
}

type tavilyFixtureExtractResponse struct {
	Results       []tavilyFixtureExtractResult  `json:"results"`
	FailedResults []tavilyFixtureExtractFailure `json:"failed_results"`
	ResponseTime  float64                       `json:"response_time"`
	RequestID     string                        `json:"request_id"`
}

type tavilyFixtureExtractResult struct {
	URL        string `json:"url"`
	RawContent string `json:"raw_content"`
}

type tavilyFixtureExtractFailure struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

type portalFixtureRow struct {
	url        string
	provenance string
}

func TestTavilyWebFallbackMaterializesIntoLocalIndex(t *testing.T) {
	acceptance := newTavilyDockerAcceptance(t)
	acceptance.requireSearchDepths()
	acceptance.requirePostgreSQLDomainScopes()
	englishURL := acceptance.fixtureURL("giraffe.example")
	russianURL := acceptance.fixtureURL("ru-giraffe.example")
	acceptance.requireInitialWebResult("adult giraffe weight", englishURL)
	acceptance.requireInitialWebResult("вес взрослого жирафа", russianURL)
	materializedURL := acceptance.fixtureURL("materialize.example")
	acceptance.requireInitialWebResult(
		"materialization sentinel quasar",
		materializedURL,
	)
	acceptance.requirePortalProvenance(
		"materialization sentinel quasar",
		materializedURL,
		"web",
	)
	acceptance.requirePortalAdvancedParity()

	startCrawlerForNode(t, acceptance.ctx, acceptance.network)
	acceptance.awaitBodyBearingResult(
		"materialization sentinel quasar",
		materializedURL,
	)
	acceptance.requirePortalProvenance(
		"materialization sentinel quasar",
		materializedURL,
		"local",
	)
	acceptance.awaitBodyBearingResult("adult giraffe weight", englishURL)
	acceptance.awaitBodyBearingResult("вес взрослого жирафа", russianURL)
	acceptance.requireExtractedLocalBody(materializedURL)
	if fetches := containerLogOccurrences(
		t,
		acceptance.ctx,
		acceptance.fixture.container,
		`"GET /materialize.html `,
	); fetches != 1 {
		t.Fatalf("materialized origin fetches = %d, want 1", fetches)
	}
	acceptance.requireIncompleteMiss()
}

func newTavilyDockerAcceptance(t *testing.T) *tavilyDockerAcceptance {
	t.Helper()
	ctx := context.Background()
	network := newNetwork(t, ctx)
	fixture := startWebFallbackFixture(t, ctx, network.Name)
	node := startNodeBrokerWithLaunch(t, ctx, network.Name, nodeBrokerLaunch{
		environment: map[string]string{
			"SSL_CERT_FILE":                               webFallbackAuthorityPath,
			"YAGO_EXTRACT_FETCH_ENABLED":                  "false",
			"YAGO_PUBLIC_SEARCH_UI_ENABLED":               "true",
			"YAGO_SEARCH_API_KEY":                         tavilyFixtureKey,
			"YAGO_SEEDLIST_URLS":                          "",
			"YAGO_SWARM_SEED_CRAWL":                       "false",
			"YAGO_WEB_FALLBACK_BACKEND":                   "ddg",
			"YAGO_WEB_FALLBACK_CACHE_TTL":                 "30s",
			"YAGO_WEB_FALLBACK_MAX_RESULTS":               "10",
			"YAGO_WEB_FALLBACK_PRIVACY":                   "always",
			"YAGO_WEB_FALLBACK_SEED_CRAWL":                "true",
			"YAGO_WEB_FALLBACK_SEED_DEPTH":                "0",
			"YAGO_WEB_FALLBACK_SEED_MAX_PAGES":            "20",
			"YAGO_WEB_FALLBACK_SAFESEARCH":                "moderate",
			"YAGO_WEB_FALLBACK_TIMEOUT":                   "1s",
			"YAGO_CRAWLER_RUN_PAGES_PER_MINUTE":           "0",
			"YAGO_CRAWLER_MAX_PAGES_PER_SECOND":           "20",
			"YAGO_CRAWLER_MAX_HOST_CONCURRENCY":           "1",
			"YAGO_CRAWLER_PRIORITIZE_AUTOMATIC_DISCOVERY": "true",
		},
		mounts: fixture.nodeMounts(t),
	})
	acceptance := &tavilyDockerAcceptance{
		t:       t,
		ctx:     ctx,
		client:  &http.Client{Timeout: 15 * time.Second},
		fixture: fixture,
		network: network.Name,
		node:    node,
	}
	acceptance.awaitReady()

	return acceptance
}

func (acceptance *tavilyDockerAcceptance) awaitReady() {
	acceptance.t.Helper()
	if !waitFor(90*time.Second, func() bool {
		request, err := http.NewRequestWithContext(
			acceptance.ctx,
			http.MethodGet,
			acceptance.node.opsURL+"/ready",
			nil,
		)
		if err != nil {
			return false
		}
		response, err := acceptance.client.Do(request)
		if err != nil {
			return false
		}
		defer func() { _ = response.Body.Close() }()

		return response.StatusCode == http.StatusOK
	}) {
		acceptance.t.Fatal("Tavily fixture node did not become ready")
	}
}

func (acceptance *tavilyDockerAcceptance) requireSearchDepths() {
	acceptance.t.Helper()
	for _, depth := range []string{"ultra-fast", "fast", "basic", "advanced"} {
		acceptance.t.Run(depth, func(t *testing.T) {
			response := acceptance.requireSearchOK(map[string]any{
				"query":               "PostgreSQL official documentation transactions",
				"search_depth":        depth,
				"max_results":         5,
				"include_answer":      false,
				"include_raw_content": false,
				"include_images":      false,
				"safe_search":         false,
			})
			if response.Query != "PostgreSQL official documentation transactions" ||
				len(response.Results) == 0 ||
				response.RequestID == "" ||
				response.ResponseTime <= 0 {
				t.Fatalf("depth %s response = %#v", depth, response)
			}
			for _, result := range response.Results {
				if result.Title == "" || result.URL == "" || result.Content == "" {
					t.Fatalf("depth %s result = %#v", depth, result)
				}
				if string(bytes.TrimSpace(result.RawContent)) != "null" {
					t.Fatalf(
						"depth %s raw_content = %s, want null",
						depth,
						result.RawContent,
					)
				}
			}
		})
	}
}

func (acceptance *tavilyDockerAcceptance) requirePostgreSQLDomainScopes() {
	acceptance.t.Helper()
	query := "PostgreSQL official documentation transactions"
	broad := acceptance.requireSearchOK(map[string]any{
		"query":        query,
		"search_depth": "basic",
		"max_results":  5,
	})
	if len(broad.Results) == 0 {
		acceptance.t.Fatal("broad PostgreSQL search returned no results")
	}
	parsed, err := url.Parse(broad.Results[0].URL)
	if err != nil || parsed.Hostname() == "" {
		acceptance.t.Fatalf("parse broad top URL %q: %v", broad.Results[0].URL, err)
	}
	for _, domain := range []string{"postgresql.org", parsed.Hostname()} {
		scoped := acceptance.requireSearchOK(map[string]any{
			"query":           query,
			"search_depth":    "basic",
			"max_results":     5,
			"include_domains": []string{domain},
		})
		if len(scoped.Results) == 0 {
			acceptance.t.Fatalf("scoped PostgreSQL search for %q is empty", domain)
		}
		for _, result := range scoped.Results {
			resultURL, parseErr := url.Parse(result.URL)
			if parseErr != nil ||
				!hostWithinDomain(resultURL.Hostname(), domain) {
				acceptance.t.Fatalf(
					"scoped result %q is outside %q: %v",
					result.URL,
					domain,
					parseErr,
				)
			}
		}
	}
}

func (acceptance *tavilyDockerAcceptance) requireInitialWebResult(
	query string,
	target string,
) {
	acceptance.t.Helper()
	response := acceptance.requireSearchOK(map[string]any{
		"query":               query,
		"search_depth":        "basic",
		"max_results":         10,
		"include_raw_content": true,
	})
	result, found := fixtureSearchResultByURL(response.Results, target)
	if !found {
		acceptance.t.Fatalf("%q did not return fixture URL %q", query, target)
	}
	if string(bytes.TrimSpace(result.RawContent)) != "null" {
		acceptance.t.Fatalf(
			"initial raw_content for %q = %s, want null",
			target,
			result.RawContent,
		)
	}
}

func (acceptance *tavilyDockerAcceptance) requirePortalAdvancedParity() {
	acceptance.t.Helper()
	query := "best mouse for gaming"
	advanced := acceptance.requireSearchOK(map[string]any{
		"query":        query,
		"search_depth": "advanced",
		"max_results":  10,
		"safe_search":  false,
	})
	rows := acceptance.portalRows(query)
	portalURLs := make([]string, 0, len(rows))
	for _, row := range rows {
		portalURLs = append(portalURLs, row.url)
	}
	tavilyURLs := make([]string, 0, len(advanced.Results))
	for _, result := range advanced.Results {
		tavilyURLs = append(tavilyURLs, result.URL)
	}
	if len(tavilyURLs) < 2 || !slices.Equal(portalURLs, tavilyURLs) {
		acceptance.t.Fatalf(
			"portal/Tavily URL order differs: portal=%v Tavily=%v",
			portalURLs,
			tavilyURLs,
		)
	}
}

func (acceptance *tavilyDockerAcceptance) awaitBodyBearingResult(
	query string,
	target string,
) {
	acceptance.t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	var last tavilyFixtureSearchResponse
	var lastStatus int
	for time.Now().Before(deadline) {
		fields := map[string]any{
			"query":               query,
			"search_depth":        "basic",
			"max_results":         10,
			"include_raw_content": true,
		}
		status, _, body := acceptance.postJSON("/search", fields)
		lastStatus = status
		if status == http.StatusOK {
			if err := json.Unmarshal(body, &last); err == nil {
				if result, found := fixtureSearchResultByURL(last.Results, target); found {
					if raw, ok := fixtureRawContent(result.RawContent); ok && raw != "" {
						return
					}
				}
			}
		}
		time.Sleep(time.Second)
	}
	acceptance.t.Fatalf(
		"body for %q did not materialize: status=%d response=%#v",
		target,
		lastStatus,
		last,
	)
}

func (acceptance *tavilyDockerAcceptance) requireExtractedLocalBody(target string) {
	acceptance.t.Helper()
	status, _, body := acceptance.postJSON("/extract", map[string]any{
		"urls":          []string{target},
		"extract_depth": "basic",
	})
	if status != http.StatusOK {
		acceptance.t.Fatalf("extract status = %d body=%s", status, body)
	}
	var response tavilyFixtureExtractResponse
	if err := json.Unmarshal(body, &response); err != nil {
		acceptance.t.Fatalf("decode extract response: %v body=%s", err, body)
	}
	if len(response.Results) != 1 ||
		response.Results[0].URL != target ||
		!strings.Contains(response.Results[0].RawContent, "Materialization sentinel quasar") ||
		len(response.FailedResults) != 0 ||
		response.ResponseTime <= 0 ||
		response.RequestID == "" {
		acceptance.t.Fatalf("extract response = %#v", response)
	}
}

func (acceptance *tavilyDockerAcceptance) requireIncompleteMiss() {
	acceptance.t.Helper()
	status, headers, body := acceptance.postJSON("/search", map[string]any{
		"query":        "incomplete-miss-zqxjv-no-document",
		"search_depth": "basic",
		"max_results":  5,
	})
	if status != http.StatusServiceUnavailable || headers.Get("Retry-After") != "1" {
		acceptance.t.Fatalf(
			"incomplete miss = status %d retry %q body=%s",
			status,
			headers.Get("Retry-After"),
			body,
		)
	}
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope) != 1 {
		acceptance.t.Fatalf("incomplete miss envelope = %#v error=%v", envelope, err)
	}
	detail, ok := envelope["detail"].(map[string]any)
	if !ok || len(detail) != 1 {
		acceptance.t.Fatalf("incomplete miss detail = %#v", envelope["detail"])
	}
	message, ok := detail["error"].(string)
	if !ok || !strings.Contains(message, "search unavailable") {
		acceptance.t.Fatalf("incomplete miss error = %#v", detail["error"])
	}
}

func (acceptance *tavilyDockerAcceptance) requirePortalProvenance(
	query string,
	target string,
	want string,
) {
	acceptance.t.Helper()
	for _, row := range acceptance.portalRows(query) {
		if row.url == target {
			if row.provenance != want {
				acceptance.t.Fatalf(
					"portal provenance for %q = %q, want %q",
					target,
					row.provenance,
					want,
				)
			}
			return
		}
	}
	acceptance.t.Fatalf("portal query %q omitted %q", query, target)
}

func (acceptance *tavilyDockerAcceptance) portalRows(query string) []portalFixtureRow {
	acceptance.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		status, headers, body := acceptance.getPortal(query)
		if status == http.StatusOK {
			return parsePortalFixtureRows(string(body))
		}
		if status != http.StatusTooManyRequests || time.Now().After(deadline) {
			acceptance.t.Fatalf("portal status = %d body=%s", status, body)
		}
		delay := time.Second
		if seconds, err := strconv.Atoi(headers.Get("Retry-After")); err == nil && seconds > 0 {
			delay = time.Duration(seconds) * time.Second
		}
		if time.Now().Add(delay).After(deadline) {
			acceptance.t.Fatalf("portal remained rate limited: body=%s", body)
		}
		time.Sleep(delay)
	}
}

func (acceptance *tavilyDockerAcceptance) getPortal(
	query string,
) (int, http.Header, []byte) {
	acceptance.t.Helper()
	request, err := http.NewRequestWithContext(
		acceptance.ctx,
		http.MethodGet,
		acceptance.node.publicURL+"/?q="+url.QueryEscape(query),
		nil,
	)
	if err != nil {
		acceptance.t.Fatalf("build portal request: %v", err)
	}
	response, err := acceptance.client.Do(request)
	if err != nil {
		acceptance.t.Fatalf("portal request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		acceptance.t.Fatalf("read portal response: %v", err)
	}

	return response.StatusCode, response.Header.Clone(), body
}

func (acceptance *tavilyDockerAcceptance) requireSearchOK(
	fields map[string]any,
) tavilyFixtureSearchResponse {
	acceptance.t.Helper()
	status, _, body := acceptance.postJSON("/search", fields)
	if status != http.StatusOK {
		acceptance.t.Fatalf("search status = %d body=%s", status, body)
	}
	var response tavilyFixtureSearchResponse
	if err := json.Unmarshal(body, &response); err != nil {
		acceptance.t.Fatalf("decode search response: %v body=%s", err, body)
	}

	return response
}

func (acceptance *tavilyDockerAcceptance) postJSON(
	path string,
	fields map[string]any,
) (int, http.Header, []byte) {
	acceptance.t.Helper()
	encoded, err := json.Marshal(fields)
	if err != nil {
		acceptance.t.Fatalf("encode %s request: %v", path, err)
	}
	request, err := http.NewRequestWithContext(
		acceptance.ctx,
		http.MethodPost,
		acceptance.node.publicURL+path,
		bytes.NewReader(encoded),
	)
	if err != nil {
		acceptance.t.Fatalf("build %s request: %v", path, err)
	}
	request.Header.Set("Authorization", "Bearer "+tavilyFixtureKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := acceptance.client.Do(request)
	if err != nil {
		acceptance.t.Fatalf("%s request: %v", path, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		acceptance.t.Fatalf("read %s response: %v", path, err)
	}

	return response.StatusCode, response.Header.Clone(), body
}

func (acceptance *tavilyDockerAcceptance) fixtureURL(host string) string {
	acceptance.t.Helper()
	for _, row := range acceptance.fixture.rows {
		if row.host == host {
			return "http://" + row.host + row.path
		}
	}
	acceptance.t.Fatalf("fixture host %q is absent", host)

	return ""
}

func fixtureSearchResultByURL(
	results []tavilyFixtureSearchResult,
	target string,
) (tavilyFixtureSearchResult, bool) {
	for _, result := range results {
		if result.URL == target {
			return result, true
		}
	}

	return tavilyFixtureSearchResult{}, false
}

func fixtureRawContent(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", false
	}
	var content string
	if json.Unmarshal(raw, &content) != nil {
		return "", false
	}

	return content, true
}

func hostWithinDomain(host string, domain string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))

	return host == domain || strings.HasSuffix(host, "."+domain)
}

func parsePortalFixtureRows(document string) []portalFixtureRow {
	const (
		rowStart        = `<li class="result">`
		titleURLStart   = `<a class="title" href="`
		provenanceStart = `class="prov prov-`
	)
	rows := make([]portalFixtureRow, 0)
	for {
		index := strings.Index(document, rowStart)
		if index < 0 {
			return rows
		}
		document = document[index+len(rowStart):]
		end := strings.Index(document, "</li>")
		if end < 0 {
			return rows
		}
		rowDocument := document[:end]
		document = document[end+len("</li>"):]
		urlValue, found := quotedPortalValue(rowDocument, titleURLStart)
		if !found {
			continue
		}
		provenance, _ := quotedPortalValue(rowDocument, provenanceStart)
		rows = append(rows, portalFixtureRow{
			url:        html.UnescapeString(urlValue),
			provenance: provenance,
		})
	}
}

func quotedPortalValue(document string, marker string) (string, bool) {
	start := strings.Index(document, marker)
	if start < 0 {
		return "", false
	}
	document = document[start+len(marker):]
	end := strings.IndexByte(document, '"')
	if end < 0 {
		return "", false
	}

	return document[:end], true
}
