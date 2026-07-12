package tavilyapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func reserveRawContentCapacity(t *testing.T) func() {
	t.Helper()
	releases := make([]func(), 0, maximumConcurrentRawContentWork)
	for range maximumConcurrentRawContentWork {
		release, admitted := rawContentWorkAdmission.tryEnter()
		if !admitted {
			t.Fatalf("raw admission stopped at %d", len(releases))
		}
		releases = append(releases, release)
	}

	return func() {
		for _, release := range releases {
			release()
		}
	}
}

type countingPageFetcher struct {
	calls atomic.Int64
}

func postRawSearch(
	t *testing.T,
	handler http.Handler,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, PathSearch, strings.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer "+searchTestKey)
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)

	return result
}

func (f *countingPageFetcher) FetchPage(context.Context, string) (CrawledPage, error) {
	f.calls.Add(1)

	return CrawledPage{}, nil
}

func TestRawContentAdmissionShedsExtractCrawlAndMapBeforeBody(t *testing.T) {
	release := reserveRawContentCapacity(t)
	defer release()

	documents := &fakeDocuments{}
	extract := NewExtractEndpointWithAccess(
		documents,
		SearchAccessPolicy{BearerToken: extractTestKey},
	)
	fetcher := &countingPageFetcher{}
	mux := http.NewServeMux()
	MountCrawl(mux, SearchAccessPolicy{BearerToken: crawlTestKey}, fetcher)
	for _, test := range []struct {
		name    string
		path    string
		key     string
		handler http.Handler
	}{
		{"extract", PathExtract, extractTestKey, extract},
		{"crawl", PathCrawl, crawlTestKey, mux},
		{"map", PathMap, crawlTestKey, mux},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &unreadJSONBody{}
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, test.path, nil)
			request.Body = body
			request.Header.Set("Authorization", "Bearer "+test.key)
			result := httptest.NewRecorder()
			test.handler.ServeHTTP(result, request)
			if result.Code != http.StatusServiceUnavailable ||
				result.Header().Get("Retry-After") != "1" || body.read {
				t.Fatalf(
					"result=%d retry=%q read=%t",
					result.Code,
					result.Header().Get("Retry-After"),
					body.read,
				)
			}
		})
	}
	if documents.got != "" || fetcher.calls.Load() != 0 {
		t.Fatalf("lookup=%q fetches=%d", documents.got, fetcher.calls.Load())
	}
}

func TestRawContentAdmissionFollowsAuthentication(t *testing.T) {
	release := reserveRawContentCapacity(t)
	defer release()
	body := &unreadJSONBody{}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, PathExtract, nil)
	request.Body = body
	result := httptest.NewRecorder()
	NewExtractEndpointWithAccess(
		&fakeDocuments{},
		SearchAccessPolicy{BearerToken: extractTestKey},
	).ServeHTTP(result, request)
	if result.Code != http.StatusUnauthorized || body.read {
		t.Fatalf("result=%d read=%t", result.Code, body.read)
	}
}

func TestRawSearchAdmissionDoesNotBlockOrdinarySearch(t *testing.T) {
	release := reserveRawContentCapacity(t)
	defer release()
	search := &fakeSearcher{}
	handler := newSearchEndpoint(
		search,
		nil,
		SearchAccessPolicy{BearerToken: searchTestKey},
		nil,
	)
	raw := postRawSearch(
		t,
		handler,
		`{"query":"bounded","include_raw_content":true}`,
	)
	if raw.Code != http.StatusServiceUnavailable || search.calls != 0 {
		t.Fatalf("raw result=%d calls=%d", raw.Code, search.calls)
	}
	ordinary := postRawSearch(t, handler, `{"query":"bounded"}`)
	if ordinary.Code != http.StatusOK || search.calls != 1 {
		t.Fatalf("ordinary result=%d calls=%d", ordinary.Code, search.calls)
	}
}

func TestRawSearchAdmissionReleasesWhenSearchAdmissionRejects(t *testing.T) {
	endpoint := newSearchEndpoint(
		&fakeSearcher{},
		nil,
		SearchAccessPolicy{BearerToken: searchTestKey},
		func(*http.Request) (func(), int, time.Duration) {
			return nil, http.StatusServiceUnavailable, time.Second
		},
	)
	result := postRawSearch(
		t,
		endpoint,
		`{"query":"bounded","include_raw_content":true}`,
	)
	if result.Code != http.StatusServiceUnavailable {
		t.Fatalf("result=%d body=%s", result.Code, result.Body.String())
	}
	release := reserveRawContentCapacity(t)
	release()
}

type deadlineContentFetcher struct {
	finished chan error
}

type deadlineRequestBody struct {
	closed chan struct{}
	once   sync.Once
}

func (b *deadlineRequestBody) Read([]byte) (int, error) {
	<-b.closed

	return 0, errors.New("request body closed")
}

func (b *deadlineRequestBody) Close() error {
	b.once.Do(func() { close(b.closed) })

	return nil
}

func TestExtractWorkDeadlineClosesSlowRequestBody(t *testing.T) {
	body := &deadlineRequestBody{closed: make(chan struct{})}
	endpoint := extractEndpoint{
		access:       SearchAccessPolicy{BearerToken: extractTestKey},
		now:          time.Now,
		workDuration: time.Millisecond,
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, PathExtract, nil)
	request.Body = body
	request.Header.Set("Authorization", "Bearer "+extractTestKey)
	result := httptest.NewRecorder()
	endpoint.ServeHTTP(result, request)
	if result.Code != http.StatusBadRequest {
		t.Fatalf("result=%d body=%s", result.Code, result.Body.String())
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("deadline did not close request body")
	}
}

func (f deadlineContentFetcher) Fetch(ctx context.Context, _ string) (FetchedContent, error) {
	<-ctx.Done()
	f.finished <- ctx.Err()

	return FetchedContent{}, fmt.Errorf("extract deadline: %w", ctx.Err())
}

func TestExtractWorkDeadlineCancelsSlowFetch(t *testing.T) {
	finished := make(chan error, 1)
	endpoint := extractEndpoint{
		access:       SearchAccessPolicy{BearerToken: extractTestKey},
		fetcher:      deadlineContentFetcher{finished: finished},
		now:          time.Now,
		workDuration: time.Millisecond,
	}
	result := postExtract(
		t,
		endpoint,
		`{"urls":"https://slow.example/"}`,
		extractTestKey,
	)
	if result.Code != http.StatusOK || !errors.Is(<-finished, context.DeadlineExceeded) {
		t.Fatalf("result=%d body=%s", result.Code, result.Body.String())
	}
	response := decodeExtract(t, result)
	if len(response.FailedResults) != 1 {
		t.Fatalf("failures=%d", len(response.FailedResults))
	}
}

type deadlinePageFetcher struct {
	finished chan error
}

func (f deadlinePageFetcher) FetchPage(ctx context.Context, _ string) (CrawledPage, error) {
	<-ctx.Done()
	f.finished <- ctx.Err()

	return CrawledPage{}, fmt.Errorf("crawl deadline: %w", ctx.Err())
}

func TestCrawlWorkDeadlineCancelsSlowFetch(t *testing.T) {
	finished := make(chan error, 1)
	endpoint := crawlEndpoint{
		access:       SearchAccessPolicy{BearerToken: crawlTestKey},
		fetcher:      deadlinePageFetcher{finished: finished},
		now:          time.Now,
		workDuration: time.Millisecond,
	}
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathCrawl,
		strings.NewReader(`{"url":"https://slow.example/"}`),
	)
	request.Header.Set("Authorization", "Bearer "+crawlTestKey)
	result := httptest.NewRecorder()
	endpoint.ServeHTTP(result, request)
	if result.Code != http.StatusOK || !errors.Is(<-finished, context.DeadlineExceeded) {
		t.Fatalf("result=%d body=%s", result.Code, result.Body.String())
	}
}

type deadlineSearcher struct {
	finished chan error
}

func (s deadlineSearcher) Search(
	ctx context.Context,
	_ searchcore.Request,
) (searchcore.Response, error) {
	<-ctx.Done()
	s.finished <- ctx.Err()

	return searchcore.Response{}, fmt.Errorf("search deadline: %w", ctx.Err())
}

func TestRawSearchWorkDeadlineCancelsSlowSearch(t *testing.T) {
	finished := make(chan error, 1)
	endpoint := searchEndpoint{
		search:          deadlineSearcher{finished: finished},
		access:          SearchAccessPolicy{BearerToken: searchTestKey},
		intake:          nil,
		now:             time.Now,
		rawWorkDuration: time.Millisecond,
	}
	result := postRawSearch(
		t,
		endpoint,
		`{"query":"slow","include_raw_content":true}`,
	)
	if result.Code != http.StatusInternalServerError ||
		!errors.Is(<-finished, context.DeadlineExceeded) {
		t.Fatalf("result=%d body=%s", result.Code, result.Body.String())
	}
}
