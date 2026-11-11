package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/httpfetch"
	"github.com/D4rk4/yago/yago-crawler/internal/robots"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagoegress"
)

func TestRateLimitDefersHostWhileServerFailuresRemainPageScoped(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		status          int
		retryAfter      string
		wantHostBlocked bool
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, retryAfter: "10", wantHostBlocked: true},
		{name: "server error", status: http.StatusServiceUnavailable},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if testCase.retryAfter != "" {
					writer.Header().Set("Retry-After", testCase.retryAfter)
				}
				writer.WriteHeader(testCase.status)
			}))
			defer server.Close()

			workFrontier, err := frontier.Open(filepath.Join(t.TempDir(), "frontier.db"), frontier.Config{
				MaximumPendingPages: 10,
				LeaseDuration:       time.Minute,
				MaximumHostActive:   1,
			})
			if err != nil {
				t.Fatalf("open frontier: %v", err)
			}
			t.Cleanup(func() { _ = workFrontier.Close() })
			fetcher, err := httpfetch.NewPageFetcher(httpfetch.Limits{
				RequestTimeout:      2 * time.Second,
				ConnectTimeout:      time.Second,
				TLSHandshakeTimeout: time.Second,
				HeaderTimeout:       time.Second,
				MaximumResponseSize: 1024,
				MaximumIdleHosts:    1,
				MaximumHostActive:   1,
				UserAgent:           "Yago rate limit test",
			}, yagoegress.NewGuard(true))
			if err != nil {
				t.Fatalf("create page fetcher: %v", err)
			}
			defer fetcher.CloseIdleConnections()
			worker := &Worker{
				frontier:    workFrontier,
				fetcher:     fetcher,
				hosts:       newHostAdmission(0, 1),
				robots:      robots.NewPolicy(nil, "", 0),
				activity:    newCrawlerActivity(time.Now()),
				urlDenylist: newCrawlerURLDenylist(),
				activePages: make(map[string]pageActivity),
			}
			ctx := context.Background()
			first, _, err := workFrontier.Enqueue(ctx, "run-a", server.URL+"/first", "", 0, time.Now())
			if err != nil {
				t.Fatalf("enqueue first page: %v", err)
			}
			if _, _, err := workFrontier.Enqueue(ctx, "run-a", server.URL+"/second", "", 0, time.Now()); err != nil {
				t.Fatalf("enqueue second page: %v", err)
			}
			claimed, found, err := workFrontier.Claim(ctx, "run-a", time.Now())
			if err != nil || !found || claimed.ID != first.ID {
				t.Fatalf("claim first page = %#v, found %v, err %v", claimed, found, err)
			}
			if err := worker.executeClaimedPage(
				ctx,
				yagocrawlcontract.CrawlOrder{
					Profile: yagocrawlcontract.CrawlProfile{IgnoreRobots: true},
				},
				runRules{},
				"run-a",
				"lease-a",
				claimed,
			); err != nil {
				t.Fatalf("execute first page: %v", err)
			}
			second, found, err := workFrontier.Claim(ctx, "run-a", time.Now())
			if err != nil {
				t.Fatalf("claim after response: %v", err)
			}
			if found == testCase.wantHostBlocked {
				t.Fatalf("same-host page found = %v, want host blocked %v", found, testCase.wantHostBlocked)
			}
			if found && second.URL != server.URL+"/second" {
				t.Fatalf("next page after response = %s", second.URL)
			}
		})
	}
}
