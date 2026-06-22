package search

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type postingQuery struct {
	wordHashes       []yacymodel.Hash
	excludeHashes    []yacymodel.Hash
	urlHashes        []yacymodel.Hash
	limitPerWord     int
	maxDistance      int
	language         string
	contentDomain    string
	strictContentDom bool
	constraint       string
	siteHash         string
}

type postingResult struct {
	postings  map[yacymodel.Hash][]yacymodel.RWIPosting
	counts    map[yacymodel.Hash]int
	truncated bool
}

func scanPostings(
	ctx context.Context,
	index rwi.PostingScanner,
	query postingQuery,
) (postingResult, error) {
	matcher, err := newPostingFilter(ctx, index, query)
	if err != nil {
		return postingResult{}, err
	}

	result := postingResult{
		postings: make(map[yacymodel.Hash][]yacymodel.RWIPosting, len(query.wordHashes)),
		counts:   make(map[yacymodel.Hash]int, len(query.wordHashes)),
	}
	for _, word := range query.wordHashes {
		var (
			matched   []yacymodel.RWIPosting
			count     int
			truncated bool
		)
		err := index.ScanWord(ctx, word, func(entry yacymodel.RWIPosting) (bool, error) {
			if !matcher.matches(ctx, entry) {
				return true, nil
			}
			count++
			if query.limitPerWord > 0 && len(matched) >= query.limitPerWord {
				truncated = true

				return true, nil
			}
			matched = append(matched, entry)

			return true, nil
		})
		if err != nil {
			return postingResult{}, fmt.Errorf("scan word: %w", err)
		}
		result.postings[word] = matched
		result.counts[word] = count
		result.truncated = result.truncated || truncated
	}

	return result, nil
}

func excludedURLHashes(
	ctx context.Context,
	index rwi.PostingScanner,
	words []yacymodel.Hash,
) (map[yacymodel.Hash]struct{}, error) {
	excluded := make(map[yacymodel.Hash]struct{})
	for _, word := range words {
		err := index.ScanWord(ctx, word, func(entry yacymodel.RWIPosting) (bool, error) {
			if urlHash, err := entry.URLHash(); err == nil {
				excluded[urlHash.Hash()] = struct{}{}
			}

			return true, nil
		})
		if err != nil {
			return nil, fmt.Errorf("scan excluded word: %w", err)
		}
	}

	return excluded, nil
}
