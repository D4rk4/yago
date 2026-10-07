package yagonode

import (
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

func TestRuntimeServerFailureLogsUseDistinctServerSpansAndSafeRoutes(t *testing.T) {
	var output concurrentLogCapture
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previousLogger)

	traces := map[string]tracectx.Trace{}
	endpoints := metrics.NewHTTPEndpointMetrics()
	handlers := runtimeServerFailureHandlers(t, endpoints, traces)

	input := runtimeServerFailureInput{
		traceID:            "11111111111111111111111111111111",
		parentSpanID:       "2222222222222222",
		clientRequestID:    "client-request-id-sentinel",
		queryValue:         "query-sentinel",
		authorizationValue: "Bearer authorization-sentinel",
	}
	requests := []runtimeServerFailureRequest{
		{server: "public search", route: "/search"},
		{server: "peer protocol", route: "/yacy/test"},
		{server: "ops", route: "/admin/test"},
	}
	issueRuntimeServerFailureRequests(
		t,
		handlers,
		requests,
		input,
	)
	assertRuntimeServerFailureLogs(
		t,
		output.String(),
		requests,
		traces,
		input,
	)
	assertRuntimeServerFailureMetrics(t, endpoints, requests)
}

type runtimeServerFailureRequest struct {
	server string
	route  string
}

type runtimeServerFailureInput struct {
	traceID            string
	parentSpanID       string
	clientRequestID    string
	queryValue         string
	authorizationValue string
}

type runtimeServerFailureLog struct {
	index   int
	line    string
	request runtimeServerFailureRequest
	trace   tracectx.Trace
}

func runtimeServerFailureHandlers(
	t *testing.T,
	endpoints *metrics.HTTPEndpointMetrics,
	traces map[string]tracectx.Trace,
) map[string]http.Handler {
	t.Helper()
	searchHandler := func(route string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			trace, ok := tracectx.FromContext(r.Context())
			if !ok {
				t.Error("public request has no trace context")
			} else {
				traces[route] = trace
			}
			w.WriteHeader(http.StatusConflict)
		}
	}
	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/search", searchHandler("/search"))
	peerMux := http.NewServeMux()
	peerMux.HandleFunc("/yacy/test", searchHandler("/yacy/test"))
	opsMux := http.NewServeMux()
	opsMux.HandleFunc("/admin/test", searchHandler("/admin/test"))
	servers := buildRuntimeServers(
		nodeConfig{
			PublicAddr: "127.0.0.1:0",
			PeerAddr:   "127.0.0.1:0",
			OpsAddr:    "127.0.0.1:0",
		},
		endpoints,
		node{publicMux: publicMux, peerMux: peerMux},
		&runtimeToggles{},
		opsMux,
	)
	handlers := map[string]http.Handler{}
	for _, named := range servers {
		handlers[named.name] = named.server.Handler
	}

	return handlers
}

func issueRuntimeServerFailureRequests(
	t *testing.T,
	handlers map[string]http.Handler,
	requests []runtimeServerFailureRequest,
	input runtimeServerFailureInput,
) {
	t.Helper()
	for _, item := range requests {
		request := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			"http://127.0.0.1"+item.route+"?query="+input.queryValue,
			nil,
		)
		request.Header.Set("X-Request-ID", input.clientRequestID)
		request.Header.Set("Authorization", input.authorizationValue)
		request.Header.Set(
			tracectx.Header,
			"00-"+input.traceID+"-"+input.parentSpanID+"-01",
		)
		response := httptest.NewRecorder()
		handler := handlers[item.server]
		if handler == nil {
			t.Fatalf("missing %s handler", item.server)
		}
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusConflict {
			t.Errorf("%s status = %d, want %d", item.server, response.Code, http.StatusConflict)
		}
	}
}

func assertRuntimeServerFailureLogs(
	t *testing.T,
	output string,
	requests []runtimeServerFailureRequest,
	traces map[string]tracectx.Trace,
	input runtimeServerFailureInput,
) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != len(requests) {
		t.Fatalf("request log count = %d, want %d", len(lines), len(requests))
	}
	serverSpanIDs := map[string]bool{}
	for index, line := range lines {
		serverSpanID := assertRuntimeServerFailureLog(
			t,
			runtimeServerFailureLog{
				index:   index,
				line:    line,
				request: requests[index],
				trace:   traces[requests[index].route],
			},
			input,
		)
		if serverSpanID == "" {
			continue
		}
		if serverSpanIDs[serverSpanID] {
			t.Errorf("separate server requests reused span ID %q", serverSpanID)
		}
		serverSpanIDs[serverSpanID] = true
	}
	assertRequestLogRedacts(t, output,
		input.traceID,
		input.parentSpanID,
		input.clientRequestID,
		input.queryValue,
		input.authorizationValue,
	)
}

func assertRuntimeServerFailureLog(
	t *testing.T,
	input runtimeServerFailureLog,
	want runtimeServerFailureInput,
) string {
	t.Helper()
	entry := map[string]any{}
	if err := json.Unmarshal([]byte(input.line), &entry); err != nil {
		t.Fatalf("decode request log %d: %v", input.index, err)
	}
	if entry["msg"] != requestFailedMessage || entry["path"] != input.request.route ||
		entry["status"] != float64(http.StatusConflict) {
		t.Errorf("request log %d has unexpected event fields", input.index)
	}
	durationMs, ok := entry["durationMs"].(float64)
	if !ok || durationMs < 0 {
		t.Errorf("request log %d has invalid durationMs: %v", input.index, entry["durationMs"])
	}
	if _, present := entry["duration_ms"]; present {
		t.Errorf("request log %d contains legacy duration_ms", input.index)
	}
	serverSpanID, ok := entry["serverSpanId"].(string)
	if !ok || len(serverSpanID) != 16 {
		t.Errorf("request log %d has no server span ID", input.index)
		return ""
	}
	if _, err := hex.DecodeString(serverSpanID); err != nil {
		t.Errorf("request log %d server span ID is not hexadecimal", input.index)
	}
	if input.trace.TraceID != want.traceID || !input.trace.Sampled {
		t.Errorf("request log %d lost parent trace identity: %+v", input.index, input.trace)
	}
	if input.trace.SpanID == want.parentSpanID || serverSpanID != input.trace.SpanID {
		t.Errorf("request log %d did not use the server span from context", input.index)
	}

	return serverSpanID
}

func assertRequestLogRedacts(t *testing.T, output string, values ...string) {
	t.Helper()
	for _, value := range values {
		if strings.Contains(output, value) {
			t.Errorf("request log contains caller-controlled value %q", value)
		}
	}
}

func assertRuntimeServerFailureMetrics(
	t *testing.T,
	endpoints *metrics.HTTPEndpointMetrics,
	requests []runtimeServerFailureRequest,
) {
	t.Helper()
	metricsResponse := httptest.NewRecorder()
	endpoints.Handler().
		ServeHTTP(metricsResponse, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	metricsBody := metricsResponse.Body.String()
	for _, request := range requests {
		route := request.route
		want := `http_requests_total{code="409",endpoint="` + route + `"} 1`
		if !strings.Contains(metricsBody, want) {
			t.Errorf("HTTP metrics missing one 409 for %s", route)
		}
	}
}

func TestRuntimeServerFailureLogBucketsUnmatchedPath(t *testing.T) {
	var output concurrentLogCapture
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previousLogger)

	servers := buildRuntimeServers(
		nodeConfig{PublicAddr: "127.0.0.1:0"},
		metrics.NewHTTPEndpointMetrics(),
		node{publicMux: http.NewServeMux()},
		&runtimeToggles{},
		http.NewServeMux(),
	)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://127.0.0.1/unmatched-path-sentinel?query=unmatched-query-sentinel",
		nil,
	)
	response := httptest.NewRecorder()
	for _, named := range servers {
		if named.name == "public search" {
			named.server.Handler.ServeHTTP(response, request)
			break
		}
	}
	if response.Code != http.StatusNotFound {
		t.Fatalf("unmatched route status = %d, want %d", response.Code, http.StatusNotFound)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("request log count = %d, want 1", len(lines))
	}
	entry := map[string]any{}
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("decode request log: %v", err)
	}
	if entry["path"] != "unmatched" {
		t.Errorf("unmatched path log = %v, want fixed unmatched route", entry["path"])
	}
	for _, value := range []string{"unmatched-path-sentinel", "unmatched-query-sentinel"} {
		if strings.Contains(output.String(), value) {
			t.Errorf("request log contains unmatched input %q", value)
		}
	}
}
