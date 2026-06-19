package infrastructure

import (
	"bytes"
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const rwiMigrationBatch = 1000

type RWIMigrationReport struct {
	Scanned   int
	Rewritten int
}

func (s *BboltStorage) MigrateRWIPostings(ctx context.Context) (RWIMigrationReport, error) {
	var report RWIMigrationReport
	var resume []byte
	for {
		if err := ctx.Err(); err != nil {
			return report, wrapContextErr(err)
		}
		done, next, err := s.migrateRWIBatch(ctx, resume, &report)
		if err != nil {
			return report, err
		}
		if done {
			return report, nil
		}
		resume = next
	}
}

func (s *BboltStorage) migrateRWIBatch(
	ctx context.Context,
	resume []byte,
	report *RWIMigrationReport,
) (bool, []byte, error) {
	var (
		rewrites []rwiRewrite
		lastKey  []byte
		scanned  int
	)
	err := s.update(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(bucketRWI).Cursor()
		for key, value := seekRWIBatch(cursor, resume); key != nil && scanned < rwiMigrationBatch; key, value = cursor.Next() {
			if err := ctx.Err(); err != nil {
				return wrapContextErr(err)
			}
			scanned++
			lastKey = append(lastKey[:0], key...)
			if isBinaryPosting(value) {
				continue
			}
			rewrite, err := rewriteLegacyPosting(key, value)
			if err != nil {
				return err
			}
			rewrites = append(rewrites, rewrite)
		}
		bucket := tx.Bucket(bucketRWI)
		for _, rewrite := range rewrites {
			if err := bucket.Put(rewrite.key, rewrite.value); err != nil {
				return fmt.Errorf("rewrite posting: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return false, nil, err
	}
	report.Scanned += scanned
	report.Rewritten += len(rewrites)
	if scanned < rwiMigrationBatch {
		return true, nil, nil
	}
	return false, lastKey, nil
}

type rwiRewrite struct {
	key   []byte
	value []byte
}

func seekRWIBatch(cursor *bolt.Cursor, resume []byte) ([]byte, []byte) {
	if resume == nil {
		return cursor.First()
	}
	key, value := cursor.Seek(resume)
	if key != nil && bytes.Equal(key, resume) {
		return cursor.Next()
	}
	return key, value
}

func isBinaryPosting(value []byte) bool {
	return len(value) > 0 && value[0] == yacymodel.RWIPostingFormatV1
}

func rewriteLegacyPosting(key, value []byte) (rwiRewrite, error) {
	wordHash := yacymodel.Hash(key[:yacymodel.HashLength])
	entry, err := yacymodel.DecodeRWIPosting(wordHash, value)
	if err != nil {
		return rwiRewrite{}, fmt.Errorf("decode legacy posting: %w", err)
	}
	return rwiRewrite{
		key:   append([]byte(nil), key...),
		value: yacymodel.EncodeRWIPosting(entry),
	}, nil
}
