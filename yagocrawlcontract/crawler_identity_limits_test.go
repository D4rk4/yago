package yagocrawlcontract

import (
	"strings"
	"testing"
)

func TestCrawlerIdentityLimits(t *testing.T) {
	workerLimit := strings.Repeat("w", MaximumCrawlerWorkerIdentityBytes)
	sessionLimit := strings.Repeat("s", MaximumCrawlerSessionIdentityBytes)
	if !ValidCrawlerWorkerIdentity(workerLimit) ||
		ValidCrawlerWorkerIdentity("") ||
		ValidCrawlerWorkerIdentity(workerLimit+"w") ||
		ValidCrawlerWorkerIdentity(string([]byte{0xff})) {
		t.Fatal("crawler worker identity limit validation failed")
	}
	if !ValidCrawlerSessionIdentity(sessionLimit) ||
		ValidCrawlerSessionIdentity("") ||
		ValidCrawlerSessionIdentity(sessionLimit+"s") ||
		ValidCrawlerSessionIdentity(string([]byte{0xff})) {
		t.Fatal("crawler session identity limit validation failed")
	}
}
