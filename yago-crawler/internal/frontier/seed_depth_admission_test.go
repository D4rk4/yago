package frontier_test

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestSeedAdmissionAcceptsBoundaryDepthAndRejectsDeeperRequest(t *testing.T) {
	crawlFrontier := frontier.NewFrontier(2, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeWide,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        1,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	seeded := crawlFrontier.SeedRun(
		context.Background(),
		[]yagocrawlcontract.CrawlRequest{
			{URL: "https://allowed.example/", Depth: 1, ProfileHandle: profile.Profile.Handle},
			{URL: "https://deep.example/", Depth: 2, ProfileHandle: profile.Profile.Handle},
		},
		[]byte("seed-depth-admission"),
		profile,
		nil,
	)
	if seeded.Queued != 1 {
		t.Fatalf("depth-bounded seed queue = %d, want 1", seeded.Queued)
	}
	if job := receiveJob(t, crawlFrontier); job.URL != "https://allowed.example/" {
		t.Fatalf("depth-bounded seed job = %+v", job)
	}
	assertNoJob(t, crawlFrontier, 20*time.Millisecond)
}
