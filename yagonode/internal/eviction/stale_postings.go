package eviction

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// PurgeStalePostings drops a URL's RWI postings for every word absent from the
// live set, in one capacity-exempt transaction (RWI-01). A recrawl replaces
// the document and the full-text index wholesale, but the word→URL posting
// index only ever gains entries — so words removed from a page would keep
// answering DHT and remote searches forever. The ingest path calls this with
// the new batch's word set before storing it; the reference projection makes
// the diff cheap, which Java YaCy cannot do without re-parsing the old page.
func (e Evictor) PurgeStalePostings(
	ctx context.Context,
	url yagomodel.Hash,
	live map[yagomodel.Hash]struct{},
) (int, error) {
	purged := 0
	err := e.vault.Update(ctx, func(tx *vault.Txn) error {
		words, err := e.references.WordsReferencing(tx, url)
		if err != nil {
			return fmt.Errorf("words referencing url: %w", err)
		}
		for _, word := range words {
			if _, kept := live[word]; kept {
				continue
			}
			deleted, err := e.postings.PurgePosting(tx, word, url)
			if err != nil {
				return fmt.Errorf("purge stale posting: %w", err)
			}
			if deleted {
				purged++
			}
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("purge stale postings: %w", err)
	}

	return purged, nil
}
