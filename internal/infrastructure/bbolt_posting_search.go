package infrastructure

import (
	"bytes"
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func (s *BboltStorage) SearchPostings(
	ctx context.Context,
	query ports.PostingSearchQuery,
) (ports.PostingSearchResult, error) {
	if err := ctx.Err(); err != nil {
		return ports.PostingSearchResult{}, wrapContextErr(err)
	}

	result := ports.PostingSearchResult{
		Postings: make(map[yacymodel.Hash][]yacymodel.RWIEntry, len(query.WordHashes)),
		Counts:   make(map[yacymodel.Hash]int, len(query.WordHashes)),
	}
	err := s.view(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketRWI)
		matcher, err := newPostingSearchMatcher(ctx, bucket, query)
		if err != nil {
			return err
		}
		for _, word := range query.WordHashes {
			postings, count, truncated, err := searchWordPostings(
				ctx,
				bucket,
				word,
				query,
				matcher,
			)
			if err != nil {
				return err
			}
			result.Postings[word] = postings
			result.Counts[word] = count
			result.Truncated = result.Truncated || truncated
		}

		return nil
	})
	if err != nil {
		return ports.PostingSearchResult{}, err
	}

	return result, nil
}

func searchWordPostings(
	ctx context.Context,
	bucket *bolt.Bucket,
	word yacymodel.Hash,
	query ports.PostingSearchQuery,
	matcher postingSearchMatcher,
) ([]yacymodel.RWIEntry, int, bool, error) {
	var (
		postings  []yacymodel.RWIEntry
		count     int
		truncated bool
	)
	prefix := []byte(word)
	cursor := bucket.Cursor()
	for key, value := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, value = cursor.Next() {
		if err := ctx.Err(); err != nil {
			return nil, 0, false, wrapContextErr(err)
		}
		entry, err := yacymodel.DecodeRWIPosting(word, value)
		if err != nil {
			return nil, 0, false, fmt.Errorf("parse rwi: %w", err)
		}
		if !matcher.matches(ctx, entry) {
			continue
		}
		count++
		if query.LimitPerWord > 0 && len(postings) >= query.LimitPerWord {
			truncated = true
			continue
		}
		postings = append(postings, entry)
	}
	return postings, count, truncated, nil
}
