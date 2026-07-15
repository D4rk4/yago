package yagonode

import (
	"context"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/eviction"
	"github.com/D4rk4/yago/yagonode/internal/redirectpurge"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type redirectCorpusPurge interface {
	Run(context.Context)
}

func newNodeRedirectPurge(storage nodeStorage, v *vault.Vault) redirectCorpusPurge {
	stored := storage.storedDocuments()
	documents := storage.documentEvictor()
	if v == nil || stored == nil || documents == nil || storage.searchIndex == nil ||
		storage.postingPurger == nil || storage.references == nil ||
		storage.urlEvictor == nil || storage.urlDirectory == nil {
		return nil
	}
	purger := eviction.NewEvictor(
		v,
		storage.postingPurger,
		storage.references,
		storage.urlEvictor,
		documents,
		storage.urlDirectory,
	)

	return redirectpurge.New(stored, purger.PurgeResolved)
}

// runRedirectPurge sweeps pre-SEARCH-28 search-engine tracking redirects out
// of the live corpus once per boot (SEARCH-29).
func runRedirectPurge(ctx context.Context, assembled node) {
	if assembled.redirectPurge != nil {
		assembled.redirectPurge.Run(ctx)
	}
}

// startRedirectPurge runs the sweep under serve's WaitGroup so no purge
// goroutine can touch the vault after serve returns and the vault is closed.
// Joining it never waits out a corpus scan: the sweep aborts between
// documents once serve cancels the context.
func startRedirectPurge(ctx context.Context, background *sync.WaitGroup, assembled node) {
	background.Add(1)
	go func() {
		defer background.Done()
		runRedirectPurge(ctx, assembled)
	}()
}
