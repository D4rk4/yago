package yagonode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yacynode/internal/events"
)

func serveEvents(
	t *testing.T,
	recorder *events.Recorder,
	target string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	newEventsEndpoint(recorder).ServeHTTP(rec, req)

	return rec
}

func TestEventsEndpointReturnsNewestFirst(t *testing.T) {
	recorder := events.NewRecorder(8)
	recorder.Record(events.SeverityInfo, events.CategoryConfig, "node.started", "node started")
	recorder.Record(
		events.SeverityWarn,
		events.CategorySecurity,
		"admin.login.failed",
		"admin login failed",
	)

	rec := serveEvents(t, recorder, pathEvents)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}

	var body eventsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.GeneratedAt == "" {
		t.Fatal("generatedAt is empty")
	}
	if len(body.Events) != 2 {
		t.Fatalf("events = %d, want 2", len(body.Events))
	}
	if body.Events[0].Name != "admin.login.failed" ||
		body.Events[0].Category != "security" ||
		body.Events[0].Severity != "warn" {
		t.Fatalf("newest event = %#v", body.Events[0])
	}
}

func TestEventsEndpointHonorsLimit(t *testing.T) {
	recorder := events.NewRecorder(8)
	for _, name := range []string{"a", "b", "c"} {
		recorder.Record(events.SeverityInfo, events.CategoryConfig, name, name)
	}

	rec := serveEvents(t, recorder, pathEvents+"?limit=1")
	var body eventsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Events) != 1 || body.Events[0].Name != "c" {
		t.Fatalf("events = %#v", body.Events)
	}
}

func TestEventsEndpointEmptyReturnsEmptyArray(t *testing.T) {
	rec := serveEvents(t, events.NewRecorder(8), pathEvents)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); !jsonHasEmptyEvents(t, got) {
		t.Fatalf("body = %s, want empty events array", got)
	}
}

func jsonHasEmptyEvents(t *testing.T, raw string) bool {
	t.Helper()
	var body eventsResponse
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	return body.Events != nil && len(body.Events) == 0
}

func TestEventsEndpointRejectsBadLimit(t *testing.T) {
	for _, raw := range []string{"abc", "0", "-4"} {
		rec := serveEvents(t, events.NewRecorder(8), pathEvents+"?limit="+raw)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("limit %q code = %d, want 400", raw, rec.Code)
		}
	}
}

func TestEventsEndpointRejectsNonGet(t *testing.T) {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, pathEvents, nil)
	rec := httptest.NewRecorder()
	newEventsEndpoint(events.NewRecorder(8)).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d, want 405", rec.Code)
	}
}
