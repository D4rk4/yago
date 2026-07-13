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
	return e.PurgeStalePostingsForURLs(
		ctx, map[yagomodel.Hash]map[yagomodel.Hash]struct{}{url: live},
	)
}

// PurgeStalePostingsForURLs runs the stale sweep for a whole ingest
// micro-batch in one transaction — one commit per touched shard instead of one
// per page, which is what fsync-bound storage feels (IO-AGG-01).
func (e Evictor) PurgeStalePostingsForURLs(
	ctx context.Context,
	staleByURL map[yagomodel.Hash]map[yagomodel.Hash]struct{},
) (int, error) {
	purged := 0
	err := e.vault.Update(ctx, func(tx *vault.Txn) error {
		purged = 0
		for url, live := range staleByURL {
			count, err := e.purgeStaleInTx(tx, url, live)
			if err != nil {
				return err
			}
			purged += count
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("purge stale postings: %w", err)
	}

	return purged, nil
}

// purgeStaleInTx drops one URL's postings for words absent from its live set,
// inside the caller's transaction.
func (e Evictor) purgeStaleInTx(
	tx *vault.Txn,
	url yagomodel.Hash,
	live map[yagomodel.Hash]struct{},
) (int, error) {
	words, err := e.references.WordsReferencing(tx, url)
	if err != nil {
		return 0, fmt.Errorf("words referencing url: %w", err)
	}
	purged := 0
	for _, word := range words {
		if _, kept := live[word]; kept {
			continue
		}
		deleted, err := e.postings.PurgePosting(tx, word, url)
		if err != nil {
			return 0, fmt.Errorf("purge stale posting: %w", err)
		}
		if deleted {
			purged++
		}
	}

	return purged, nil
}
