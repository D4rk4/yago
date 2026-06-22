package urlmeta

import (
	"container/heap"
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

type urlEvictor struct {
	vault      *boltvault.Vault
	collection *boltvault.Collection[yacymodel.URIMetadataRow]
}

func (e urlEvictor) SelectStale(ctx context.Context, limit int) ([]yacymodel.Hash, error) {
	if limit <= 0 {
		return nil, nil
	}

	staleest := &stalestURLs{limit: limit}
	err := e.vault.View(ctx, func(tx *boltvault.Txn) error {
		return e.collection.Scan(
			tx,
			nil,
			func(_ boltvault.Key, row yacymodel.URIMetadataRow) (bool, error) {
				if err := ctx.Err(); err != nil {
					return false, fmt.Errorf("context: %w", err)
				}
				hash, err := row.URLHash()
				if err != nil {
					return false, fmt.Errorf("url metadata hash: %w", err)
				}
				staleest.offer(hash.Hash(), row.Freshness())

				return true, nil
			},
		)
	})
	if err != nil {
		return nil, fmt.Errorf("select stale urls: %w", err)
	}

	return staleest.drain(), nil
}

func (e urlEvictor) Purge(tx *boltvault.Txn, urls []yacymodel.Hash) (PurgeResult, error) {
	var result PurgeResult
	for _, hash := range urls {
		deleted, err := e.collection.Delete(tx, boltvault.Key(hash))
		if err != nil {
			return PurgeResult{}, fmt.Errorf("delete url metadata: %w", err)
		}
		if deleted {
			result.URLsDeleted++
		}
	}

	return result, nil
}

type stalestURLs struct {
	limit   int
	entries staleHeap
}

func (s *stalestURLs) offer(hash yacymodel.Hash, freshness string) {
	if s.entries.Len() < s.limit {
		heap.Push(&s.entries, staleEntry{hash: hash, freshness: freshness})

		return
	}
	if freshness < s.entries[0].freshness {
		s.entries[0] = staleEntry{hash: hash, freshness: freshness}
		heap.Fix(&s.entries, 0)
	}
}

func (s *stalestURLs) drain() []yacymodel.Hash {
	hashes := make([]yacymodel.Hash, s.entries.Len())
	for i := len(hashes) - 1; i >= 0; i-- {
		hashes[i] = heap.Pop(&s.entries).(staleEntry).hash
	}

	return hashes
}

type staleEntry struct {
	hash      yacymodel.Hash
	freshness string
}

type staleHeap []staleEntry

func (h staleHeap) Len() int           { return len(h) }
func (h staleHeap) Less(i, j int) bool { return h[i].freshness > h[j].freshness }
func (h staleHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *staleHeap) Push(x any)        { *h = append(*h, x.(staleEntry)) }
func (h *staleHeap) Pop() any {
	old := *h
	n := len(old)
	entry := old[n-1]
	*h = old[:n-1]

	return entry
}

var _ URLEvictor = urlEvictor{}
