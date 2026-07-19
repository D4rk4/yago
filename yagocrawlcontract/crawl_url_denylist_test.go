package yagocrawlcontract

import (
	"bytes"
	"strings"
	"testing"
)

func TestCrawlURLDenylistCanonicalRevision(t *testing.T) {
	first, err := NewCrawlURLDenylist(
		[]string{" https://b.example/page ", "https://a.example/page"},
		[]string{"Sub.Example.", "blocked.example"},
	)
	if err != nil {
		t.Fatalf("build first denylist: %v", err)
	}
	second, err := NewCrawlURLDenylist(
		[]string{"https://a.example/page", "https://b.example/page"},
		[]string{"blocked.example", "sub.example"},
	)
	if err != nil {
		t.Fatalf("build second denylist: %v", err)
	}
	if !bytes.Equal(first.Revision, second.Revision) ||
		!bytes.Equal(first.Revision, crawlURLDenylistRevision(first)) {
		t.Fatalf("revisions differ: %x / %x", first.Revision, second.Revision)
	}
	if len(first.Revision) != CrawlURLDenylistRevisionBytes ||
		first.ExactURLs[0] != "https://a.example/page" ||
		first.Domains[1] != "sub.example" {
		t.Fatalf("canonical denylist = %+v", first)
	}
}

func TestParseCrawlURLDenylistRejectsChangedPayload(t *testing.T) {
	denylist, err := NewCrawlURLDenylist(nil, []string{"blocked.example"})
	if err != nil {
		t.Fatalf("build denylist: %v", err)
	}
	if _, err := ParseCrawlURLDenylist(
		denylist.Revision,
		nil,
		[]string{"other.example"},
	); err == nil {
		t.Fatal("changed payload accepted with stale revision")
	}
	if _, err := ParseCrawlURLDenylist([]byte("short"), nil, nil); err == nil {
		t.Fatal("short revision accepted")
	}
	parsed, err := ParseCrawlURLDenylist(
		denylist.Revision,
		denylist.ExactURLs,
		denylist.Domains,
	)
	if err != nil || !bytes.Equal(parsed.Revision, denylist.Revision) {
		t.Fatalf("valid policy parse = %+v, %v", parsed, err)
	}
	if _, err := ParseCrawlURLDenylist(
		make([]byte, CrawlURLDenylistRevisionBytes),
		[]string{""},
		nil,
	); err == nil {
		t.Fatal("invalid payload accepted")
	}
}

func TestCrawlURLDenylistRejectsProtocolBounds(t *testing.T) {
	overflow := make([]string, MaximumCrawlURLDenylistEntries+1)
	for index := range overflow {
		overflow[index] = "https://example.com/" + strings.Repeat("a", index%7)
	}
	if _, err := NewCrawlURLDenylist(overflow, nil); err == nil {
		t.Fatal("entry overflow accepted")
	}
	if _, err := NewCrawlURLDenylist(
		[]string{strings.Repeat("u", MaximumCrawlURLBytes+1)},
		nil,
	); err == nil {
		t.Fatal("oversized URL accepted")
	}
	if _, err := NewCrawlURLDenylist(
		nil,
		[]string{strings.Repeat("d", MaximumCrawlURLDenylistDomainBytes+1)},
	); err == nil {
		t.Fatal("oversized domain accepted")
	}
	entry := strings.Repeat("u", MaximumCrawlURLBytes)
	byteOverflow := make(
		[]string,
		MaximumCrawlURLDenylistBytes/(len(entry)+5)+1,
	)
	for index := range byteOverflow {
		byteOverflow[index] = entry
	}
	if _, err := NewCrawlURLDenylist(byteOverflow, nil); err == nil {
		t.Fatal("encoded byte overflow accepted")
	}
}
