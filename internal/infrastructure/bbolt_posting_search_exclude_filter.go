package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func excludedURLHashes(
	ctx context.Context,
	bucket *bolt.Bucket,
	words []yacymodel.Hash,
) (map[yacymodel.Hash]struct{}, error) {
	excluded := make(map[yacymodel.Hash]struct{})
	for _, word := range words {
		prefix := []byte(word)
		cursor := bucket.Cursor()
		for key, value := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, value = cursor.Next() {
			if err := ctx.Err(); err != nil {
				return nil, wrapContextErr(err)
			}
			entry, err := yacymodel.DecodeRWIPosting(word, value)
			if err != nil {
				return nil, fmt.Errorf("parse rwi: %w", err)
			}
			if urlHash, err := entry.URLHash(); err == nil {
				excluded[urlHash.Hash()] = struct{}{}
			} else {
				slog.WarnContext(
					ctx,
					"rwi exclude candidate discarded",
					slog.String("reason", "invalid url hash"),
					slog.Any("error", err),
				)
			}
		}
	}
	return excluded, nil
}
