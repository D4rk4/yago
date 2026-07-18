package frontier_test

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type toggleGrowthAdmission struct {
	allowed bool
	calls   int
}

func (admission *toggleGrowthAdmission) WaitForGrowth(context.Context) bool {
	admission.calls++

	return admission.allowed
}

func TestStoragePressureStopsTakeBeforeClaimAndPreservesWork(t *testing.T) {
	admission := &toggleGrowthAdmission{}
	crawlFrontier := frontier.NewFrontier(
		1,
		nil,
		frontier.WithGrowthAdmission(admission),
	)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	crawlFrontier.SeedRun(
		t.Context(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		[]byte("storage-pressure"),
		profile,
		nil,
	)
	if job, ok := crawlFrontier.Take(t.Context()); ok {
		t.Fatalf("storage pressure dispatched %q", job.URL)
	}
	admission.allowed = true
	job, ok := crawlFrontier.Take(t.Context())
	if !ok || job.URL != "https://example.com/" || admission.calls != 2 {
		t.Fatalf("resumed take = %+v/%t calls=%d", job, ok, admission.calls)
	}
}
