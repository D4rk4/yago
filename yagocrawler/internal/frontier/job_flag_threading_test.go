package frontier_test

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/frontier"
)

// TestSeedRunThreadsLinkPolicyFlagsIntoJobs pins the CRAWL-28/CRAWL-29
// plumbing: the profile's FollowNoFollowLinks and NoindexCanonicalMismatch
// flags ride every dispatched job so the pipeline can apply page-level
// robots and canonical policy without a profile lookup.
func TestSeedRunThreadsLinkPolicyFlagsIntoJobs(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:                    yagocrawlcontract.ScopeDomain,
		URLMustMatch:             yagocrawlcontract.MatchAll,
		FollowNoFollowLinks:      true,
		NoindexCanonicalMismatch: true,
		MaxPagesPerHost:          yagocrawlcontract.UnlimitedPagesPerHost,
	})
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		nil,
		profile,
		func(bool) {},
	)
	job := receiveJob(t, f)
	if !job.FollowNoFollowLinks {
		t.Error("FollowNoFollowLinks did not reach the job")
	}
	if !job.NoindexCanonicalMismatch {
		t.Error("NoindexCanonicalMismatch did not reach the job")
	}
	f.Done(job, false)
}
