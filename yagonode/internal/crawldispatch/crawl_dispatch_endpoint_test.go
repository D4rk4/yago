package crawldispatch_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
)

type recordingQueue struct {
	order     yagocrawlcontract.CrawlOrder
	published bool
	err       error
	duplicate bool
	keys      []string
}

func (q *recordingQueue) PublishOnce(
	_ context.Context,
	key string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	q.keys = append(q.keys, key)
	if q.err != nil {
		return false, q.err
	}
	if q.duplicate {
		return true, nil
	}
	q.order = order
	q.published = true

	return false, nil
}

const initiator = yagomodel.Hash("abcdefABCDEF")

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

	return postWithKey(t, mux, "", body)
}

func postWithKey(t *testing.T, mux *http.ServeMux, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		crawldispatch.PathCrawlDispatch,
		strings.NewReader(body),
	)
	if key != "" {
		req.Header.Set(crawldispatch.IdempotencyKeyHeader, key)
	}
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
		if r.Mode != yagocrawlcontract.CrawlRequestModeSitemap {
			t.Fatalf("request mode = %q, want sitemap", r.Mode)
		}
		if r.Initiator != initiator {
			t.Fatalf("initiator = %q, want %q", r.Initiator, initiator)
		}
		if r.AppDate.IsZero() {
			t.Fatal("request AppDate is zero")
		}
	}
	if order.Profile.Scope != yagocrawlcontract.ScopeDomain {
		t.Fatalf("scope = %v, want domain", order.Profile.Scope)
	}
	if order.Profile.URLMustMatch != yagocrawlcontract.MatchAll {
		t.Fatalf("urlMustMatch = %q, want MatchAll", order.Profile.URLMustMatch)
	}
	if !order.Profile.FollowNoFollowLinks {
		t.Fatal("followNoFollowLinks should be enabled")
	}
	if order.Profile.MaxPagesPerRun == nil ||
		*order.Profile.MaxPagesPerRun != yagocrawlcontract.DefaultMaxPagesPerRun {
		t.Fatalf("max pages per run = %v", order.Profile.MaxPagesPerRun)
	}
}

func TestDispatchUsesLiveDefaultAndExplicitRunBudget(t *testing.T) {
	queue := &recordingQueue{}
	maximum := 321
	mux := http.NewServeMux()
	crawldispatch.MountCrawlDispatch(
		mux,
		initiator,
		func() []byte { return []byte("token") },
		queue,
		crawldispatch.WithMaxPagesPerRun(func() int { return maximum }),
	)

	rec := post(t, mux, `{"seeds":["https://example.org/"],"maxPagesPerHost":-1}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("default status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if queue.order.Profile.MaxPagesPerRun == nil || *queue.order.Profile.MaxPagesPerRun != 321 {
		t.Fatalf("default max pages per run = %v, want 321", queue.order.Profile.MaxPagesPerRun)
	}

	maximum = 654
	rec = post(t, mux, `{"seeds":["https://example.net/"],"maxPagesPerHost":-1}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("updated default status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if queue.order.Profile.MaxPagesPerRun == nil || *queue.order.Profile.MaxPagesPerRun != 654 {
		t.Fatalf("updated default max pages per run = %v, want 654",
			queue.order.Profile.MaxPagesPerRun)
	}

	rec = post(t, mux, `{"seeds":["https://example.edu/"],"maxPagesPerHost":-1,"maxPagesPerRun":0}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("explicit status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if queue.order.Profile.MaxPagesPerRun == nil || *queue.order.Profile.MaxPagesPerRun != 0 {
		t.Fatalf("explicit max pages per run = %v, want zero", queue.order.Profile.MaxPagesPerRun)
	}
}

func TestDispatcherRejectsInvalidDefaultRunBudgetSource(t *testing.T) {
	dispatcher := crawldispatch.NewDispatcher(
		initiator,
		func() []byte { return []byte("token") },
		&recordingQueue{},
		crawldispatch.WithMaxPagesPerRun(func() int { return -1 }),
	)
	if got := dispatcher.MaxPagesPerRun(); got != yagocrawlcontract.DefaultMaxPagesPerRun {
		t.Fatalf("max pages per run = %d, want %d", got,
			yagocrawlcontract.DefaultMaxPagesPerRun)
	}
}

func TestDispatchRejectsNegativeRunBudget(t *testing.T) {
	queue := &recordingQueue{}
	rec := post(
		t,
		mount(t, queue),
		`{"seeds":["https://example.org/"],"maxPagesPerHost":-1,"maxPagesPerRun":-1}`,
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if queue.published {
		t.Fatal("order with negative run budget was published")
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
	if queue.order.Profile.Scope != yagocrawlcontract.ScopeWide {
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

func TestDispatchAcceptsRobotsStartMode(t *testing.T) {
	queue := &recordingQueue{}
	rec := post(
		t,
		mount(t, queue),
		`{"seeds":["https://example.org/"],"startMode":"robots","maxPagesPerHost":50}`,
	)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if len(queue.order.Requests) != 1 ||
		queue.order.Requests[0].Mode != yagocrawlcontract.CrawlRequestModeRobots {
		t.Fatalf("requests = %#v", queue.order.Requests)
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

func TestDispatchRejectsImpossibleRegex(t *testing.T) {
	queue := &recordingQueue{}
	rec := post(t, mount(t, queue), `{"seeds":["x"],"maxPagesPerHost":-1,"urlMustMatch":"("}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "urlMustMatch is not a valid") {
		t.Fatalf("body = %q, want an impossible-regex error", rec.Body.String())
	}
	if queue.published {
		t.Fatal("order with an impossible regex should not have been published")
	}
}

func TestDispatchCarriesIndexRules(t *testing.T) {
	queue := &recordingQueue{}
	rec := post(t, mount(t, queue), `{
		"seeds": ["https://example.org/"],
		"maxPagesPerHost": -1,
		"indexMustMatch": "https://example.org/articles/.*",
		"indexMustNotMatch": "/draft"
	}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if queue.order.Profile.IndexURLMustMatch != "https://example.org/articles/.*" ||
		queue.order.Profile.IndexURLMustNotMatch != "/draft" {
		t.Fatalf(
			"index rules = %q / %q",
			queue.order.Profile.IndexURLMustMatch,
			queue.order.Profile.IndexURLMustNotMatch,
		)
	}
}

func TestDispatchRejectsImpossibleIndexRegex(t *testing.T) {
	queue := &recordingQueue{}
	rec := post(t, mount(t, queue), `{"seeds":["x"],"maxPagesPerHost":-1,"indexMustMatch":"("}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if queue.published {
		t.Fatal("order with an impossible index regex should not have been published")
	}
}

func TestDispatchRejectsUnboundedDepth(t *testing.T) {
	queue := &recordingQueue{}
	rec := post(t, mount(t, queue), `{"seeds":["x"],"maxPagesPerHost":-1,"maxDepth":1000}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "maxDepth must not exceed") {
		t.Fatalf("body = %q, want an unbounded-depth error", rec.Body.String())
	}
	if queue.published {
		t.Fatal("order with unbounded depth should not have been published")
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

func TestDispatchForwardsIdempotencyKey(t *testing.T) {
	queue := &recordingQueue{}
	rec := postWithKey(
		t,
		mount(t, queue),
		"start-123",
		`{"seeds":["https://example.org/"],"maxPagesPerHost":-1}`,
	)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if len(queue.keys) != 1 || queue.keys[0] != "start-123" {
		t.Fatalf("forwarded keys = %#v, want [start-123]", queue.keys)
	}
}

func TestDispatchReportsDuplicateStart(t *testing.T) {
	queue := &recordingQueue{duplicate: true}
	rec := postWithKey(
		t,
		mount(t, queue),
		"start-123",
		`{"seeds":["https://example.org/"],"maxPagesPerHost":-1}`,
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if queue.published {
		t.Fatal("duplicate start must not publish a new order")
	}
	if !strings.Contains(rec.Body.String(), `"duplicate":true`) {
		t.Fatalf("body = %q, want duplicate:true", rec.Body.String())
	}
}

func TestDispatchRejectsOverlongIdempotencyKey(t *testing.T) {
	queue := &recordingQueue{}
	rec := postWithKey(
		t,
		mount(t, queue),
		strings.Repeat("k", 201),
		`{"seeds":["https://example.org/"],"maxPagesPerHost":-1}`,
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(queue.keys) != 0 {
		t.Fatal("overlong idempotency key must be rejected before publish")
	}
}
