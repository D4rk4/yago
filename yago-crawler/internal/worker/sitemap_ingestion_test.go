package worker

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestEnqueueSitemapRespectsMaximumDepth(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		maximumDepth int
		childDepth   int
		wantQueued   uint64
	}{
		{name: "child_at_depth_limit", maximumDepth: 1, childDepth: 1, wantQueued: 1},
		{name: "child_over_depth_limit", maximumDepth: 1, childDepth: 2},
		{name: "higher_depth_limit_admits_child", maximumDepth: 2, childDepth: 2, wantQueued: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			workFrontier, err := frontier.Open(filepath.Join(t.TempDir(), "frontier.db"), frontier.Config{
				MaximumPendingPages: 10,
				LeaseDuration:       time.Minute,
				HostDelay:           time.Second,
				MaximumHostActive:   1,
			})
			if err != nil {
				t.Fatalf("open frontier: %v", err)
			}
			t.Cleanup(func() { _ = workFrontier.Close() })

			crawler := &Worker{
				frontier:    workFrontier,
				policy:      yagocrawlcontract.CrawlerRuntimePolicy{SitemapURLLimit: 10},
				urlDenylist: newCrawlerURLDenylist(),
			}
			rules := runRules{
				profile: yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
					Name:            "sitemap-depth",
					Scope:           yagocrawlcontract.ScopeDomain,
					MaxDepth:        testCase.maximumDepth,
					MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
					AllowQueryURLs:  true,
				}),
				maximumDepth: testCase.maximumDepth,
				roots:        &scopeRoots{},
			}
			rules.roots.add("https://example.org/")
			if err := crawler.enqueueSitemap(
				context.Background(),
				[]byte("<urlset><url><loc>https://example.org/page</loc></url></urlset>"),
				rules,
				"run",
				testCase.childDepth,
			); err != nil {
				t.Fatalf("enqueue sitemap: %v", err)
			}
			depth, err := workFrontier.DepthForRun(context.Background(), "run")
			if err != nil {
				t.Fatalf("read frontier depth: %v", err)
			}
			if depth.Queued != testCase.wantQueued {
				t.Fatalf("queued pages = %d, want %d", depth.Queued, testCase.wantQueued)
			}
		})
	}
}
