package crawldispatch_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/crawldispatch"
)

type recordingQueue struct {
	order     yacycrawlcontract.CrawlOrder
	published bool
	err       error
}

func (q *recordingQueue) Publish(_ context.Context, order yacycrawlcontract.CrawlOrder) error {
	q.order = order
	q.published = true
	return q.err
}

func mount(t *testing.T, queue crawldispatch.CrawlOrderQueue) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	crawldispatch.MountCrawlDispatch(mux, queue)
	return mux
}

func post(t *testing.T, mux *http.ServeMux, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		crawldispatch.PathCrawlDispatch,
		strings.NewReader(body),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestDispatchBuildsOrderFromOperatorInput(t *testing.T) {
	queue := &recordingQueue{}
	mux := mount(t, queue)

	rec := post(t, mux, `{
		"name": "docs",
		"seeds": ["https://example.org/a", "https://example.org/b"],
		"scope": "domain",
		"maxDepth": 3,
		"maxPagesPerHost": 50,
		"crawlDelay": "1s"
	}`)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if !queue.published {
		t.Fatal("order was not published")
	}
	order := queue.order
	wantSeeds := []string{"https://example.org/a", "https://example.org/b"}
	if len(order.SeedURLs) != len(wantSeeds) {
		t.Fatalf("seedURLs = %d, want %d", len(order.SeedURLs), len(wantSeeds))
	}
	for i, seed := range wantSeeds {
		if order.SeedURLs[i] != seed {
			t.Fatalf("seedURLs[%d] = %q, want %q", i, order.SeedURLs[i], seed)
		}
	}
	if order.Profile.Handle == "" {
		t.Fatal("profile handle is empty")
	}
	if order.Profile.Scope != yacycrawlcontract.ScopeDomain {
		t.Fatalf("scope = %v, want domain", order.Profile.Scope)
	}
	if order.Profile.URLMustMatch != yacycrawlcontract.MatchAll {
		t.Fatalf("urlMustMatch = %q, want MatchAll", order.Profile.URLMustMatch)
	}
}

func TestDispatchRejectsEmptySeeds(t *testing.T) {
	queue := &recordingQueue{}
	rec := post(t, mount(t, queue), `{"name":"x","seeds":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if queue.published {
		t.Fatal("order should not have been published")
	}
}

func TestDispatchRejectsUnknownScope(t *testing.T) {
	rec := post(t, mount(t, &recordingQueue{}), `{"seeds":["x"],"scope":"galaxy"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestDispatchRejectsZeroMaxPagesPerHost(t *testing.T) {
	queue := &recordingQueue{}
	rec := post(t, mount(t, queue), `{"seeds":["x"],"maxPagesPerHost":0}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if queue.published {
		t.Fatal("order should not have been published")
	}
}

func TestDispatchRejectsBadDuration(t *testing.T) {
	rec := post(
		t,
		mount(t, &recordingQueue{}),
		`{"seeds":["x"],"maxPagesPerHost":-1,"crawlDelay":"soon"}`,
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestDispatchRejectsBadJSON(t *testing.T) {
	rec := post(t, mount(t, &recordingQueue{}), `{`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestDispatchRejectsNonPost(t *testing.T) {
	mux := mount(t, &recordingQueue{})
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		crawldispatch.PathCrawlDispatch,
		nil,
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestDispatchReportsPublishFailure(t *testing.T) {
	queue := &recordingQueue{err: errors.New("broker down")}
	rec := post(t, mount(t, queue), `{"seeds":["https://example.org"],"maxPagesPerHost":-1}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}
