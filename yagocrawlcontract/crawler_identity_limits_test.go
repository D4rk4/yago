package yagocrawlcontract

import (
	"strings"
	"testing"
)

func TestCrawlerIdentityLimits(t *testing.T) {
	workerLimit := strings.Repeat("w", MaximumCrawlerWorkerIdentityBytes)
	sessionLimit := strings.Repeat("s", MaximumCrawlerSessionIdentityBytes)
	invalidIdentities := []string{
		"",
		"worker\nidentity",
		"worker\u200bidentity",
		"worker\u2028identity",
		"worker\u2029identity",
		string([]byte{0xff}),
	}
	if !ValidCrawlerWorkerIdentity(workerLimit) || ValidCrawlerWorkerIdentity(workerLimit+"w") {
		t.Fatal("crawler worker identity limit validation failed")
	}
	if !ValidCrawlerSessionIdentity(sessionLimit) || ValidCrawlerSessionIdentity(sessionLimit+"s") {
		t.Fatal("crawler session identity limit validation failed")
	}
	for _, identity := range invalidIdentities {
		if ValidCrawlerWorkerIdentity(identity) {
			t.Errorf("invalid worker identity %q accepted", identity)
		}
		if ValidCrawlerSessionIdentity(identity) {
			t.Errorf("invalid session identity %q accepted", identity)
		}
	}
}

func TestParseCrawlerWorkerIdentityPrefix(t *testing.T) {
	identity, err := ParseCrawlerWorkerIdentityPrefix("  crawler-节点  ")
	if err != nil || identity != "crawler-节点" {
		t.Fatalf("worker identity prefix = %q, err = %v", identity, err)
	}
	if !ValidCrawlerSessionIdentity("session-جلسة") {
		t.Fatal("visible Unicode session identity rejected")
	}
	for _, raw := range []string{
		"",
		"   ",
		"crawler\n7",
		"crawler\u20287",
		strings.Repeat("w", MaximumCrawlerWorkerIdentityPrefixBytes+1),
	} {
		if _, err := ParseCrawlerWorkerIdentityPrefix(raw); err == nil {
			t.Errorf("worker identity prefix %q parsed", raw)
		}
	}
}
