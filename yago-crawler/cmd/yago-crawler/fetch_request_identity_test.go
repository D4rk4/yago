package main

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/botwall"
	"github.com/D4rk4/yago/yago-crawler/internal/crawldenylist"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/publicweb"
	"github.com/D4rk4/yago/yago-crawler/internal/robots"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagoegress"
)

type fetchRequestMarker struct{}

type fetchRequestRecorder struct {
	requests []pagefetch.FetchRequest
	markers  []any
}

func (r *fetchRequestRecorder) Fetch(
	ctx context.Context,
	request pagefetch.FetchRequest,
) (pagefetch.FetchedPage, error) {
	r.requests = append(r.requests, request)
	r.markers = append(r.markers, ctx.Value(fetchRequestMarker{}))

	return pagefetch.FetchedPage{}, &pagefetch.NotModifiedError{Validators: request.Validators}
}

type privateAddressResolver struct{}

func (privateAddressResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
}

func TestPageSourceWrappersPreserveFetchRequestIdentity(t *testing.T) {
	readyDenylist := crawldenylist.New()
	policy, err := yagocrawlcontract.NewCrawlURLDenylist(nil, []string{"denied.example"})
	if err != nil {
		t.Fatalf("denylist policy: %v", err)
	}
	if err := readyDenylist.Apply(policy); err != nil {
		t.Fatalf("apply denylist: %v", err)
	}
	robotsClient := &http.Client{Transport: crawlerRobotsRoundTrip(
		func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       http.NoBody,
				Request:    request,
			}, nil
		},
	)}
	for _, tc := range []struct {
		name string
		wrap func(*testing.T, pagefetch.PageSource) pagefetch.PageSource
	}{
		{"bot wall screening", func(_ *testing.T, inner pagefetch.PageSource) pagefetch.PageSource {
			return botwall.NewBotWallScreeningFetcher(inner)
		}},
		{"crawl URL denylist", func(_ *testing.T, inner pagefetch.PageSource) pagefetch.PageSource {
			return crawldenylist.NewAdmissionFetcher(inner, readyDenylist)
		}},
		{"public web admission", func(_ *testing.T, inner pagefetch.PageSource) pagefetch.PageSource {
			return publicweb.NewAdmissionFetcher(inner, privateAddressResolver{}, yagoegress.NewGuard(true))
		}},
		{"robots admission", func(t *testing.T, inner pagefetch.PageSource) pagefetch.PageSource {
			fetcher, err := robots.NewRobotsAdmissionFetcher(inner, robotsClient, "crawler/1.0", 8)
			if err != nil {
				t.Fatalf("robots admission: %v", err)
			}

			return fetcher
		}},
		{"browser fallback", func(_ *testing.T, inner pagefetch.PageSource) pagefetch.PageSource {
			return pagefetch.NewBrowserFallbackPageSource(inner, &fetchRequestRecorder{}, browserRenderNeeded)
		}},
		{"duplicate fetch suppression", func(_ *testing.T, inner pagefetch.PageSource) pagefetch.PageSource {
			return pagefetch.NewDuplicatePageFetchSuppressor(inner)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &fetchRequestRecorder{}
			target := &url.URL{Scheme: "https", Host: "example.org", Path: "/article"}
			request := pagefetch.FetchRequest{
				Target: target,
				Validators: yagocrawlcontract.HTTPValidators{
					ETag:         `"v1"`,
					LastModified: "Wed, 01 Jul 2026 10:00:00 GMT",
				},
			}
			ctx := context.WithValue(
				pagefetch.WithoutBrowserFallback(t.Context()),
				fetchRequestMarker{},
				"browser options",
			)

			_, err := tc.wrap(t, recorder).Fetch(ctx, request)
			notModified, ok := pagefetch.AsNotModified(err)
			if !ok || notModified.Validators != request.Validators {
				t.Fatalf("error = %v, want the inner not-modified outcome", err)
			}
			if len(recorder.requests) != 1 ||
				recorder.requests[0].Target.String() != target.String() ||
				recorder.requests[0].Validators != request.Validators ||
				recorder.markers[0] != "browser options" {
				t.Fatalf(
					"inner saw %+v with %v, want the original request and context",
					recorder.requests,
					recorder.markers,
				)
			}
		})
	}
}
