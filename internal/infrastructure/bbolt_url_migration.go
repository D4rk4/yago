package infrastructure

import (
	"bytes"
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const urlMigrationBatch = 1000

type URLMigrationReport struct {
	Scanned   int
	Rewritten int
}

func (s *BboltStorage) MigrateURLMetadata(ctx context.Context) (URLMigrationReport, error) {
	var report URLMigrationReport
	var resume []byte
	for {
		if err := ctx.Err(); err != nil {
			return report, wrapContextErr(err)
		}
		done, next, err := s.migrateURLBatch(ctx, resume, &report)
		if err != nil {
			return report, err
		}
		if done {
			return report, nil
		}
		resume = next
	}
}

func (s *BboltStorage) migrateURLBatch(
	ctx context.Context,
	resume []byte,
	report *URLMigrationReport,
) (bool, []byte, error) {
	var (
		rewrites []urlRewrite
		lastKey  []byte
		scanned  int
	)
	err := s.update(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(bucketURLs).Cursor()
		for key, value := seekURLBatch(cursor, resume); key != nil && scanned < urlMigrationBatch; key, value = cursor.Next() {
			if err := ctx.Err(); err != nil {
				return wrapContextErr(err)
			}
			scanned++
			lastKey = append(lastKey[:0], key...)
			if isCompressedURLMetadata(value) {
				continue
			}
			rewrite, err := rewriteLegacyURLMetadata(key, value)
			if err != nil {
				return err
			}
			rewrites = append(rewrites, rewrite)
		}
		bucket := tx.Bucket(bucketURLs)
		for _, rewrite := range rewrites {
			if err := bucket.Put(rewrite.key, rewrite.value); err != nil {
				return fmt.Errorf("rewrite url metadata: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return false, nil, err
	}
	report.Scanned += scanned
	report.Rewritten += len(rewrites)
	if scanned < urlMigrationBatch {
		return true, nil, nil
	}
	return false, lastKey, nil
}

type urlRewrite struct {
	key   []byte
	value []byte
}

func seekURLBatch(cursor *bolt.Cursor, resume []byte) ([]byte, []byte) {
	if resume == nil {
		return cursor.First()
	}
	key, value := cursor.Seek(resume)
	if key != nil && bytes.Equal(key, resume) {
		return cursor.Next()
	}
	return key, value
}

func isCompressedURLMetadata(value []byte) bool {
	return len(value) > 0 && value[0] == yacymodel.URLMetadataFormatV1
}

func rewriteLegacyURLMetadata(key, value []byte) (urlRewrite, error) {
	row, err := yacymodel.DecodeURIMetadata(value)
	if err != nil {
		return urlRewrite{}, fmt.Errorf("decode legacy url metadata: %w", err)
	}
	encoded, err := yacymodel.EncodeURIMetadata(row)
	if err != nil {
		return urlRewrite{}, fmt.Errorf("encode url metadata: %w", err)
	}
	return urlRewrite{
		key:   append([]byte(nil), key...),
		value: encoded,
	}, nil
}
