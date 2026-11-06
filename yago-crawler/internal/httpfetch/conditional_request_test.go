package httpfetch_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/httpfetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	issuedEntityTag    = `"v1"`
	updatedEntityTag   = `"v2"`
	issuedModifiedDate = "Wed, 01 Jul 2026 10:00:00 GMT"
	updatedModifiedDay = "Thu, 02 Jul 2026 10:00:00 GMT"
)

type conditionalHeadersSeen struct {
	ifNoneMatch        string
	ifNoneMatchSent    bool
	ifModifiedSince    string
	ifModifiedSinceSet bool
	path               string
}

type conditionalOrigin struct {
	mu      sync.Mutex
	seen    []conditionalHeadersSeen
	respond func(w http.ResponseWriter, r *http.Request, attempt int)
}

func (o *conditionalOrigin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	_, ifNoneMatchSent := r.Header["If-None-Match"]
	_, ifModifiedSinceSet := r.Header["If-Modified-Since"]
	o.seen = append(o.seen, conditionalHeadersSeen{
		ifNoneMatch:        r.Header.Get("If-None-Match"),
		ifNoneMatchSent:    ifNoneMatchSent,
		ifModifiedSince:    r.Header.Get("If-Modified-Since"),
		ifModifiedSinceSet: ifModifiedSinceSet,
		path:               r.URL.Path,
	})
	attempt := len(o.seen)
	o.mu.Unlock()
	o.respond(w, r, attempt)
}

func (o *conditionalOrigin) requests() []conditionalHeadersSeen {
	o.mu.Lock()
	defer o.mu.Unlock()

	return append([]conditionalHeadersSeen(nil), o.seen...)
}

func serveHTMLPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("ETag", updatedEntityTag)
	_, _ = w.Write([]byte("<html><body>fresh representation</body></html>"))
}

func conditionalFetch(
	t *testing.T,
	ctx context.Context,
	client *http.Client,
	target string,
	validators yagocrawlcontract.HTTPValidators,
) (pagefetch.FetchedPage, error) {
	t.Helper()

	page, err := httpfetch.NewPageFetcher(client, "crawler/1.0", 1<<20).Fetch(
		ctx,
		pagefetch.FetchRequest{Target: mustParse(t, target), Validators: validators},
	)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("conditional fetch: %w", err)
	}

	return page, nil
}

func TestConditionalFetchReportsNotModifiedWithMergedValidators(t *testing.T) {
	origin := &conditionalOrigin{respond: func(w http.ResponseWriter, _ *http.Request, _ int) {
		w.Header().Set("ETag", updatedEntityTag)
		w.WriteHeader(http.StatusNotModified)
	}}
	server := httptest.NewServer(origin)
	defer server.Close()

	_, err := conditionalFetch(t, t.Context(), server.Client(), server.URL+"/page",
		yagocrawlcontract.HTTPValidators{ETag: issuedEntityTag, LastModified: issuedModifiedDate})
	notModified, ok := pagefetch.AsNotModified(err)
	if !ok {
		t.Fatalf("error = %v, want not modified", err)
	}
	want := yagocrawlcontract.HTTPValidators{
		ETag:         updatedEntityTag,
		LastModified: issuedModifiedDate,
	}
	if notModified.Validators != want {
		t.Fatalf("validators = %+v, want %+v", notModified.Validators, want)
	}
	requests := origin.requests()
	if len(requests) != 1 ||
		requests[0].ifNoneMatch != issuedEntityTag ||
		requests[0].ifModifiedSince != issuedModifiedDate {
		t.Fatalf("requests = %+v, want one conditional request with both validators", requests)
	}
}

func TestConditionalFetchSendsOnlyTheValidatorsItHolds(t *testing.T) {
	for _, tc := range []struct {
		name       string
		issued     yagocrawlcontract.HTTPValidators
		update     func(http.Header)
		want       yagocrawlcontract.HTTPValidators
		entityTag  bool
		modifiedAt bool
	}{
		{
			name:      "entity tag only",
			issued:    yagocrawlcontract.HTTPValidators{ETag: issuedEntityTag},
			update:    func(http.Header) {},
			want:      yagocrawlcontract.HTTPValidators{ETag: issuedEntityTag},
			entityTag: true,
		},
		{
			name:       "modification date only",
			issued:     yagocrawlcontract.HTTPValidators{LastModified: issuedModifiedDate},
			update:     func(h http.Header) { h.Set("Last-Modified", updatedModifiedDay) },
			want:       yagocrawlcontract.HTTPValidators{LastModified: updatedModifiedDay},
			modifiedAt: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origin := &conditionalOrigin{
				respond: func(w http.ResponseWriter, _ *http.Request, _ int) {
					tc.update(w.Header())
					w.WriteHeader(http.StatusNotModified)
				},
			}
			server := httptest.NewServer(origin)
			defer server.Close()

			_, err := conditionalFetch(t, t.Context(), server.Client(), server.URL, tc.issued)
			notModified, ok := pagefetch.AsNotModified(err)
			if !ok || notModified.Validators != tc.want {
				t.Fatalf("outcome = %v, want not modified with %+v", err, tc.want)
			}
			requests := origin.requests()
			if len(requests) != 1 ||
				requests[0].ifNoneMatchSent != tc.entityTag ||
				requests[0].ifModifiedSinceSet != tc.modifiedAt {
				t.Fatalf("requests = %+v, want entity tag %v and date %v",
					requests, tc.entityTag, tc.modifiedAt)
			}
		})
	}
}

func TestConditionalFetchRetriesUnusableNotModifiedOnceWithoutValidators(t *testing.T) {
	for _, tc := range []struct {
		name   string
		issued yagocrawlcontract.HTTPValidators
		first  func(http.Header)
	}{
		{
			name:  "unsolicited",
			first: func(http.Header) {},
		},
		{
			name:   "malformed updated entity tag",
			issued: yagocrawlcontract.HTTPValidators{ETag: issuedEntityTag},
			first:  func(h http.Header) { h.Set("ETag", "unquoted") },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origin := &conditionalOrigin{
				respond: func(w http.ResponseWriter, _ *http.Request, attempt int) {
					if attempt == 1 {
						tc.first(w.Header())
						w.WriteHeader(http.StatusNotModified)

						return
					}
					serveHTMLPage(w)
				},
			}
			server := httptest.NewServer(origin)
			defer server.Close()

			page, err := conditionalFetch(t, t.Context(), server.Client(), server.URL, tc.issued)
			if err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			if !strings.Contains(string(page.Body), "fresh representation") ||
				page.Validators.ETag != updatedEntityTag {
				t.Fatalf("page = %q %+v, want the full representation", page.Body, page.Validators)
			}
			requests := origin.requests()
			if len(requests) != 2 || requests[1].ifNoneMatchSent || requests[1].ifModifiedSinceSet {
				t.Fatalf("requests = %+v, want one unconditional retry", requests)
			}
		})
	}
}

func TestConditionalFetchFailsWhenTheRetryAlsoAnswersNotModified(t *testing.T) {
	origin := &conditionalOrigin{respond: func(w http.ResponseWriter, _ *http.Request, _ int) {
		w.WriteHeader(http.StatusNotModified)
	}}
	server := httptest.NewServer(origin)
	defer server.Close()

	page, err := conditionalFetch(
		t,
		t.Context(),
		server.Client(),
		server.URL,
		yagocrawlcontract.HTTPValidators{},
	)
	if err == nil {
		t.Fatalf("Fetch returned bodyless success %+v", page)
	}
	if _, notModified := pagefetch.AsNotModified(err); notModified {
		t.Fatalf("error = %v, want a fetch failure", err)
	}
	if requests := origin.requests(); len(requests) != 2 {
		t.Fatalf("requests = %d, want the original and one retry", len(requests))
	}
}

func TestConditionalFetchTreatsNotModifiedAfterStrippedRedirectAsUnsolicited(t *testing.T) {
	origin := &conditionalOrigin{}
	origin.respond = func(w http.ResponseWriter, r *http.Request, attempt int) {
		switch {
		case r.URL.Path == "/old":
			http.Redirect(w, r, "/new", http.StatusFound)
		case attempt == 2:
			w.WriteHeader(http.StatusNotModified)
		default:
			serveHTMLPage(w)
		}
	}
	server := httptest.NewServer(origin)
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		httpfetch.RemoveConditionalHeaders(next.Header)

		return nil
	}

	page, err := conditionalFetch(t, t.Context(), client, server.URL+"/old",
		yagocrawlcontract.HTTPValidators{ETag: issuedEntityTag, LastModified: issuedModifiedDate})
	if err != nil || page.URL.Path != "/new" {
		t.Fatalf("Fetch = %+v %v, want the redirected full page", page.URL, err)
	}
	for _, request := range origin.requests() {
		if request.path == "/new" && (request.ifNoneMatchSent || request.ifModifiedSinceSet) {
			t.Fatalf("validators leaked to the redirect target: %+v", request)
		}
	}
	if requests := origin.requests(); len(requests) != 4 || !requests[0].ifNoneMatchSent ||
		requests[2].ifNoneMatchSent {
		t.Fatalf("requests = %+v, want conditional first hop and unconditional retry", requests)
	}
}

func TestConditionalFetchRefusesNotModifiedWithoutRequestEvidence(t *testing.T) {
	attempts := 0
	client := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return &http.Response{
					StatusCode: http.StatusNotModified,
					Header:     http.Header{},
					Body:       http.NoBody,
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/html"}},
				Body:       http.NoBody,
				Request:    request,
			}, nil
		}),
	}

	page, err := conditionalFetch(t, t.Context(), client, "https://example.org/",
		yagocrawlcontract.HTTPValidators{ETag: issuedEntityTag})
	if err != nil || page.HTTPStatus != http.StatusOK || attempts != 2 {
		t.Fatalf(
			"Fetch = %d %v after %d attempts, want an unconditional full fetch",
			page.HTTPStatus,
			err,
			attempts,
		)
	}
}

func TestConditionalFetchRetryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	origin := &conditionalOrigin{respond: func(w http.ResponseWriter, _ *http.Request, _ int) {
		cancel()
		w.WriteHeader(http.StatusNotModified)
	}}
	server := httptest.NewServer(origin)
	defer server.Close()

	_, err := conditionalFetch(
		t,
		ctx,
		server.Client(),
		server.URL,
		yagocrawlcontract.HTTPValidators{},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
}

func TestConditionalFetchRetryStaysWithinTheOriginalDeadline(t *testing.T) {
	const pause = 400 * time.Millisecond
	origin := &conditionalOrigin{
		respond: func(w http.ResponseWriter, r *http.Request, attempt int) {
			select {
			case <-time.After(pause):
			case <-r.Context().Done():
				return
			}
			if attempt == 1 {
				w.WriteHeader(http.StatusNotModified)

				return
			}
			serveHTMLPage(w)
		},
	}
	server := httptest.NewServer(origin)
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * pause / 2

	page, err := conditionalFetch(
		t,
		t.Context(),
		client,
		server.URL,
		yagocrawlcontract.HTTPValidators{},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Fetch = %+v %v, want the original deadline to expire", page.URL, err)
	}
	if requests := origin.requests(); len(requests) != 2 {
		t.Fatalf("requests = %d, want the original and the retry", len(requests))
	}
}
