package yacysearch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type fakeImpressionRecorder struct {
	query        string
	candidates   []ImpressionCandidate
	prepared     PreparedImpression
	prepareErr   error
	prepareCalls int
	token        string
	identity     string
	position     int
	recordErr    error
	recordCalls  int
}

func (r *fakeImpressionRecorder) PrepareImpression(
	_ context.Context,
	query string,
	candidates []ImpressionCandidate,
) (PreparedImpression, error) {
	r.prepareCalls++
	r.query = query
	r.candidates = append([]ImpressionCandidate(nil), candidates...)

	return r.prepared, r.prepareErr
}

func (r *fakeImpressionRecorder) RecordClick(
	_ context.Context,
	token string,
	identity string,
	position int,
) error {
	r.recordCalls++
	r.token = token
	r.identity = identity
	r.position = position

	return r.recordErr
}

func postClick(t *testing.T, endpoint http.Handler, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		pathSearchClick,
		strings.NewReader(form.Encode()),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	endpoint.ServeHTTP(recorder, request)

	return recorder
}

func TestClickEndpointRecordsSignedMembership(t *testing.T) {
	recorder := &fakeImpressionRecorder{}
	response := postClick(t, clickEndpoint{recorder: recorder}, url.Values{
		"t": {"signed-token"},
		"i": {"https://a.example/doc"},
		"p": {"3"},
	})
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
	if recorder.recordCalls != 1 || recorder.token != "signed-token" ||
		recorder.identity != "https://a.example/doc" || recorder.position != 3 {
		t.Fatalf("recorded = %#v", recorder)
	}
}

func TestClickEndpointRejectsMalformedRequests(t *testing.T) {
	recorder := &fakeImpressionRecorder{}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathSearchClick, nil)
	response := httptest.NewRecorder()
	clickEndpoint{recorder: recorder}.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed ||
		response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET status=%d allow=%q", response.Code, response.Header().Get("Allow"))
	}

	invalidForms := []url.Values{
		{},
		{"t": {"token"}, "i": {"identity"}, "p": {"bad"}},
		{"t": {"token"}, "i": {"identity"}, "p": {"0"}},
		{"t": {strings.Repeat("t", maximumClickTokenBytes+1)}, "i": {"identity"}, "p": {"1"}},
		{"t": {"token"}, "i": {strings.Repeat("i", maximumClickIdentityBytes+1)}, "p": {"1"}},
		{"t": {"token"}, "i": {" identity"}, "p": {"1"}},
	}
	for index, form := range invalidForms {
		response = postClick(t, clickEndpoint{recorder: recorder}, form)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid form %d status = %d", index, response.Code)
		}
	}

	unparsable := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		pathSearchClick,
		strings.NewReader("%zz"),
	)
	unparsable.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	clickEndpoint{recorder: recorder}.ServeHTTP(response, unparsable)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unparsable status = %d", response.Code)
	}

	oversize := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		pathSearchClick,
		strings.NewReader(strings.Repeat("x", maximumClickBodyBytes+1)),
	)
	oversize.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	clickEndpoint{recorder: recorder}.ServeHTTP(response, oversize)
	if response.Code != http.StatusBadRequest || recorder.recordCalls != 0 {
		t.Fatalf("oversize status=%d calls=%d", response.Code, recorder.recordCalls)
	}
}

func TestClickEndpointDoesNotExposeRecorderErrors(t *testing.T) {
	recorder := &fakeImpressionRecorder{recordErr: context.Canceled}
	response := postClick(t, clickEndpoint{recorder: recorder}, url.Values{
		"t": {"tampered-token"}, "i": {"identity"}, "p": {"1"},
	})
	if response.Code != http.StatusNoContent || recorder.recordCalls != 1 {
		t.Fatalf("status=%d calls=%d", response.Code, recorder.recordCalls)
	}
}

func TestMountEnablesCaptureOnlyWithRecorder(t *testing.T) {
	recorder := &fakeImpressionRecorder{}
	on := http.NewServeMux()
	Mount(on, &fakeSearch{}, nil, false, ClickCapture{Enabled: true, Recorder: recorder})
	if response := postClick(t, on, url.Values{
		"t": {"token"}, "i": {"identity"}, "p": {"1"},
	}); response.Code != http.StatusNoContent {
		t.Fatalf("enabled status = %d", response.Code)
	}
	off := http.NewServeMux()
	Mount(off, &fakeSearch{}, nil, false, ClickCapture{Recorder: recorder})
	if response := postClick(t, off, url.Values{
		"t": {"token"}, "i": {"identity"}, "p": {"1"},
	}); response.Code != http.StatusNotFound {
		t.Fatalf("disabled status = %d", response.Code)
	}
	if enabledImpressionRecorder(ClickCapture{Enabled: true}) != nil {
		t.Fatal("enabled nil recorder became non-nil")
	}
}

func TestHTMLCaptureIssuesTokenReordersAndKeepsDirectLinks(t *testing.T) {
	recorder := &fakeImpressionRecorder{prepared: PreparedImpression{
		Token: "signed-token",
		Order: []int{1, 0},
	}}
	body := renderSearchWithCapture(t, recorder, []searchcore.Result{
		{
			Title:     "First",
			URL:       "https://a.example/doc",
			URLHash:   "url-hash-a",
			ClusterID: "cluster-a",
		},
		{Title: "Second", URL: "https://b.example/doc"},
	}, 10)
	for _, expected := range []string{
		`data-t="signed-token"`,
		`data-i="https://a.example/doc"`,
		`data-p="11"`,
		`/searchclick`,
		`navigator.sendBeacon`,
		`body.set("t", token)`,
		`body.set("i",`,
		`href="https://a.example/doc"`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("page missing %q", expected)
		}
	}
	if strings.Index(body, "Second") > strings.Index(body, "First") {
		t.Fatal("prepared result order was not rendered")
	}
	if recorder.query != "foo" || len(recorder.candidates) != 2 ||
		recorder.candidates[0].ClusterIdentity != "cluster-a" ||
		recorder.candidates[1].ClusterIdentity != "https://b.example/doc" ||
		recorder.candidates[0].Position != 11 {
		t.Fatalf("prepared candidates = %#v", recorder.candidates)
	}
	for _, forbidden := range []string{`data-q=`, `body.set("q"`, `body.set("u"`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("page contains legacy beacon field %q", forbidden)
		}
	}
}

func TestHTMLCaptureFallsBackWithoutBreakingResults(t *testing.T) {
	results := []searchcore.Result{{Title: "Result", URL: "https://a.example/doc"}}
	recorders := []*fakeImpressionRecorder{
		{prepareErr: errors.New("unavailable")},
		{prepared: PreparedImpression{Order: []int{0}}},
		{prepared: PreparedImpression{Token: "token", Order: nil}},
		{prepared: PreparedImpression{Token: "token", Order: []int{1}}},
	}
	for index, recorder := range recorders {
		body := renderSearchWithCapture(t, recorder, results, 0)
		if !strings.Contains(body, `href="https://a.example/doc"`) ||
			strings.Contains(body, `data-t=`) {
			t.Fatalf("fallback %d body = %s", index, body)
		}
	}
	body := renderSearchWithCapture(t, nil, results, 0)
	if strings.Contains(body, "/searchclick") {
		t.Fatal("capture-disabled page contains beacon")
	}
	emptyRecorder := &fakeImpressionRecorder{}
	_ = renderSearchWithCapture(t, emptyRecorder, nil, 0)
	if emptyRecorder.prepareCalls != 0 {
		t.Fatalf("empty results prepared %d impressions", emptyRecorder.prepareCalls)
	}
}

func TestValidImpressionOrder(t *testing.T) {
	if !validImpressionOrder([]int{2, 0, 1}, 3) {
		t.Fatal("valid permutation rejected")
	}
	for _, order := range [][]int{{0}, {0, 0}, {-1, 0}, {0, 2}} {
		if validImpressionOrder(order, 2) {
			t.Fatalf("invalid order accepted: %v", order)
		}
	}
}

func renderSearchWithCapture(
	t *testing.T,
	recorder ImpressionRecorder,
	results []searchcore.Result,
	offset int,
) string {
	t.Helper()
	endpoint := htmlEndpoint{
		search: &fakeSearch{response: searchcore.Response{
			Request:      searchcore.Request{Query: "foo", Offset: offset},
			TotalResults: len(results),
			Results:      results,
		}},
		suggestions:  newRecentQueries(),
		clickCapture: recorder,
	}
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/yacysearch.html?query=foo&startRecord="+strconv.Itoa(offset),
		nil,
	)
	response := httptest.NewRecorder()
	endpoint.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}

	return response.Body.String()
}
