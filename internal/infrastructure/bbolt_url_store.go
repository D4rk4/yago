package infrastructure

import (
	"context"
	"fmt"
	"log/slog"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func (s *BboltStorage) StoreURLs(
	ctx context.Context,
	rows []yacymodel.URIMetadataRow,
) (ports.StoreURLsResult, error) {
	if err := ctx.Err(); err != nil {
		return ports.StoreURLsResult{}, wrapContextErr(err)
	}
	if err := s.rejectAtCapacity(); err != nil {
		return ports.StoreURLsResult{}, err
	}

	var result ports.StoreURLsResult
	err := s.update(func(tx *bolt.Tx) error {
		urls := tx.Bucket(bucketURLs)
		counts := tx.Bucket(bucketCounts)
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return wrapContextErr(err)
			}

			hash, err := row.URLHash()
			if err != nil {
				slog.WarnContext(
					ctx,
					"url row discarded",
					"reason",
					"invalid url hash",
					"error",
					err,
				)
				continue
			}

			key := []byte(hash)
			if urls.Get(key) != nil {
				result.Existing = append(result.Existing, hash)
				continue
			}
			if err := urls.Put(key, []byte(row.String())); err != nil {
				result.Rejected = append(result.Rejected, hash)
				slog.WarnContext(ctx, "url row discarded", "reason", "store failed", "error", err)
				continue
			}
			if err := incrementCount(counts, countURLs); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return ports.StoreURLsResult{}, err
	}

	return result, nil
}

func (s *BboltStorage) MissingURLs(
	ctx context.Context,
	hashes []yacymodel.Hash,
) ([]yacymodel.Hash, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}

	missing := make([]yacymodel.Hash, 0)
	seen := make(map[yacymodel.Hash]struct{}, len(hashes))
	err := s.view(func(tx *bolt.Tx) error {
		urls := tx.Bucket(bucketURLs)
		for _, hash := range hashes {
			if err := ctx.Err(); err != nil {
				return wrapContextErr(err)
			}
			if _, ok := seen[hash]; ok {
				continue
			}
			seen[hash] = struct{}{}
			if urls.Get([]byte(hash)) == nil {
				missing = append(missing, hash)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return missing, nil
}

func (s *BboltStorage) RowsByHash(
	ctx context.Context,
	hashes []yacymodel.Hash,
) ([]yacymodel.URIMetadataRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}

	rows := make([]yacymodel.URIMetadataRow, 0, len(hashes))
	err := s.view(func(tx *bolt.Tx) error {
		urls := tx.Bucket(bucketURLs)
		for _, hash := range hashes {
			if err := ctx.Err(); err != nil {
				return wrapContextErr(err)
			}

			raw := urls.Get([]byte(hash))
			if raw == nil {
				continue
			}
			row, err := yacymodel.ParseURIMetadataRow(string(raw))
			if err != nil {
				return fmt.Errorf("parse url metadata: %w", err)
			}
			rows = append(rows, row)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return rows, nil
}

func (s *BboltStorage) URLCount(ctx context.Context) (int, error) {
	return s.count(ctx, countURLs)
}
