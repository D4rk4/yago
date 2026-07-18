package main

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCrawlerSessionIdentityIsProcessScoped(t *testing.T) {
	first := newCrawlerSessionID("stable-worker")
	second := newCrawlerSessionID("stable-worker")
	if !yagocrawlcontract.ValidCrawlerSessionIdentity(first) ||
		!yagocrawlcontract.ValidCrawlerSessionIdentity(second) ||
		first == "stable-worker" || first == second {
		t.Fatalf("session identities = %q and %q", first, second)
	}
}
