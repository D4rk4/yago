package yagonode

import (
	"context"
	"net/http"
	"testing"
	"time"

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
