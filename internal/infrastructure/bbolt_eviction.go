package infrastructure

import (
	"container/heap"
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func (s *BboltStorage) UsedBytes(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, wrapContextErr(err)
	}

	return s.measureUsedBytes()
}

func (s *BboltStorage) measureUsedBytes() (int64, error) {
	var used int64
	err := s.view(func(tx *bolt.Tx) error {
		stats := s.db.Stats()
		pageSize := int64(s.db.Info().PageSize)
		free := int64(stats.FreePageN+stats.PendingPageN) * pageSize
		used = tx.Size() - free

		return nil
	})
	if err != nil {
		return 0, err
	}
	if used < 0 {
		used = 0
	}

	return used, nil
}

func (s *BboltStorage) SelectEvictionCandidates(
	ctx context.Context,
	limit int,
) ([]yacymodel.Hash, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}
	if limit <= 0 {
		return nil, nil
	}

	staleest := &stalestURLs{limit: limit}
	err := s.view(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(bucketURLs).Cursor()
		for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
			if err := ctx.Err(); err != nil {
				return wrapContextErr(err)
			}

			row, err := yacymodel.DecodeURIMetadata(value)
			if err != nil {
				return fmt.Errorf("decode url metadata: %w", err)
			}
			hash, err := row.URLHash()
			if err != nil {
				return fmt.Errorf("url metadata hash: %w", err)
			}
			staleest.offer(hash.Hash(), row.Freshness())
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return staleest.drain(), nil
}

//nolint:gocognit,revive // FIXME: split URL deletion from posting cleanup after rules are committed.
func (s *BboltStorage) DeleteURLs(
	ctx context.Context,
	urls []yacymodel.Hash,
) (ports.EvictionResult, error) {
	if err := ctx.Err(); err != nil {
		return ports.EvictionResult{}, wrapContextErr(err)
	}
	if len(urls) == 0 {
		return ports.EvictionResult{}, nil
	}

	targets := make(map[string]struct{}, len(urls))
	for _, hash := range urls {
		targets[hash.String()] = struct{}{}
	}

	var result ports.EvictionResult
	err := s.update(func(tx *bolt.Tx) error {
		rwi := tx.Bucket(bucketRWI)
		refs := tx.Bucket(bucketReferencedURLs)
		urlBucket := tx.Bucket(bucketURLs)
		counts := tx.Bucket(bucketCounts)

		stale, err := postingsForURLs(ctx, rwi, targets)
		if err != nil {
			return err
		}
		for _, key := range stale {
			deleted, err := deleteRWIPosting(rwi, counts, key)
			if err != nil {
				return err
			}
			if deleted {
				result.PostingsDeleted++
			}
		}

		for _, hash := range urls {
			key := []byte(hash)
			if urlBucket.Get(key) != nil {
				if err := urlBucket.Delete(key); err != nil {
					return fmt.Errorf("delete url metadata: %w", err)
				}
				if err := decrementCount(counts, countURLs); err != nil {
					return err
				}
				result.URLsDeleted++
			}
			if refs.Get(key) != nil {
				if err := refs.Delete(key); err != nil {
					return fmt.Errorf("delete referenced url: %w", err)
				}
				if err := decrementCount(counts, countReferencedURLs); err != nil {
					return err
				}
			}
		}

		return nil
	})
	if err != nil {
		return ports.EvictionResult{}, err
	}

	return result, nil
}

func postingsForURLs(
	ctx context.Context,
	rwi *bolt.Bucket,
	targets map[string]struct{},
) ([][]byte, error) {
	var stale [][]byte
	cursor := rwi.Cursor()
	for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
		if err := ctx.Err(); err != nil {
			return nil, wrapContextErr(err)
		}
		id, err := parseRWIPostingKey(key)
		if err != nil {
			continue
		}
		if _, ok := targets[id.URLHash.String()]; !ok {
			continue
		}
		stale = append(stale, append([]byte(nil), key...))
	}

	return stale, nil
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

var _ ports.RWIEvictor = (*BboltStorage)(nil)
