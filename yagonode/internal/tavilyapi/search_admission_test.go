package tavilyapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestSearchRequestIntakeShedsBeforeBodyAndSearchAdmission(t *testing.T) {
	intake := newRequestAdmission(1)
	release, admitted := intake.tryEnter()
	if !admitted {
		t.Fatal("failed to reserve intake fixture")
	}
	body := &unreadJSONBody{}
	var admissionCalls atomic.Int64
	endpoint := searchEndpoint{
		search: &fakeSearcher{},
		access: SearchAccessPolicy{BearerToken: searchTestKey},
		admission: func(*http.Request) (func(), int, time.Duration) {
			admissionCalls.Add(1)

			return func() {}, 0, 0
		},
		intake: intake,
		now:    time.Now,
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, PathSearch, nil)
	request.Body = body
	request.Header.Set("Authorization", "Bearer "+searchTestKey)
	result := httptest.NewRecorder()
	endpoint.ServeHTTP(result, request)
	if result.Code != http.StatusServiceUnavailable || result.Header().Get("Retry-After") != "1" ||
		body.read || admissionCalls.Load() != 0 {
		t.Fatalf(
			"result=%d retry=%q body=%t admissions=%d",
			result.Code,
			result.Header().Get("Retry-After"),
			body.read,
			admissionCalls.Load(),
		)
	}
	release()

	invalid := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader("{"),
	)
	invalid.Header.Set("Authorization", "Bearer "+searchTestKey)
	endpoint.ServeHTTP(httptest.NewRecorder(), invalid)
	finalRelease, admitted := intake.tryEnter()
	if !admitted {
		t.Fatal("invalid JSON retained intake slot")
	}
	finalRelease()
}

func TestSearchAdmissionFollowsBodyDecode(t *testing.T) {
	reader, writer := io.Pipe()
	search := &fakeSearcher{}
	var admissionCalls atomic.Int64
	var releases atomic.Int64
	endpoint := newSearchEndpoint(
		search,
		nil,
		SearchAccessPolicy{BearerToken: searchTestKey},
		func(*http.Request) (func(), int, time.Duration) {
			admissionCalls.Add(1)

			return func() { releases.Add(1) }, 0, 0
		},
	)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, PathSearch, reader)
	req.Header.Set("Authorization", "Bearer "+searchTestKey)
	result := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		endpoint.ServeHTTP(result, req)
		close(done)
	}()
	if _, err := writer.Write([]byte(`{"query":"bounded"}`)); err != nil {
		t.Fatalf("write request: %v", err)
	}
	if got := admissionCalls.Load(); got != 0 {
		t.Fatalf("admission calls before request EOF = %d", got)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close request: %v", err)
	}
	<-done
	if result.Code != http.StatusOK || admissionCalls.Load() != 1 || releases.Load() != 1 {
		t.Fatalf(
			"result = %d admissions = %d releases = %d",
			result.Code,
			admissionCalls.Load(),
			releases.Load(),
		)
	}
}

func TestSearchAdmissionFollowsScopeAuthorization(t *testing.T) {
	authorizer := &stubScopeAuthorizer{decision: DecisionAllow}
	admissionCalls := 0
	endpoint := newSearchEndpoint(
		&fakeSearcher{},
		nil,
		SearchAccessPolicy{Authorizer: authorizer},
		func(*http.Request) (func(), int, time.Duration) {
			admissionCalls++

			return func() {}, 0, 0
		},
	)
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"bounded"}`),
	)
	req.Header.Set("Authorization", "Bearer id.secret")
	result := httptest.NewRecorder()
	endpoint.ServeHTTP(result, req)
	if result.Code != http.StatusOK || authorizer.calls != 1 || admissionCalls != 1 {
		t.Fatalf(
			"result = %d authorizations = %d admissions = %d",
			result.Code,
			authorizer.calls,
			admissionCalls,
		)
	}
}

func TestSearchAdmissionRejectsBeforeSearch(t *testing.T) {
	for _, test := range []struct {
		status     int
		retryAfter time.Duration
		wantHeader string
	}{
		{http.StatusTooManyRequests, 3 * time.Second, "3"},
		{http.StatusServiceUnavailable, time.Second, "1"},
	} {
		search := &fakeSearcher{}
		endpoint := newSearchEndpoint(
			search,
			nil,
			SearchAccessPolicy{BearerToken: searchTestKey},
			func(*http.Request) (func(), int, time.Duration) {
				return nil, test.status, test.retryAfter
			},
		)
		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			PathSearch,
			strings.NewReader(`{"query":"bounded"}`),
		)
		req.Header.Set("Authorization", "Bearer "+searchTestKey)
		result := httptest.NewRecorder()
		endpoint.ServeHTTP(result, req)
		if result.Code != test.status || result.Header().Get("Retry-After") != test.wantHeader {
			t.Fatalf("result = %d retry = %q", result.Code, result.Header().Get("Retry-After"))
		}
		if search.calls != 0 {
			t.Fatalf("search calls = %d", search.calls)
		}
	}
}

type canceledAdmissionSearcher struct {
	started chan struct{}
}

func (s canceledAdmissionSearcher) Search(
	ctx context.Context,
	_ searchcore.Request,
) (searchcore.Response, error) {
	close(s.started)
	<-ctx.Done()

	return searchcore.Response{}, fmt.Errorf("canceled search: %w", ctx.Err())
}

func TestSearchAdmissionReleasesCanceledSearch(t *testing.T) {
	started := make(chan struct{})
	var releases atomic.Int64
	endpoint := newSearchEndpoint(
		canceledAdmissionSearcher{started: started},
		nil,
		SearchAccessPolicy{BearerToken: searchTestKey},
		func(*http.Request) (func(), int, time.Duration) {
			return func() { releases.Add(1) }, 0, 0
		},
	)
	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"bounded"}`),
	)
	req.Header.Set("Authorization", "Bearer "+searchTestKey)
	done := make(chan struct{})
	go func() {
		endpoint.ServeHTTP(httptest.NewRecorder(), req)
		close(done)
	}()
	<-started
	cancel()
	<-done
	if got := releases.Load(); got != 1 {
		t.Fatalf("releases = %d", got)
	}
}
