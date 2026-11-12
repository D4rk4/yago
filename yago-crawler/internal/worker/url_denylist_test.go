package worker

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestCrawlerURLDenylistBlocksExactURLsAndDomains(t *testing.T) {
	list := newCrawlerURLDenylist()
	policy, err := yagocrawlcontract.NewCrawlURLDenylist(
		[]string{"https://example.test/private"},
		[]string{"blocked.test"},
	)
	if err != nil {
		t.Fatalf("NewCrawlURLDenylist: %v", err)
	}
	message := &crawlrpc.CrawlURLDenylist{
		Revision: policy.Revision, ExactUrls: policy.ExactURLs, Domains: policy.Domains,
	}
	if err := list.Replace(message); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	for _, rawURL := range []string{
		"https://example.test/private",
		"https://sub.blocked.test/page",
		"https://blocked.test/",
	} {
		if !list.Blocks(rawURL) {
			t.Errorf("Blocks(%q) = false, want true", rawURL)
		}
	}
	for _, rawURL := range []string{
		"https://example.test/public",
		"https://notblocked.test/page",
	} {
		if list.Blocks(rawURL) {
			t.Errorf("Blocks(%q) = true, want false", rawURL)
		}
	}
}

func TestCrawlerURLDenylistRejectsChangedPayloadAndInvalidURLs(t *testing.T) {
	list := newCrawlerURLDenylist()
	policy, err := yagocrawlcontract.NewCrawlURLDenylist(nil, []string{"blocked.test"})
	if err != nil {
		t.Fatalf("NewCrawlURLDenylist: %v", err)
	}
	changed := &crawlrpc.CrawlURLDenylist{
		Revision: policy.Revision, Domains: []string{"allowed.test"},
	}
	if err := list.Replace(changed); err == nil {
		t.Fatal("denylist payload with a mismatched revision was accepted")
	}
	if !list.Blocks("file:///etc/passwd") {
		t.Fatal("invalid URL was admitted by the denylist")
	}
	if list.Blocks("https://allowed.test/") {
		t.Fatal("rejected denylist update changed the prior empty policy")
	}
}
