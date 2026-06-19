package infrastructure

import (
	"context"
	"fmt"
	"log/slog"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func (s *BboltStorage) AppendRWI(
	ctx context.Context,
	entries []yacymodel.RWIEntry,
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
					"rwi entry discarded",
					"reason",
					"invalid url hash",
					"error",
					err,
				)
				continue
			}
			if !entry.WordHash.Valid() {
				rejected = append(rejected, urlHash)
				slog.WarnContext(
					ctx,
					"rwi entry discarded",
					"reason",
					"invalid word hash",
					"word_hash",
					entry.WordHash,
				)
				continue
			}

			key := rwiKey(entry.WordHash, urlHash)
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

func rwiKey(wordHash yacymodel.Hash, urlHash yacymodel.Hash) []byte {
	key := make([]byte, 0, yacymodel.HashLength*2)
	key = append(key, wordHash.String()...)
	key = append(key, urlHash.String()...)

	return key
}
