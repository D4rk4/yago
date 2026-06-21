package infrastructure

import (
	"context"
	"fmt"
	"log/slog"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

//nolint:gocognit,revive // FIXME: split posting validation, encoding, and quota handling after rules are committed.
func (s *BboltStorage) AppendRWI(
	ctx context.Context,
	entries []yacymodel.RWIPosting,
) ([]yacymodel.Hash, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}
	if err := s.rejectAtCapacity(); err != nil {
		return nil, err
	}

	var rejected []yacymodel.Hash
	err := s.update(func(tx *bolt.Tx) error {
		rwi := tx.Bucket(bucketRWI)
		refs := tx.Bucket(bucketReferencedURLs)
		counts := tx.Bucket(bucketCounts)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return wrapContextErr(err)
			}

			urlHash, err := entry.URLHash()
			if err != nil {
				slog.WarnContext(
					ctx,
					"rwi posting discarded",
					slog.String("reason", "invalid url hash"),
					slog.Any("error", err),
				)
				continue
			}
			if !entry.WordHash.Valid() {
				rejected = append(rejected, urlHash)
				slog.WarnContext(
					ctx,
					"rwi posting discarded",
					slog.String("reason", "invalid word hash"),
					slog.String("wordHash", entry.WordHash.String()),
				)
				continue
			}

			key := rwiPostingKey(entry.WordHash, urlHash)
			existing := rwi.Get(key)
			if existing == nil {
				if err := incrementCount(counts, countRWI); err != nil {
					return err
				}
			}
			if refs.Get([]byte(urlHash)) == nil {
				if err := refs.Put([]byte(urlHash), setMember); err != nil {
					return fmt.Errorf("store referenced url: %w", err)
				}
				if err := incrementCount(counts, countReferencedURLs); err != nil {
					return err
				}
			}
			if err := rwi.Put(key, yacymodel.EncodeRWIPosting(entry)); err != nil {
				return fmt.Errorf("store rwi: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return rejected, nil
}

func (s *BboltStorage) RWICount(ctx context.Context) (int, error) {
	return s.count(ctx, countRWI)
}

func (s *BboltStorage) ReferencedURLCount(ctx context.Context) (int, error) {
	return s.count(ctx, countReferencedURLs)
}

func deleteRWIPosting(rwi, counts *bolt.Bucket, key []byte) (bool, error) {
	if rwi.Get(key) == nil {
		return false, nil
	}
	if err := rwi.Delete(key); err != nil {
		return false, fmt.Errorf("delete rwi: %w", err)
	}
	if err := decrementCount(counts, countRWI); err != nil {
		return false, err
	}

	return true, nil
}
