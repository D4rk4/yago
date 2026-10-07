package yagonode

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

func TestAuthenticatedOpsRequestLogsMatchedRouteAndBucketsUnmatchedPath(t *testing.T) {
	var output concurrentLogCapture
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	defer slog.SetDefault(previousLogger)
	handler, cookies := authenticatedOpsLogHandler(t, &output)
	updatedCookies := assertMatchedOpsRequestLog(t, handler, &output, cookies)
	if len(updatedCookies) == 0 {
		updatedCookies = cookies
	}
	assertUnmatchedOpsRequestLog(t, handler, &output, updatedCookies)
}

func authenticatedOpsLogHandler(
	t *testing.T,
	output *concurrentLogCapture,
) (http.Handler, []*http.Cookie) {
	t.Helper()
	endpoints := metrics.NewHTTPEndpointMetrics()
	service, err := provisionAdminAuth(
		t.Context(),
		nodeConfig{Admin: adminConfig{Username: "admin", Password: "pw"}},
		openTestVault(t),
		nil,
	)
	if err != nil {
		t.Fatalf("provision admin auth: %v", err)
	}
	opsMux := newOpsMux(endpoints.Handler(), nil, nil, nil, nil)
	handler := instrumentHTTP(endpoints, logHTTPRequests(guardAdminSurface(service, opsMux)))
	login := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, adminauth.PathLogin,
		strings.NewReader(`{"username":"admin","password":"pw"}`),
	)
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200", loginResponse.Code)
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login did not issue a session cookie")
	}
	output.Reset()

	return handler, cookies
}

func assertMatchedOpsRequestLog(
	t *testing.T,
	handler http.Handler,
	output *concurrentLogCapture,
	cookies []*http.Cookie,
) []*http.Cookie {
	t.Helper()
	const traceID = "11111111111111111111111111111111"
	const parentSpanID = "2222222222222222"
	const requestID = "request-id-private-canary"
	const query = "query-private-canary"
	matched := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/metrics?query="+query, nil,
	)
	matched.Header.Set(tracectx.Header, "00-"+traceID+"-"+parentSpanID+"-01")
	matched.Header.Set("X-Request-ID", requestID)
	for _, cookie := range cookies {
		matched.AddCookie(cookie)
	}
	matchedResponse := httptest.NewRecorder()
	handler.ServeHTTP(matchedResponse, matched)
	if matchedResponse.Code != http.StatusOK {
		t.Fatalf("matched ops request = %d, want 200", matchedResponse.Code)
	}
	matchedLog := singleOpsRequestLog(t, output)
	if matchedLog["path"] != "/metrics" || matchedLog["status"] != float64(http.StatusOK) {
		t.Fatalf("matched ops request log = %v", matchedLog)
	}
	serverSpanID, ok := matchedLog["serverSpanId"].(string)
	if !ok || len(serverSpanID) != 16 || serverSpanID == parentSpanID {
		t.Fatalf("matched ops request server span = %v", matchedLog["serverSpanId"])
	}
	assertOpsLogOmits(t, output.String(), traceID, parentSpanID, requestID, query)

	output.Reset()

	return matchedResponse.Result().Cookies()
}

func assertUnmatchedOpsRequestLog(
	t *testing.T,
	handler http.Handler,
	output *concurrentLogCapture,
	cookies []*http.Cookie,
) {
	t.Helper()
	const unmatchedPath = "/private-path-canary"
	const unmatchedQuery = "private-query-canary"
	unmatched := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, unmatchedPath+"?query="+unmatchedQuery, nil,
	)
	for _, cookie := range cookies {
		unmatched.AddCookie(cookie)
	}
	unmatchedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unmatchedResponse, unmatched)
	if unmatchedResponse.Code != http.StatusNotFound {
		t.Fatalf("unmatched ops request = %d, want 404", unmatchedResponse.Code)
	}
	unmatchedLog := singleOpsRequestLog(t, output)
	if unmatchedLog["path"] != "unmatched" ||
		unmatchedLog["status"] != float64(http.StatusNotFound) {
		t.Fatalf("unmatched ops request log = %v", unmatchedLog)
	}
	assertOpsLogOmits(t, output.String(), unmatchedPath, unmatchedQuery)
}

func singleOpsRequestLog(t *testing.T, output *concurrentLogCapture) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("ops request log count = %d, want 1", len(lines))
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("decode ops request log: %v", err)
	}

	return event
}

func assertOpsLogOmits(t *testing.T, logged string, canaries ...string) {
	t.Helper()
	for _, canary := range canaries {
		if strings.Contains(logged, canary) {
			t.Errorf("ops request log exposes %q", canary)
		}
	}
}
