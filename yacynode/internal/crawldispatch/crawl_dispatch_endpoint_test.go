package crawldispatch_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/crawldispatch"
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

const initiator = yacymodel.Hash("abcdefABCDEF")

func mount(t *testing.T, queue crawldispatch.CrawlOrderQueue) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	crawldispatch.MountCrawlDispatch(
		mux,
		initiator,
		func() []byte { return []byte("token") },
		queue,
	)
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
		"startMode": "sitemap",
		"scope": "domain",
		"maxDepth": 3,
		"followNoFollowLinks": true,
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
	if string(order.Provenance) != "token" {
		t.Fatalf("provenance = %q, want token", order.Provenance)
	}
	if len(order.Requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(order.Requests))
	}
	if order.Profile.Handle == "" {
		t.Fatal("profile handle is empty")
	}
	for _, r := range order.Requests {
		if r.ProfileHandle != order.Profile.Handle {
			t.Fatalf("request handle = %q, want %q", r.ProfileHandle, order.Profile.Handle)
		}
		if r.Mode != yacycrawlcontract.CrawlRequestModeSitemap {
			t.Fatalf("request mode = %q, want sitemap", r.Mode)
		}
		if r.Initiator != initiator {
			t.Fatalf("initiator = %q, want %q", r.Initiator, initiator)
		}
		if r.AppDate.IsZero() {
			t.Fatal("request AppDate is zero")
		}
	}
	if order.Profile.Scope != yacycrawlcontract.ScopeDomain {
		t.Fatalf("scope = %v, want domain", order.Profile.Scope)
	}
	if order.Profile.URLMustMatch != yacycrawlcontract.MatchAll {
		t.Fatalf("urlMustMatch = %q, want MatchAll", order.Profile.URLMustMatch)
	}
	if !order.Profile.FollowNoFollowLinks {
		t.Fatal("followNoFollowLinks should be enabled")
	}
}

func TestDispatchBuildsOrderWithExplicitMatchAndRecrawl(t *testing.T) {
	queue := &recordingQueue{}
	rec := post(t, mount(t, queue), `{
		"name": "docs",
		"seeds": ["https://example.org/a"],
		"scope": "wide",
		"urlMustMatch": "https://example.org/.*",
		"maxPagesPerHost": -1,
		"recrawlIfOlder": "24h"
	}`)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if queue.order.Profile.Scope != yacycrawlcontract.ScopeWide {
		t.Fatalf("scope = %v, want wide", queue.order.Profile.Scope)
	}
	if queue.order.Profile.URLMustMatch != "https://example.org/.*" {
		t.Fatalf("urlMustMatch = %q", queue.order.Profile.URLMustMatch)
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

func TestDispatchRejectsUnknownStartMode(t *testing.T) {
	rec := post(t, mount(t, &recordingQueue{}), `{"seeds":["x"],"startMode":"archive"}`)
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

func TestDispatchRejectsBadRecrawlDuration(t *testing.T) {
	rec := post(
		t,
		mount(t, &recordingQueue{}),
		`{"seeds":["x"],"maxPagesPerHost":-1,"recrawlIfOlder":"eventually"}`,
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
