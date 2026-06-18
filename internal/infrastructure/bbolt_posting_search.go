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
		excluded, err := excludedURLHashes(ctx, bucket, query.ExcludeHashes)
		if err != nil {
			return err
		}
		allowed := hashSet(query.URLHashes)
		for _, word := range query.WordHashes {
			postings, count, truncated, err := searchWordPostings(
				ctx,
				bucket,
				word,
				query,
				allowed,
				excluded,
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
			entry, err := yacymodel.ParseRWIEntry(string(value))
			if err != nil {
				return nil, fmt.Errorf("parse rwi: %w", err)
			}
			if urlHash, err := entry.URLHash(); err == nil {
				excluded[urlHash] = struct{}{}
			}
		}
	}
	return excluded, nil
}

func searchWordPostings(
	ctx context.Context,
	bucket *bolt.Bucket,
	word yacymodel.Hash,
	query ports.PostingSearchQuery,
	allowed map[yacymodel.Hash]struct{},
	excluded map[yacymodel.Hash]struct{},
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
		entry, err := yacymodel.ParseRWIEntry(string(value))
		if err != nil {
			return nil, 0, false, fmt.Errorf("parse rwi: %w", err)
		}
		if !postingMatchesSearch(entry, query, allowed, excluded) {
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

func postingMatchesSearch(
	entry yacymodel.RWIEntry,
	query ports.PostingSearchQuery,
	allowed map[yacymodel.Hash]struct{},
	excluded map[yacymodel.Hash]struct{},
) bool {
	if query.Language != "" && entry.Properties[yacymodel.ColLanguage] != query.Language {
		return false
	}
	distance, err := yacymodel.DecodeCardinal(entry.Properties[yacymodel.ColWordDistance])
	if err != nil {
		distance = 0
	}
	if query.MaxDistance > 0 && distance > uint64(query.MaxDistance) {
		return false
	}
	urlHash, err := entry.URLHash()
	if err != nil {
		return false
	}
	if len(allowed) != 0 {
		if _, ok := allowed[urlHash]; !ok {
			return false
		}
	}
	if _, ok := excluded[urlHash]; ok {
		return false
	}
	if !matchesSiteHash(urlHash, query.SiteHash) {
		return false
	}
	if !matchesContentDomain(entry, query.ContentDomain, query.StrictContentDom) {
		return false
	}
	if !matchesConstraint(entry, query.Constraint) {
		return false
	}
	return true
}

func hashSet(hashes []yacymodel.Hash) map[yacymodel.Hash]struct{} {
	if len(hashes) == 0 {
		return nil
	}
	out := make(map[yacymodel.Hash]struct{}, len(hashes))
	for _, hash := range hashes {
		out[hash] = struct{}{}
	}
	return out
}
