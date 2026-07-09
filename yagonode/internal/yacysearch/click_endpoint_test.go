package yacysearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type fakeRecorder struct {
	query, target string
	rank          int
	err           error
	calls         int
}

func (r *fakeRecorder) Record(_ context.Context, query, target string, rank int) error {
	r.calls++
	r.query, r.target, r.rank = query, target, rank

	return r.err
}

func postClick(t *testing.T, h http.Handler, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		pathSearchClick,
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestClickEndpointRecordsBeacon(t *testing.T) {
	recorder := &fakeRecorder{}
	rec := postClick(t, clickEndpoint{recorder: recorder}, url.Values{
		"q": {"go generics"},
		"u": {"https://a.example/doc"},
		"p": {"3"},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if recorder.calls != 1 || recorder.query != "go generics" ||
		recorder.target != "https://a.example/doc" || recorder.rank != 3 {
		t.Fatalf("recorded %+v, want q/u/p forwarded", recorder)
	}
}

func TestClickEndpointRejectsNonPost(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathSearchClick, nil)
	rec := httptest.NewRecorder()
	clickEndpoint{recorder: &fakeRecorder{}}.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestClickEndpointRejectsBadInput(t *testing.T) {
	cases := map[string]url.Values{
		"missing url":   {"q": {"go"}},
		"missing query": {"u": {"https://a.example/"}},
		"relative url":  {"q": {"go"}, "u": {"/local/path"}},
		"js scheme":     {"q": {"go"}, "u": {"javascript:alert(1)"}},
	}
	for name, form := range cases {
		t.Run(name, func(t *testing.T) {
			recorder := &fakeRecorder{}
			rec := postClick(t, clickEndpoint{recorder: recorder}, form)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if recorder.calls != 0 {
				t.Fatalf("recorder called %d times, want 0", recorder.calls)
			}
		})
	}
}

func TestClickEndpointSwallowsRecorderError(t *testing.T) {
	recorder := &fakeRecorder{err: context.Canceled}
	rec := postClick(t, clickEndpoint{recorder: recorder}, url.Values{
		"q": {"go"},
		"u": {"https://a.example/"},
		"p": {"1"},
	})
	// The beacon is best-effort: a store error must not disturb navigation.
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 despite the recorder error", rec.Code)
	}
	if recorder.calls != 1 {
		t.Fatalf("recorder called %d times, want 1", recorder.calls)
	}
}

func TestMountRegistersClickEndpointOnlyWhenEnabled(t *testing.T) {
	recorder := &fakeRecorder{}

	on := http.NewServeMux()
	Mount(on, &fakeSearch{}, nil, false, ClickCapture{Enabled: true, Recorder: recorder})
	if rec := postClick(t, on, url.Values{
		"q": {"go"}, "u": {"https://a.example/"}, "p": {"1"},
	}); rec.Code != http.StatusNoContent {
		t.Fatalf("enabled: status = %d, want 204", rec.Code)
	}

	off := http.NewServeMux()
	Mount(off, &fakeSearch{}, nil, false, ClickCapture{})
	if rec := postClick(t, off, url.Values{
		"q": {"go"}, "u": {"https://a.example/"}, "p": {"1"},
	}); rec.Code != http.StatusNotFound {
		t.Fatalf("disabled: status = %d, want 404 (endpoint not mounted)", rec.Code)
	}
}

func renderSearch(t *testing.T, capture bool) string {
	t.Helper()
	endpoint := htmlEndpoint{
		search: &fakeSearch{response: searchcore.Response{
			TotalResults: 1,
			Results: []searchcore.Result{{
				Title:      "Result",
				URL:        "https://a.example/doc",
				DisplayURL: "a.example/doc",
			}},
		}},
		suggestions:  newRecentQueries(),
		clickCapture: capture,
	}
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/yacysearch.html?query=foo",
		nil,
	)
	rec := httptest.NewRecorder()
	endpoint.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	return rec.Body.String()
}

func TestHTMLResultsEmitBeaconWhenCaptureEnabled(t *testing.T) {
	body := renderSearch(t, true)
	for _, want := range []string{
		`data-q="foo"`,
		`data-p="1"`,
		"/searchclick",
		"navigator.sendBeacon",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("capture-enabled page missing %q", want)
		}
	}
}

func TestHTMLResultsOmitBeaconWhenCaptureDisabled(t *testing.T) {
	body := renderSearch(t, false)
	for _, absent := range []string{"data-q=", "data-p=", "/searchclick", "sendBeacon"} {
		if strings.Contains(body, absent) {
			t.Errorf("capture-disabled page unexpectedly contains %q", absent)
		}
	}
}

func TestIsHTTPURL(t *testing.T) {
	cases := map[string]bool{
		"https://a.example/":  true,
		"http://a.example/":   true,
		"/relative/path":      false,
		"javascript:alert(1)": false,
		"http:///only-path":   false,
		"http://\x7fbad":      false,
	}
	for raw, want := range cases {
		if got := isHTTPURL(raw); got != want {
			t.Errorf("isHTTPURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
