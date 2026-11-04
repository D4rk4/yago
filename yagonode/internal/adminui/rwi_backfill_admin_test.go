package adminui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type rwiBackfillFixture struct {
	actions []string
	status  RWIBackfillStatus
	err     error
}

func (f *rwiBackfillFixture) Start(
	_ context.Context,
	maximum int64,
	dryRun bool,
) (RWIBackfillStatus, error) {
	f.actions = append(f.actions, "start")
	f.status = RWIBackfillStatus{State: "running", MaximumDocumentsRun: maximum, DryRun: dryRun}
	return f.status, f.err
}

func (f *rwiBackfillFixture) Pause(context.Context) (RWIBackfillStatus, error) {
	f.actions = append(f.actions, "pause")
	f.status.State = "paused"
	return f.status, f.err
}

func (f *rwiBackfillFixture) Resume(
	_ context.Context,
	maximum int64,
	dryRun bool,
) (RWIBackfillStatus, error) {
	f.actions = append(f.actions, "resume")
	f.status = RWIBackfillStatus{State: "running", MaximumDocumentsRun: maximum, DryRun: dryRun}
	return f.status, f.err
}

func (f *rwiBackfillFixture) Status(context.Context) (RWIBackfillStatus, error) {
	return f.status, f.err
}

func TestRWIBackfillAdminControlsStartPauseResumeAndStatus(t *testing.T) {
	controller := &rwiBackfillFixture{status: RWIBackfillStatus{State: "idle"}}
	console := New(Options{RWIBackfill: controller})
	for _, requestBody := range []string{
		`{"version":1,"action":"start","maximumDocuments":100,"dryRun":true}`,
		`{"version":1,"action":"pause"}`,
		`{"version":1,"action":"resume","maximumDocuments":0}`,
	} {
		request := httptest.NewRequestWithContext(
			t.Context(), http.MethodPost, rwiBackfillPath, strings.NewReader(requestBody),
		)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		console.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("POST %s = %d %s", requestBody, response.Code, response.Body.String())
		}
		var status RWIBackfillStatus
		if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
			t.Fatalf("decode status: %v", err)
		}
	}
	if strings.Join(controller.actions, ",") != "start,pause,resume" {
		t.Fatalf("actions = %q", controller.actions)
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, rwiBackfillPath, nil)
	response := httptest.NewRecorder()
	console.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"state":"running"`) {
		t.Fatalf("GET status = %d %s", response.Code, response.Body.String())
	}
}

func TestRWIBackfillAdminRejectsInvalidRequestsBeforeControl(t *testing.T) {
	controller := new(rwiBackfillFixture)
	console := New(Options{RWIBackfill: controller})
	for _, body := range []string{
		`{"version":2,"action":"start"}`,
		`{"version":1,"action":"remove"}`,
		`{"version":1,"action":"start","maximumDocuments":-1}`,
		`{"version":1,"action":"pause","dryRun":true}`,
		`{"version":1,"action":"start","unknown":true}`,
		`{"version":1,"action":"start"} {"version":1}`,
	} {
		request := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			rwiBackfillPath,
			strings.NewReader(body),
		)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		console.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("body %s returned %d", body, response.Code)
		}
	}
	if len(controller.actions) != 0 {
		t.Fatalf("invalid requests invoked control actions %q", controller.actions)
	}
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		rwiBackfillPath,
		strings.NewReader(`{}`),
	)
	response := httptest.NewRecorder()
	console.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("missing JSON content type returned %d", response.Code)
	}
}

func TestRWIBackfillAdminUnavailableAndControllerFailures(t *testing.T) {
	console := New(Options{})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		request := httptest.NewRequestWithContext(t.Context(), method, rwiBackfillPath, nil)
		response := httptest.NewRecorder()
		console.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("nil controller %s = %d, want 404", method, response.Code)
		}
	}
	controller := &rwiBackfillFixture{
		status: RWIBackfillStatus{State: "running"},
		err:    errors.New("fixture controller unavailable"),
	}
	console = New(Options{RWIBackfill: controller})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, rwiBackfillPath, nil)
	response := httptest.NewRecorder()
	console.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed status = %d, want 503", response.Code)
	}
	request = httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, rwiBackfillPath,
		strings.NewReader(`{"version":1,"action":"start"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	console.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("failed start = %d, want 409", response.Code)
	}
	request = httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, rwiBackfillPath,
		strings.NewReader(`{"version":1,"action":"unsupported"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	console.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unsupported action = %d, want 400", response.Code)
	}
}

func TestRWIBackfillAdminRejectsMalformedAndOversizedBodies(t *testing.T) {
	for _, test := range []struct {
		name        string
		contentType string
		body        io.ReadCloser
		want        int
	}{
		{
			name:        "wrong media type",
			contentType: "text/plain",
			body:        io.NopCloser(strings.NewReader(`{}`)),
			want:        http.StatusUnsupportedMediaType,
		},
		{
			name:        "body read error",
			contentType: "application/json",
			body:        errorReadCloser{},
			want:        http.StatusRequestEntityTooLarge,
		},
		{
			name:        "oversized body",
			contentType: "application/json",
			body:        io.NopCloser(strings.NewReader(strings.Repeat("x", 4097))),
			want:        http.StatusRequestEntityTooLarge,
		},
		{
			name:        "malformed json",
			contentType: "application/json",
			body:        io.NopCloser(strings.NewReader("{")),
			want:        http.StatusBadRequest,
		},
		{
			name:        "over maximum",
			contentType: "application/json",
			body:        io.NopCloser(strings.NewReader(`{"version":1,"action":"start","maximumDocuments":10000001}`)),
			want:        http.StatusBadRequest,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodPost,
				rwiBackfillPath,
				nil,
			)
			request.Body = test.body
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			_, ok := decodeRWIBackfillRequest(response, request)
			if ok || response.Code != test.want {
				t.Fatalf(
					"decode ok=%v status=%d, want rejection with %d",
					ok,
					response.Code,
					test.want,
				)
			}
		})
	}
}

type errorReadCloser struct{}

func (errorReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("fixture read failure")
}

func (errorReadCloser) Close() error {
	return nil
}

type errorResponseWriter struct{}

func (errorResponseWriter) Header() http.Header {
	return make(http.Header)
}

func (errorResponseWriter) WriteHeader(int) {}

func (errorResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("fixture write failure")
}

func TestWriteRWIBackfillStatusHandlesWriteFailure(t *testing.T) {
	writeRWIBackfillStatus(errorResponseWriter{}, RWIBackfillStatus{State: "idle"})
}
