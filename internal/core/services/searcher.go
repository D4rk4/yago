package services

import (
	"context"
	"fmt"
	"slices"
	"time"

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
	start := time.Now()
	if len(query.Words) == 0 && query.Abstracts.Mode == contracts.SearchAbstractExplicit {
		return s.searchAbstractCounts(ctx, query, start)
	}

	postings, err := s.rwi.PostingsForWords(ctx, query.Words, s.postingsPerWord)
	if err != nil {
		return contracts.SearchResult{}, fmt.Errorf("postings for words: %w", err)
	}

	var wordCounts map[yacymodel.Hash]int
	if query.Abstracts.Mode != contracts.SearchAbstractNone {
		wordCounts = make(map[yacymodel.Hash]int, len(query.Words))
	}

	var joined map[yacymodel.Hash]searchCandidate
	for _, word := range query.Words {
		matched := matchWord(postings[word], query.MaxDistance, query.Filters.Language)
		if wordCounts != nil {
			wordCounts[word] = len(matched)
		}
		joined = intersect(joined, matched)
	}

	if err := s.applyExclusions(ctx, query.Exclude, joined); err != nil {
		return contracts.SearchResult{}, err
	}
	restrictToURLs(joined, query.URLs)

	joinCount := len(joined)
	ordered := truncate(rankedHashes(joined), query.MaxResults)

	rows, err := s.urls.RowsByHash(ctx, ordered)
	if err != nil {
		return contracts.SearchResult{}, fmt.Errorf("rows by hash: %w", err)
	}

	return contracts.SearchResult{
		Resources:  rows,
		JoinCount:  joinCount,
		SearchTime: time.Since(start),
		WordCounts: wordCounts,
	}, nil
}

type searchCandidate struct {
	hash     yacymodel.Hash
	hits     uint64
	distance uint64
}

func matchWord(
	entries []yacymodel.RWIEntry,
	maxDistance int,
	language string,
) map[yacymodel.Hash]searchCandidate {
	matched := make(map[yacymodel.Hash]searchCandidate, len(entries))
	for _, entry := range entries {
		if language != "" && entry.Properties[yacymodel.ColLanguage] != language {
			continue
		}
		distance := wordDistance(entry)
		if maxDistance > 0 && distance > uint64(maxDistance) {
			continue
		}
		urlHash, err := entry.URLHash()
		if err != nil {
			continue
		}
		if _, seen := matched[urlHash]; !seen {
			matched[urlHash] = searchCandidate{
				hash:     urlHash,
				hits:     hitCount(entry),
				distance: distance,
			}
		}
	}

	return matched
}

func (s Searcher) applyExclusions(
	ctx context.Context,
	exclude []yacymodel.Hash,
	joined map[yacymodel.Hash]searchCandidate,
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
	joined map[yacymodel.Hash]searchCandidate,
	matched map[yacymodel.Hash]searchCandidate,
) map[yacymodel.Hash]searchCandidate {
	if joined == nil {
		return matched
	}

	for hash, candidate := range joined {
		next, ok := matched[hash]
		if !ok {
			delete(joined, hash)
			continue
		}
		candidate.hits += next.hits
		candidate.distance += next.distance
		joined[hash] = candidate
	}

	return joined
}

func restrictToURLs(joined map[yacymodel.Hash]searchCandidate, urls []yacymodel.Hash) {
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

func wordDistance(entry yacymodel.RWIEntry) uint64 {
	n, err := yacymodel.DecodeCardinal(entry.Properties[yacymodel.ColWordDistance])
	if err != nil {
		return 0
	}

	return n
}

func hitCount(entry yacymodel.RWIEntry) uint64 {
	n, err := yacymodel.DecodeCardinal(entry.Properties[yacymodel.ColHitCount])
	if err != nil {
		return 0
	}

	return n
}

func rankedHashes(set map[yacymodel.Hash]searchCandidate) []yacymodel.Hash {
	candidates := make([]searchCandidate, 0, len(set))
	for hash, candidate := range set {
		candidate.hash = hash
		candidates = append(candidates, candidate)
	}
	slices.SortFunc(candidates, func(a, b searchCandidate) int {
		if a.hits != b.hits {
			return compareDesc(a.hits, b.hits)
		}
		if a.distance != b.distance {
			return compareAsc(a.distance, b.distance)
		}
		return compareAsc(a.hash, b.hash)
	})

	hashes := make([]yacymodel.Hash, 0, len(candidates))
	for _, candidate := range candidates {
		hashes = append(hashes, candidate.hash)
	}
	return hashes
}

func truncate(hashes []yacymodel.Hash, maxResults int) []yacymodel.Hash {
	if maxResults > 0 && len(hashes) > maxResults {
		return hashes[:maxResults]
	}

	return hashes
}

func (s Searcher) searchAbstractCounts(
	ctx context.Context,
	query contracts.SearchQuery,
	start time.Time,
) (contracts.SearchResult, error) {
	postings, err := s.rwi.PostingsForWords(ctx, query.Abstracts.Words, s.postingsPerWord)
	if err != nil {
		return contracts.SearchResult{}, fmt.Errorf("postings for words: %w", err)
	}

	wordCounts := make(map[yacymodel.Hash]int, len(query.Abstracts.Words))
	for _, word := range query.Abstracts.Words {
		matched := matchWord(postings[word], query.MaxDistance, query.Filters.Language)
		restrictToURLs(matched, query.URLs)
		wordCounts[word] = len(matched)
	}

	return contracts.SearchResult{
		SearchTime: time.Since(start),
		WordCounts: wordCounts,
	}, nil
}

func compareDesc[T ~uint64](a, b T) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func compareAsc[T ~uint64 | ~string](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
