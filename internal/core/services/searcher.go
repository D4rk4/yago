package services

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type Searcher struct {
	rwi             ports.RWIStore
	urls            ports.URLStore
	postingsPerWord int
}

func NewSearcher(rwi ports.RWIStore, urls ports.URLStore, postingsPerWord int) Searcher {
	return Searcher{rwi: rwi, urls: urls, postingsPerWord: postingsPerWord}
}

func (s Searcher) Search(
	ctx context.Context,
	query contracts.SearchQuery,
) (contracts.SearchResult, error) {
	postings, err := s.rwi.PostingsForWords(ctx, query.Words, s.postingsPerWord)
	if err != nil {
		return contracts.SearchResult{}, fmt.Errorf("postings for words: %w", err)
	}

	wordCounts := make(map[yacymodel.Hash]int, len(query.Words))
	abstracts := make(map[yacymodel.Hash]string, len(query.Words))

	var joined map[yacymodel.Hash]struct{}
	for _, word := range query.Words {
		matched, abstract := matchWord(postings[word], query.MaxDistance)
		wordCounts[word] = len(matched)
		abstracts[word] = abstract
		joined = intersect(joined, matched)
	}

	if err := s.applyExclusions(ctx, query.Exclude, joined); err != nil {
		return contracts.SearchResult{}, err
	}
	restrictToURLs(joined, query.URLs)

	joinCount := len(joined)
	ordered := truncate(sortedHashes(joined), query.MaxResults)

	rows, err := s.urls.RowsByHash(ctx, ordered)
	if err != nil {
		return contracts.SearchResult{}, fmt.Errorf("rows by hash: %w", err)
	}

	return contracts.SearchResult{
		Resources:  rows,
		JoinCount:  joinCount,
		WordCounts: wordCounts,
		Abstracts:  abstracts,
	}, nil
}

func matchWord(
	entries []yacymodel.RWIEntry,
	maxDistance int,
) (map[yacymodel.Hash]struct{}, string) {
	matched := make(map[yacymodel.Hash]struct{}, len(entries))
	var abstract strings.Builder
	for _, entry := range entries {
		if maxDistance > 0 && wordDistance(entry) > maxDistance {
			continue
		}
		urlHash, err := entry.URLHash()
		if err != nil {
			continue
		}
		if _, seen := matched[urlHash]; !seen {
			matched[urlHash] = struct{}{}
			abstract.WriteString(string(urlHash))
		}
	}

	return matched, abstract.String()
}

func (s Searcher) applyExclusions(
	ctx context.Context,
	exclude []yacymodel.Hash,
	joined map[yacymodel.Hash]struct{},
) error {
	if len(exclude) == 0 {
		return nil
	}

	postings, err := s.rwi.PostingsForWords(ctx, exclude, s.postingsPerWord)
	if err != nil {
		return fmt.Errorf("postings for words: %w", err)
	}
	for _, entries := range postings {
		for _, entry := range entries {
			if urlHash, err := entry.URLHash(); err == nil {
				delete(joined, urlHash)
			}
		}
	}

	return nil
}

func intersect(
	joined map[yacymodel.Hash]struct{},
	matched map[yacymodel.Hash]struct{},
) map[yacymodel.Hash]struct{} {
	if joined == nil {
		return matched
	}

	for hash := range joined {
		if _, ok := matched[hash]; !ok {
			delete(joined, hash)
		}
	}

	return joined
}

func restrictToURLs(joined map[yacymodel.Hash]struct{}, urls []yacymodel.Hash) {
	if len(urls) == 0 {
		return
	}

	allowed := make(map[yacymodel.Hash]struct{}, len(urls))
	for _, hash := range urls {
		allowed[hash] = struct{}{}
	}
	for hash := range joined {
		if _, ok := allowed[hash]; !ok {
			delete(joined, hash)
		}
	}
}

func wordDistance(entry yacymodel.RWIEntry) int {
	n, err := strconv.Atoi(entry.Properties[yacymodel.ColWordDistance])
	if err != nil {
		return 0
	}

	return n
}

func sortedHashes(set map[yacymodel.Hash]struct{}) []yacymodel.Hash {
	hashes := make([]yacymodel.Hash, 0, len(set))
	for hash := range set {
		hashes = append(hashes, hash)
	}
	slices.Sort(hashes)

	return hashes
}

func truncate(hashes []yacymodel.Hash, maxResults int) []yacymodel.Hash {
	if maxResults > 0 && len(hashes) > maxResults {
		return hashes[:maxResults]
	}

	return hashes
}
