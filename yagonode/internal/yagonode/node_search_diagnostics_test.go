package yagonode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/publicportal"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

type searchDiagnosticFixture struct {
	response searchcore.Response
	err      error
}

func (s searchDiagnosticFixture) Search(
	_ context.Context,
	_ searchcore.Request,
) (searchcore.Response, error) {
	return s.response, s.err
}

type searchExecutionDiagnosticCase struct {
	name              string
	response          searchcore.Response
	err               error
	wantLevel         string
	wantStage         string
	wantCause         string
	wantErrorCategory string
}

func startDiagnosticSpan(t *testing.T) (context.Context, string) {
	t.Helper()
	ctx, _ := tracectx.StartServerSpan(
		t.Context(),
		"00-11111111111111112222222222222222-3333333333333333-01",
	)
	serverSpanID, ok := tracectx.ServerSpanIDFromContext(ctx)
	if !ok {
		t.Fatal("server span ID missing")
	}

	return ctx, serverSpanID
}

func decodeDiagnosticLog(t *testing.T, logged string) map[string]any {
	t.Helper()
	if strings.TrimSpace(logged) == "" {
		t.Fatal("search execution diagnostic missing")
	}
	lines := strings.Split(strings.TrimSpace(logged), "\n")
	if len(lines) != 1 {
		t.Fatalf("diagnostic event count = %d, want one: %s", len(lines), logged)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("decode diagnostic event: %v", err)
	}

	return event
}

func assertDiagnosticCanariesAbsent(t *testing.T, logged string, canaries ...string) {
	t.Helper()
	for _, canary := range canaries {
		if strings.Contains(logged, canary) {
			t.Fatalf("diagnostic event exposed %q: %s", canary, logged)
		}
	}
}

func TestAssembledSearchLogsBoundedSourceSummary(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	ctx, serverSpanID := startDiagnosticSpan(t)
	search := assemblePublicSearcher(searchDiagnosticFixture{
		response: sourceLossFixtureResponse(),
	}, nil, publicSearchAssembly{})
	response, err := search.Search(ctx, searchcore.Request{
		Query:  "PRIVATE_QUERY_CANARY",
		Source: searchcore.SourceLocal,
		Limit:  5,
	})
	if err != nil || len(response.Results) != 0 || len(response.PartialFailures) == 0 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}

	event := decodeDiagnosticLog(t, output.String())
	if event["msg"] != "search execution incomplete" || event["level"] != "WARN" ||
		event["serverSpanId"] != serverSpanID || event["retrievedRows"] != float64(0) ||
		event["totalResults"] != float64(0) {
		t.Fatalf("diagnostic event = %#v", event)
	}
	counts, ok := event["failureCounts"].([]any)
	if !ok || len(counts) != 1 {
		t.Fatalf("failure counts = %#v", event["failureCounts"])
	}
	count, ok := counts[0].(map[string]any)
	if !ok || count["stage"] != "remote-peer" || count["cause"] != "deadline" ||
		count["count"] != float64(len(response.PartialFailures)) {
		t.Fatalf("failure count = %#v", counts[0])
	}
	assertDiagnosticCanariesAbsent(t, output.String(),
		"PRIVATE_QUERY_CANARY",
		"PRIVATE_URL_CANARY_A",
		"PRIVATE_URL_CANARY_B",
		"PRIVATE_ERROR_CANARY_A",
		"PRIVATE_ERROR_CANARY_B",
		"PEER_HASH_CANARY_A",
		"PEER_HASH_CANARY_B",
		"11111111111111112222222222222222",
		"3333333333333333",
	)
}

func sourceLossFixtureResponse() searchcore.Response {
	return searchcore.Response{PartialFailures: []searchcore.PartialFailure{
		{
			Source: "PEER_HASH_CANARY_A",
			Reason: "PRIVATE_URL_CANARY_A PRIVATE_ERROR_CANARY_A",
			Diagnostic: searchcore.PartialFailureDiagnostic{
				Stage: searchcore.FailureStageRemotePeer,
				Cause: searchcore.FailureCauseDeadline,
			},
		},
		{
			Source: "PEER_HASH_CANARY_B",
			Reason: "PRIVATE_URL_CANARY_B PRIVATE_ERROR_CANARY_B",
			Diagnostic: searchcore.PartialFailureDiagnostic{
				Stage: searchcore.FailureStageRemotePeer,
				Cause: searchcore.FailureCauseDeadline,
			},
		},
	}}
}

func TestSearchExecutionDiagnosticsClassifiesIncompleteOutcomes(t *testing.T) {
	for _, test := range searchExecutionDiagnosticCases() {
		t.Run(test.name, func(t *testing.T) {
			assertSearchExecutionDiagnosticCase(t, test)
		})
	}
}

func searchExecutionDiagnosticCases() []searchExecutionDiagnosticCase {
	return append(
		searchExecutionDiagnosticResponseCases(),
		searchExecutionDiagnosticErrorCases()...,
	)
}

func searchExecutionDiagnosticResponseCases() []searchExecutionDiagnosticCase {
	queryShape := queryShapeDiagnosticFailure()
	remoteFailure := peerDeadlineDiagnosticFailure()
	return []searchExecutionDiagnosticCase{
		{name: "honest empty"},
		{
			name:     "query shape only",
			response: searchcore.Response{PartialFailures: []searchcore.PartialFailure{queryShape}},
		},
		{
			name: "zero with source loss",
			response: searchcore.Response{
				PartialFailures: []searchcore.PartialFailure{remoteFailure},
			},
			wantLevel: "WARN",
			wantStage: "remote-peer",
			wantCause: "deadline",
		},
		{
			name: "zero with inner capacity refusal",
			response: searchcore.Response{PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceRemoteStage,
				Reason: "capacity text remains internal",
				Diagnostic: searchcore.PartialFailureDiagnostic{
					Stage: searchcore.FailureStageRemoteStage,
					Cause: searchcore.FailureCauseCapacity,
				},
			}}},
			wantLevel: "WARN",
			wantStage: "remote-stage",
			wantCause: "capacity",
		},
		{
			name: "zero with response budget exhaustion",
			response: searchcore.Response{PartialFailures: []searchcore.PartialFailure{{
				Source: "PEER_HASH_CANARY",
				Reason: "budget text remains internal",
				Diagnostic: searchcore.PartialFailureDiagnostic{
					Stage: searchcore.FailureStageRemotePeer,
					Cause: searchcore.FailureCauseBudget,
				},
			}}},
			wantLevel: "WARN",
			wantStage: "remote-peer",
			wantCause: "budget",
		},
		{
			name: "row-bearing partial",
			response: searchcore.Response{
				Results:         []searchcore.Result{{URL: "https://safe.example/"}},
				TotalResults:    1,
				PartialFailures: []searchcore.PartialFailure{remoteFailure},
			},
			wantLevel: "DEBUG",
			wantStage: "remote-peer",
			wantCause: "deadline",
		},
	}
}

func searchExecutionDiagnosticErrorCases() []searchExecutionDiagnosticCase {
	errorOnly := privateDiagnosticFailure()
	return []searchExecutionDiagnosticCase{
		{
			name:              "error only",
			err:               errorOnly,
			wantLevel:         "WARN",
			wantErrorCategory: "other",
		},
		{
			name: "error with query shape note",
			response: searchcore.Response{
				PartialFailures: []searchcore.PartialFailure{queryShapeDiagnosticFailure()},
			},
			err:               errorOnly,
			wantLevel:         "WARN",
			wantErrorCategory: "other",
		},
		{
			name:              "deadline error",
			err:               context.DeadlineExceeded,
			wantLevel:         "WARN",
			wantErrorCategory: "deadline",
		},
		{
			name:              "canceled error",
			err:               context.Canceled,
			wantLevel:         "WARN",
			wantErrorCategory: "canceled",
		},
		{
			name:              "capacity error",
			err:               errInteractiveSearchCapacity,
			wantLevel:         "WARN",
			wantErrorCategory: "capacity",
		},
	}
}

func queryShapeDiagnosticFailure() searchcore.PartialFailure {
	return searchcore.PartialFailure{
		Source: searchcore.PartialFailureSourceQueryShape,
		Reason: "no query terms",
		Diagnostic: searchcore.PartialFailureDiagnostic{
			Stage: searchcore.FailureStageQueryShape,
			Cause: searchcore.FailureCauseNoTarget,
		},
	}
}

func peerDeadlineDiagnosticFailure() searchcore.PartialFailure {
	return searchcore.PartialFailure{
		Source: "PEER_HASH_CANARY",
		Reason: "PRIVATE_ERROR_CANARY PRIVATE_URL_CANARY",
		Diagnostic: searchcore.PartialFailureDiagnostic{
			Stage: searchcore.FailureStageRemotePeer,
			Cause: searchcore.FailureCauseDeadline,
		},
	}
}

func privateDiagnosticFailure() error {
	return errors.New("PRIVATE_ERROR_CANARY PRIVATE_URL_CANARY")
}

func assertSearchExecutionDiagnosticCase(t *testing.T, test searchExecutionDiagnosticCase) {
	t.Helper()
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	ctx, _ := startDiagnosticSpan(t)
	got, gotErr := withSearchExecutionDiagnostics(searchDiagnosticFixture{
		response: test.response,
		err:      test.err,
	}).Search(ctx, searchcore.Request{Query: "PRIVATE_QUERY_CANARY"})
	if !reflect.DeepEqual(got, test.response) || !errors.Is(gotErr, test.err) {
		t.Fatalf("response/error changed: %#v/%v", got, gotErr)
	}
	if test.wantLevel == "" {
		if output.Len() != 0 {
			t.Fatalf("complete search logged diagnostics: %s", output.String())
		}
		return
	}
	assertSearchDiagnosticEvent(t, test, output.String())
}

func assertSearchDiagnosticEvent(
	t *testing.T,
	test searchExecutionDiagnosticCase,
	logged string,
) {
	t.Helper()
	event := decodeDiagnosticLog(t, logged)
	if event["msg"] != searchExecutionDiagnosticMessage || event["level"] != test.wantLevel {
		t.Fatalf("diagnostic event = %#v", event)
	}
	if test.err != nil && event["errorCategory"] != test.wantErrorCategory {
		t.Fatalf("error category = %#v, want %q", event["errorCategory"], test.wantErrorCategory)
	}
	if test.wantStage != "" {
		assertDiagnosticFailureCount(t, event, test.wantStage, test.wantCause)
	}
	assertDiagnosticCanariesAbsent(t, logged,
		"PRIVATE_QUERY_CANARY",
		"PRIVATE_ERROR_CANARY",
		"PRIVATE_URL_CANARY",
		"PEER_HASH_CANARY",
		"11111111111111112222222222222222",
		"3333333333333333",
	)
}

func assertDiagnosticFailureCount(
	t *testing.T,
	event map[string]any,
	wantStage, wantCause string,
) {
	t.Helper()
	counts, ok := event["failureCounts"].([]any)
	if !ok || len(counts) != 1 {
		t.Fatalf("failure counts = %#v", event["failureCounts"])
	}
	count, ok := counts[0].(map[string]any)
	if !ok || count["stage"] != wantStage || count["cause"] != wantCause ||
		count["count"] != float64(1) {
		t.Fatalf("failure count = %#v", counts[0])
	}
}

func TestTavilyDiagnosticAndAccessLogsShareServerSpan(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	mux := http.NewServeMux()
	mountNodePublicSearch(mux, publicSearchAssembly{
		storage: nodeStorage{
			postings:     publicSearchPostingIndex{},
			urlDirectory: publicSearchURLDirectory{},
		},
		identity:     nodeidentity.Identity{NetworkName: "freeworld"},
		dht:          defaultPublicSearchDHTConfig(),
		client:       http.DefaultClient,
		searchAPIKey: "PRIVATE_TOKEN_CANARY",
	})
	handler := instrumentHTTP(metrics.NewHTTPEndpointMetrics(), logHTTPRequests(mux))
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/search",
		strings.NewReader(`{"query":"PRIVATE_QUERY_CANARY"}`),
	)
	request.Header.Set("Authorization", "Bearer PRIVATE_TOKEN_CANARY")
	request.Header.Set("X-Request-ID", "PRIVATE_REQUEST_ID_CANARY")
	parentTraceID := "11111111111111112222222222222222"
	parentSpanID := "3333333333333333"
	request.Header.Set("traceparent", "00-"+parentTraceID+"-"+parentSpanID+"-01")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || response.Header().Get("Retry-After") != "1" {
		t.Fatalf("status/retry = %d/%q", response.Code, response.Header().Get("Retry-After"))
	}
	if !strings.Contains(
		response.Body.String(),
		`"detail":{"error":"search unavailable: one or more search sources did not complete"}`,
	) {
		t.Fatalf("409 body changed: %s", response.Body.String())
	}

	var diagnosticSpan string
	var diagnosticFound, accessFound bool
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		spanID, _ := record["serverSpanId"].(string)
		switch record["msg"] {
		case searchExecutionDiagnosticMessage:
			diagnosticFound = true
			diagnosticSpan = spanID
		case requestFailedMessage:
			accessFound = true
			if record["path"] != "/search" || record["status"] != float64(http.StatusConflict) {
				t.Fatalf("access record = %#v", record)
			}
		}
	}
	if !diagnosticFound || !accessFound || diagnosticSpan == "" {
		t.Fatalf("diagnostic/access logs missing server span: %s", output.String())
	}
	if !strings.Contains(output.String(), `"serverSpanId":"`+diagnosticSpan+`"`) {
		t.Fatalf("access log did not share diagnostic span %q: %s", diagnosticSpan, output.String())
	}
	if diagnosticSpan == parentTraceID || diagnosticSpan == parentSpanID {
		t.Fatalf("server span reused caller trace material: %q", diagnosticSpan)
	}
	for _, canary := range []string{
		"PRIVATE_QUERY_CANARY",
		"PRIVATE_TOKEN_CANARY",
		"PRIVATE_REQUEST_ID_CANARY",
		parentTraceID,
		parentSpanID,
	} {
		if strings.Contains(output.String(), canary) {
			t.Fatalf("search diagnostic exposed %q: %s", canary, output.String())
		}
	}
}

func TestPortalSearchErrorDoesNotLogSourceError(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	ctx, _ := tracectx.StartServerSpan(
		t.Context(),
		"00-11111111111111112222222222222222-3333333333333333-01",
	)
	serverSpanID, ok := tracectx.ServerSpanIDFromContext(ctx)
	if !ok {
		t.Fatal("server span ID missing")
	}
	search := withSearchExecutionDiagnostics(searchDiagnosticFixture{
		err: errors.New("PRIVATE_ERROR_CANARY https://PRIVATE_URL_CANARY/"),
	})
	portal := publicportal.New(newPortalSource(search), false)
	request := httptest.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"/?q=PRIVATE_QUERY_CANARY",
		nil,
	)
	response := httptest.NewRecorder()
	portal.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("portal status = %d, want 200", response.Code)
	}
	logged := output.String()
	for _, message := range []string{
		searchExecutionDiagnosticMessage,
		"public portal search failed",
	} {
		if !strings.Contains(logged, `"msg":"`+message+`"`) ||
			!strings.Contains(logged, `"serverSpanId":"`+serverSpanID+`"`) {
			t.Fatalf("%s missing shared span %q: %s", message, serverSpanID, logged)
		}
	}
	for _, canary := range []string{
		"PRIVATE_QUERY_CANARY",
		"PRIVATE_ERROR_CANARY",
		"PRIVATE_URL_CANARY",
		"11111111111111112222222222222222",
		"3333333333333333",
	} {
		if strings.Contains(logged, canary) {
			t.Fatalf("portal log exposed %q: %s", canary, logged)
		}
	}
}

func TestPublicAndAdminSearchSurfacesShareDiagnostics(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	toggles := &runtimeToggles{}
	toggles.SetPortalEnabled(true)
	mux := http.NewServeMux()
	searcher, _ := mountNodePublicSearch(mux, publicSearchAssembly{
		storage: nodeStorage{
			postings:     publicSearchPostingIndex{},
			urlDirectory: publicSearchURLDirectory{},
		},
		identity:     nodeidentity.Identity{NetworkName: "freeworld"},
		dht:          defaultPublicSearchDHTConfig(),
		client:       http.DefaultClient,
		searchAPIKey: "PRIVATE_TOKEN_CANARY",
		toggles:      toggles,
	})
	handler := instrumentHTTP(metrics.NewHTTPEndpointMetrics(), logHTTPRequests(mux))
	parentTraceID := "11111111111111112222222222222222"
	parentSpanID := "3333333333333333"
	traceparent := "00-" + parentTraceID + "-" + parentSpanID + "-01"
	assertDiagnosticHTTPRoute(t, output.String, handler, traceparent, diagnosticHTTPRoute{
		method:     http.MethodPost,
		target:     "/search",
		body:       `{"query":"PRIVATE_QUERY_CANARY"}`,
		wantStatus: http.StatusConflict,
	})
	assertDiagnosticHTTPRoute(t, output.String, handler, traceparent, diagnosticHTTPRoute{
		method:     http.MethodGet,
		target:     "/yacysearch.json?query=PRIVATE_QUERY_CANARY",
		wantStatus: http.StatusOK,
	})
	assertDiagnosticHTTPRoute(t, output.String, handler, traceparent, diagnosticHTTPRoute{
		method:     http.MethodGet,
		target:     "/?q=PRIVATE_QUERY_CANARY",
		wantStatus: http.StatusOK,
	})

	ctx, _ := tracectx.StartServerSpan(t.Context(), traceparent)
	serverSpanID, ok := tracectx.ServerSpanIDFromContext(ctx)
	if !ok {
		t.Fatal("admin search server span ID missing")
	}
	before := strings.Count(output.String(), searchExecutionDiagnosticMessage)
	_, err := newSearchSource(searcher).Search(ctx, adminui.SearchQuery{
		Query:  "PRIVATE_QUERY_CANARY",
		Global: true,
		Limit:  5,
	})
	if err != nil {
		t.Fatalf("admin search: %v", err)
	}
	assertOneSearchDiagnosticAdded(t, output.String, before)
	if !strings.Contains(output.String(), `"serverSpanId":"`+serverSpanID+`"`) {
		t.Fatalf("admin diagnostic lacks server span %q: %s", serverSpanID, output.String())
	}
	for _, canary := range []string{
		"PRIVATE_QUERY_CANARY",
		"PRIVATE_TOKEN_CANARY",
		"PRIVATE_REQUEST_ID_CANARY",
		parentTraceID,
		parentSpanID,
	} {
		if strings.Contains(output.String(), canary) {
			t.Fatalf("surface diagnostic exposed %q: %s", canary, output.String())
		}
	}
}

type diagnosticHTTPRoute struct {
	method     string
	target     string
	body       string
	wantStatus int
}

func assertDiagnosticHTTPRoute(
	t *testing.T,
	logged func() string,
	handler http.Handler,
	traceparent string,
	route diagnosticHTTPRoute,
) {
	t.Helper()
	before := strings.Count(logged(), searchExecutionDiagnosticMessage)
	request := httptest.NewRequestWithContext(
		t.Context(), route.method, route.target, strings.NewReader(route.body),
	)
	request.Header.Set(tracectx.Header, traceparent)
	request.Header.Set("X-Request-ID", "PRIVATE_REQUEST_ID_CANARY")
	if route.method == http.MethodPost {
		request.Header.Set("Authorization", "Bearer PRIVATE_TOKEN_CANARY")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != route.wantStatus {
		t.Fatalf(
			"%s %s status = %d, want %d",
			route.method,
			route.target,
			response.Code,
			route.wantStatus,
		)
	}
	assertOneSearchDiagnosticAdded(t, logged, before)
}

func assertOneSearchDiagnosticAdded(t *testing.T, logged func() string, before int) {
	t.Helper()
	if got := strings.Count(logged(), searchExecutionDiagnosticMessage) - before; got != 1 {
		t.Fatalf("diagnostic count change = %d, want one: %s", got, logged())
	}
}

func TestAdminExplainSearchUsesOneSharedDiagnostic(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	ctx, _ := tracectx.StartServerSpan(
		t.Context(),
		"00-11111111111111112222222222222222-3333333333333333-01",
	)
	source := assemblePublicExplanationSearcher(searchDiagnosticFixture{
		response: searchcore.Response{PartialFailures: []searchcore.PartialFailure{{
			Source: "PEER_HASH_CANARY",
			Reason: "PRIVATE_ERROR_CANARY PRIVATE_URL_CANARY",
			Diagnostic: searchcore.PartialFailureDiagnostic{
				Stage: searchcore.FailureStageRemotePeer,
				Cause: searchcore.FailureCauseDeadline,
			},
		}}},
	}, nil, publicSearchAssembly{})
	endpoint := newSearchExplainEndpoint(stubSearchIndex{}, nil, nil, nil, nil).withGlobal(source)
	for _, scope := range []searchcore.Source{searchcore.SourceLocal, searchcore.SourceGlobal} {
		before := strings.Count(output.String(), searchExecutionDiagnosticMessage)
		response, status, err := endpoint.explanation(ctx, searchExplainRequest{
			Query: "PRIVATE_QUERY_CANARY",
			Scope: scope,
		})
		if err != nil || status != http.StatusOK || len(response.PartialFailures) == 0 {
			t.Fatalf("scope %s response/status/error = %#v/%d/%v", scope, response, status, err)
		}
		if got := strings.Count(
			output.String(),
			searchExecutionDiagnosticMessage,
		) - before; got != 1 {
			t.Fatalf("scope %s diagnostic count = %d, want one: %s", scope, got, output.String())
		}
	}
	for _, canary := range []string{
		"PRIVATE_QUERY_CANARY",
		"PRIVATE_ERROR_CANARY",
		"PRIVATE_URL_CANARY",
		"PEER_HASH_CANARY",
		"11111111111111112222222222222222",
		"3333333333333333",
	} {
		if strings.Contains(output.String(), canary) {
			t.Fatalf("admin explanation diagnostic exposed %q: %s", canary, output.String())
		}
	}
}
