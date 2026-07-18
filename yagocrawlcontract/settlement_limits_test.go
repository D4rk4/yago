package yagocrawlcontract

import (
	"strings"
	"testing"
)

func TestCrawlLeaseIdentityLimits(t *testing.T) {
	maximum := strings.Repeat("l", MaximumCrawlLeaseIDBytes)
	if !ValidCrawlLeaseID(maximum) || ValidCrawlLeaseID("") ||
		ValidCrawlLeaseID(maximum+"l") || ValidCrawlLeaseID(string([]byte{0xff})) {
		t.Fatal("crawl lease identity limits are inconsistent")
	}
}
