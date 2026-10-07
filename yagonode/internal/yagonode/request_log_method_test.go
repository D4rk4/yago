package yagonode

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLogMethodPreservesStandardMethods(t *testing.T) {
	for _, method := range []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodConnect,
		http.MethodOptions,
		http.MethodTrace,
	} {
		if got := requestLogMethod(method); got != method {
			t.Errorf("requestLogMethod(%q) = %q, want original method", method, got)
		}
	}
}

func TestRequestLogMethodBucketsCustomAndEmptyValues(t *testing.T) {
	for _, method := range []string{"PRIVATE-CUSTOM-METHOD", ""} {
		if got := requestLogMethod(method); got != "other" {
			t.Errorf("requestLogMethod(%q) = %q, want other", method, got)
		}
	}
}

func TestRequestLogBucketsMethodWithoutChangingHandlerRequest(t *testing.T) {
	var output concurrentLogCapture
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previousLogger)

	const customMethod = "PRIVATE-CUSTOM-METHOD"
	var observedMethod string
	handler := logHTTPRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedMethod = r.Method
		w.WriteHeader(http.StatusConflict)
	}))
	request := httptest.NewRequestWithContext(
		t.Context(),
		customMethod,
		"http://example.test/search",
		nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if observedMethod != customMethod {
		t.Fatalf("handler received method %q, want %q", observedMethod, customMethod)
	}
	if response.Code != http.StatusConflict {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusConflict)
	}

	entry := map[string]any{}
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode request log: %v", err)
	}
	if entry["method"] != "other" {
		t.Errorf("logged method = %v, want other", entry["method"])
	}
	if strings.Contains(output.String(), customMethod) {
		t.Error("request log contains private custom method")
	}
}
