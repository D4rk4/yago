package yagonode

import (
	"context"

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
// of the live corpus once per boot (SEARCH-29). It runs detached from the
// background WaitGroup: the sweep is best-effort cleanup, and shutdown must
// not wait for a corpus scan.
func runRedirectPurge(ctx context.Context, assembled node) {
	redirectpurge.New(assembled.docScan, assembled.docEvictor, assembled.index).Run(ctx)
}
