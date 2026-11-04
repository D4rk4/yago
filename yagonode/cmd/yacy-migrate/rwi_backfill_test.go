package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type failedRWIBackfillWriter struct{}

func (failedRWIBackfillWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type failedRWIBackfillTransport struct{ err error }

func (t failedRWIBackfillTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

type failedRWIBackfillBody struct{ err error }

func (b failedRWIBackfillBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b failedRWIBackfillBody) Close() error             { return b.err }

func TestRWIBackfillCommandStartsBoundedDryRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/index/rwi-backfill" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fixture-key" {
			t.Errorf("authorization = %q", got)
		}
		var request rwiBackfillRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Version != 1 || request.Action != "start" ||
			request.MaximumDocuments != 64 || !request.DryRun {
			t.Errorf("request = %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rwiBackfillStatus{State: "running", DryRun: true})
	}))
	defer server.Close()
	t.Setenv("YAGO_API_KEY", "fixture-key")
	var output strings.Builder
	err := run(context.Background(), []string{
		"rwi-backfill", "--action=start", "--endpoint=" + server.URL + "/admin/index/rwi-backfill",
		"--maximum-documents=64", "--dry-run",
	}, &output)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(output.String(), `"state":"running"`) ||
		!strings.Contains(output.String(), `"dryRun":true`) {
		t.Fatalf("status output = %q", output.String())
	}
}

func TestRWIBackfillCommandReadsStatusWithBearerAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fixture-key" {
			t.Errorf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"state":"complete","scannedDocuments":3}`))
	}))
	defer server.Close()
	t.Setenv("YAGO_API_KEY", "fixture-key")
	var output strings.Builder
	err := run(context.Background(), []string{
		"rwi-backfill", "--endpoint=" + server.URL + "/admin/index/rwi-backfill",
	}, &output)
	if err != nil || !strings.Contains(output.String(), `"scannedDocuments":3`) {
		t.Fatalf("run = %v, output = %q", err, output.String())
	}
}

func TestRWIBackfillCommandRejectsInvalidTargetsAndActions(t *testing.T) {
	for _, opts := range []rwiBackfillOptions{
		{Endpoint: "http://example.test/admin/index/rwi-backfill", APIKey: "key", Action: "start"},
		{Endpoint: "ftp://example.test/admin/index/rwi-backfill", APIKey: "key", Action: "status"},
		{Endpoint: "https://example.test/wrong", APIKey: "key", Action: "status"},
		{Endpoint: "https://user:password@example.test/admin/index/rwi-backfill", APIKey: "key", Action: "status"},
		{Endpoint: "https://example.test/admin/index/rwi-backfill?token=x", APIKey: "key", Action: "status"},
		{Endpoint: "https://example.test/admin/index/rwi-backfill#status", APIKey: "key", Action: "status"},
		{Endpoint: "https://example.test/admin/index/rwi-backfill", APIKey: "key", Action: "delete"},
		{Endpoint: "https://example.test/admin/index/rwi-backfill", APIKey: "key", Action: "start", MaximumDocuments: -1},
		{Endpoint: "https://example.test/admin/index/rwi-backfill", APIKey: "key", Action: "resume", MaximumDocuments: 10_000_001},
		{Endpoint: "https://example.test/admin/index/rwi-backfill", APIKey: "key", Action: "status", MaximumDocuments: 1},
		{Endpoint: "https://example.test/admin/index/rwi-backfill", APIKey: "key", Action: "pause", DryRun: true},
		{Endpoint: "https://example.test/admin/index/rwi-backfill", Action: "status"},
	} {
		if _, err := validateRWIBackfillOptions(opts); err == nil {
			t.Fatalf("invalid options accepted: %+v", opts)
		}
	}
	if _, err := validateRWIBackfillOptions(rwiBackfillOptions{
		Endpoint: "https://example.test/admin/index/rwi-backfill", APIKey: "key", Action: "pause",
	}); err != nil {
		t.Fatalf("valid pause rejected: %v", err)
	}
	if _, err := validateRWIBackfillOptions(rwiBackfillOptions{
		Endpoint: "http://localhost/admin/index/rwi-backfill", APIKey: "key", Action: "status",
	}); err != nil {
		t.Fatalf("valid loopback endpoint rejected: %v", err)
	}
	for _, host := range []string{"localhost", "LOCALHOST", "127.0.0.1", "::1"} {
		if !loopbackEndpoint(host) {
			t.Errorf("loopbackEndpoint(%q) = false", host)
		}
	}
	for _, host := range []string{"example.test", "192.0.2.1", "invalid address"} {
		if loopbackEndpoint(host) {
			t.Errorf("loopbackEndpoint(%q) = true", host)
		}
	}
}

func TestRWIBackfillCommandRejectsParsingAndOutputFailures(t *testing.T) {
	t.Setenv("YAGO_API_KEY", "fixture-key")
	if err := runRWIBackfillCommand(
		context.Background(), []string{"--unknown"}, io.Discard,
	); err == nil {
		t.Fatal("command accepted unknown option")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"state":"complete"}`))
	}))
	defer server.Close()
	if err := runRWIBackfillCommand(context.Background(), []string{
		"--endpoint=" + server.URL + "/admin/index/rwi-backfill",
	}, failedRWIBackfillWriter{}); err == nil {
		t.Fatal("command ignored status output write failure")
	}
	if err := runRWIBackfillCommand(context.Background(), []string{
		"--endpoint=https://example.test/wrong",
	}, io.Discard); err == nil {
		t.Fatal("command ignored endpoint validation failure")
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/unexpected", http.StatusFound)
	}))
	defer redirect.Close()
	if err := runRWIBackfillCommand(context.Background(), []string{
		"--endpoint=" + redirect.URL + "/admin/index/rwi-backfill",
	}, io.Discard); err == nil {
		t.Fatal("command followed or accepted an HTTP redirect")
	}
}

func TestCallRWIBackfillValidatesResponseAndSurfacesTransportErrors(t *testing.T) {
	endpoint, err := url.Parse("https://example.test/admin/index/rwi-backfill")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	for name, testCase := range map[string]struct {
		status int
		body   string
	}{
		"http error":         {status: http.StatusBadGateway, body: `{}`},
		"malformed json":     {status: http.StatusOK, body: `{`},
		"unknown field":      {status: http.StatusOK, body: `{"state":"ok","unexpected":true}`},
		"trailing value":     {status: http.StatusOK, body: `{"state":"ok"} {}`},
		"oversized response": {status: http.StatusOK, body: `{"state":"` + strings.Repeat("x", 17<<10) + `"}`},
	} {
		t.Run(name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripperFunc(
				func(request *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: testCase.status,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(testCase.body)),
						Request:    request,
					}, nil
				},
			)}
			if _, err := callRWIBackfill(
				t.Context(), client, endpoint,
				rwiBackfillOptions{Action: "status", APIKey: "key"},
			); err == nil {
				t.Fatal("callRWIBackfill accepted invalid response")
			}
		})
	}
	if _, err := callRWIBackfill(
		t.Context(),
		&http.Client{Transport: failedRWIBackfillTransport{err: errors.New("dial failed")}},
		endpoint,
		rwiBackfillOptions{Action: "status", APIKey: "key"},
	); err == nil {
		t.Fatal("callRWIBackfill ignored transport failure")
	}
	invalidEndpoint := &url.URL{
		Scheme: "https",
		Host:   "bad host",
		Path:   "/admin/index/rwi-backfill",
	}
	if _, err := callRWIBackfill(
		t.Context(), http.DefaultClient, invalidEndpoint,
		rwiBackfillOptions{Action: "status", APIKey: "key"},
	); err == nil || !strings.Contains(err.Error(), "create rwi backfill request") {
		t.Fatalf("callRWIBackfill malformed URL error = %v", err)
	}
	closeErr := errors.New("close failed")
	client := &http.Client{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK, Header: make(http.Header),
				Body: failedRWIBackfillBody{err: closeErr}, Request: request,
			}, nil
		}),
	}
	if _, err := callRWIBackfill(
		t.Context(), client, endpoint,
		rwiBackfillOptions{Action: "status", APIKey: "key"},
	); !errors.Is(err, closeErr) {
		t.Fatalf("callRWIBackfill close error = %v", err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
