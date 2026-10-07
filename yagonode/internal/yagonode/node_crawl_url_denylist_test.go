package yagonode

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

type crawlURLDenylistProcess struct {
	store *urldenylist.Store
}

func (*crawlURLDenylistProcess) mountDispatch(*http.ServeMux) {}
func (*crawlURLDenylistProcess) Run(context.Context)          {}
func (*crawlURLDenylistProcess) Close()                       {}

func (p *crawlURLDenylistProcess) useCrawlURLDenylist(store *urldenylist.Store) {
	p.store = store
}

func TestAttachCrawlURLDenylistAndLiveSource(t *testing.T) {
	vaultStore := openTestVault(t)
	store, err := urldenylist.Open(vaultStore, time.Now)
	if err != nil {
		t.Fatalf("urldenylist.Open: %v", err)
	}
	if err := store.Add(t.Context(), urldenylist.KindDomain, "blocked.example"); err != nil {
		t.Fatalf("add domain: %v", err)
	}
	process := &crawlURLDenylistProcess{}
	attachCrawlURLDenylist(process, store)
	if process.store != store {
		t.Fatal("URL denylist was not attached")
	}
	attachCrawlURLDenylist(bareCrawlProcess{}, store)
	policy, err := crawlURLDenylistSource(store)()
	if err != nil || len(policy.Domains) != 1 || policy.Domains[0] != "blocked.example" {
		t.Fatalf("crawl URL denylist policy = %+v, %v", policy, err)
	}
	runtime := liveCrawlRuntime(t)
	runtime.useCrawlURLDenylist(store)
}

func TestCrawlURLDenylistSourceCanonicalizesEquivalentExactRules(t *testing.T) {
	vaultStore := openTestVault(t)
	store, err := urldenylist.Open(vaultStore, time.Now)
	if err != nil {
		t.Fatalf("urldenylist.Open: %v", err)
	}
	for _, value := range []string{
		"https://EXAMPLE.test:443/a/../blocked?utm_source=synthetic",
		"https://example.test/blocked?utm_campaign=synthetic",
	} {
		if err := store.Add(t.Context(), urldenylist.KindURL, value); err != nil {
			t.Fatalf("add URL rule: %v", err)
		}
	}

	policy, err := crawlURLDenylistSource(store)()
	if err != nil {
		t.Fatalf("crawl URL denylist source: %v", err)
	}
	if len(policy.ExactURLs) != 1 || policy.ExactURLs[0] != "https://example.test/blocked" {
		t.Fatalf("exact URLs = %#v", policy.ExactURLs)
	}
	entries, err := store.Entries(t.Context())
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("raw durable aliases = %d, want 2", len(entries))
	}
}

func TestCrawlURLDenylistKeepsLiteralExactURLRules(t *testing.T) {
	vaultStore := openTestVault(t)
	store, err := urldenylist.Open(vaultStore, time.Now)
	if err != nil {
		t.Fatalf("urldenylist.Open: %v", err)
	}
	literal := "ftp://files.example/archive"
	if err := store.Add(t.Context(), urldenylist.KindURL, literal); err != nil {
		t.Fatalf("add literal URL rule: %v", err)
	}
	snapshot := store.Snapshot()
	if !snapshot.Blocks(literal) || snapshot.Blocks("ftp://files.example/other") ||
		snapshot.Blocks("https://files.example/archive") {
		t.Fatal("noncanonicalizable exact URL rule did not retain literal-only matching")
	}

	actual, err := crawlURLDenylistSource(store)()
	if err != nil {
		t.Fatalf("crawl URL denylist source: %v", err)
	}
	expected, err := yagocrawlcontract.NewCrawlURLDenylist([]string{literal}, nil)
	if err != nil {
		t.Fatalf("build literal URL policy: %v", err)
	}
	if len(actual.ExactURLs) != 1 || actual.ExactURLs[0] != literal ||
		!bytes.Equal(actual.Revision, expected.Revision) {
		t.Fatal("literal exact URL wire value or revision changed")
	}
}
