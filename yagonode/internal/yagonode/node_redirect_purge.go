package yagonode

import (
	"context"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/redirectpurge"
)

// documentEvictorOf exposes the vault's delete capability when the directory
// implementation provides one; a read-only store disables the purge.
func documentEvictorOf(storage nodeStorage) documentstore.DocumentEvictor {
	evictor, _ := storage.documentDirectory.(documentstore.DocumentEvictor)

	return evictor
}

// runRedirectPurge sweeps pre-SEARCH-28 search-engine tracking redirects out
// of the live corpus once per boot (SEARCH-29).
func runRedirectPurge(ctx context.Context, assembled node) {
	redirectpurge.New(assembled.docScan, assembled.docEvictor, assembled.index).Run(ctx)
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
