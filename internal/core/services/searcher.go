package services

import (
	"context"
	"fmt"
	"log/slog"
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

	postingResult, err := s.rwi.SearchPostings(ctx, ports.PostingSearchQuery{
		WordHashes:    query.Words,
		ExcludeHashes: query.Exclude,
		URLHashes:     query.URLs,
		LimitPerWord:  s.postingsPerWord,
		MaxDistance:   query.MaxDistance,
		Language:      query.Filters.Language,

		ContentDomain:    query.Filters.ContentDomain,
		StrictContentDom: query.Filters.StrictContentDom,
		Constraint:       query.Filters.Constraint,
		SiteHash:         query.Filters.SiteHash,
	})
	if err != nil {
		return contracts.SearchResult{}, fmt.Errorf("search postings: %w", err)
	}

	var wordCounts map[yacymodel.Hash]int
	if query.Abstracts.Mode != contracts.SearchAbstractNone {
		wordCounts = make(map[yacymodel.Hash]int, len(query.Words))
	}
	abstractInputs := map[yacymodel.Hash]map[yacymodel.Hash]searchCandidate{}

	var joined map[yacymodel.Hash]searchCandidate
	for _, word := range query.Words {
		matched := matchWord(ctx, postingResult.Postings[word])
		if wordCounts != nil {
			wordCounts[word] = postingResult.Counts[word]
		}
		if query.Abstracts.Mode == contracts.SearchAbstractAuto {
			abstractInputs[word] = cloneCandidates(matched)
		}
		joined = intersect(joined, matched)
	}

	joinCount := len(joined)
	ordered := truncate(rankedHashes(joined), query.MaxResults)

	rows, err := s.urls.RowsByHash(ctx, ordered)
	if err != nil {
		return contracts.SearchResult{}, fmt.Errorf("rows by hash: %w", err)
	}
	abstracts, err := s.searchAbstracts(ctx, query, abstractInputs)
	if err != nil {
		return contracts.SearchResult{}, err
	}

	return contracts.SearchResult{
		Resources:  rows,
		JoinCount:  joinCount,
		SearchTime: time.Since(start),
		WordCounts: wordCounts,
		Abstracts:  abstracts,
	}, nil
}

type searchCandidate struct {
	hash     yacymodel.Hash
	hits     uint64
	distance uint64
}

func matchWord(
	ctx context.Context,
	entries []yacymodel.RWIEntry,
) map[yacymodel.Hash]searchCandidate {
	matched := make(map[yacymodel.Hash]searchCandidate, len(entries))
	for _, entry := range entries {
		distance, err := wordDistance(entry)
		if err != nil {
			slog.WarnContext(
				ctx,
				"rwi ranking field discarded",
				"field",
				yacymodel.ColWordDistance,
				"error",
				err,
			)
		}
		hits, err := hitCount(entry)
		if err != nil {
			slog.WarnContext(
				ctx,
				"rwi ranking field discarded",
				"field",
				yacymodel.ColHitCount,
				"error",
				err,
			)
		}
		urlHash, err := entry.URLHash()
		if err != nil {
			slog.WarnContext(
				ctx,
				"rwi search candidate discarded",
				"reason",
				"invalid url hash",
				"error",
				err,
			)
			continue
		}
		if _, seen := matched[urlHash]; !seen {
			matched[urlHash] = searchCandidate{
				hash:     urlHash,
				hits:     hits,
				distance: distance,
			}
		}
	}

	return matched
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

func cloneCandidates(in map[yacymodel.Hash]searchCandidate) map[yacymodel.Hash]searchCandidate {
	out := make(map[yacymodel.Hash]searchCandidate, len(in))
	for hash, candidate := range in {
		out[hash] = candidate
	}
	return out
}

func wordDistance(entry yacymodel.RWIEntry) (uint64, error) {
	n, err := yacymodel.DecodeCardinal(entry.Properties[yacymodel.ColWordDistance])
	if err != nil {
		return 0, fmt.Errorf("decode word distance: %w", err)
	}

	return n, nil
}

func hitCount(entry yacymodel.RWIEntry) (uint64, error) {
	n, err := yacymodel.DecodeCardinal(entry.Properties[yacymodel.ColHitCount])
	if err != nil {
		return 0, fmt.Errorf("decode hit count: %w", err)
	}

	return n, nil
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
	postingResult, err := s.rwi.SearchPostings(ctx, ports.PostingSearchQuery{
		WordHashes:   query.Abstracts.Words,
		URLHashes:    query.URLs,
		LimitPerWord: s.postingsPerWord,
		MaxDistance:  query.MaxDistance,
		Language:     query.Filters.Language,

		ContentDomain:    query.Filters.ContentDomain,
		StrictContentDom: query.Filters.StrictContentDom,
		Constraint:       query.Filters.Constraint,
		SiteHash:         query.Filters.SiteHash,
	})
	if err != nil {
		return contracts.SearchResult{}, fmt.Errorf("search postings: %w", err)
	}

	wordCounts := make(map[yacymodel.Hash]int, len(query.Abstracts.Words))
	abstracts := make(map[yacymodel.Hash]string, len(query.Abstracts.Words))
	for _, word := range query.Abstracts.Words {
		matched := matchWord(ctx, postingResult.Postings[word])
		wordCounts[word] = postingResult.Counts[word]
		abstracts[word] = yacymodel.EncodeSearchIndexAbstract(candidateHashes(matched))
	}

	return contracts.SearchResult{
		SearchTime: time.Since(start),
		WordCounts: wordCounts,
		Abstracts:  abstracts,
	}, nil
}

func (s Searcher) searchAbstracts(
	ctx context.Context,
	query contracts.SearchQuery,
	autoInputs map[yacymodel.Hash]map[yacymodel.Hash]searchCandidate,
) (map[yacymodel.Hash]string, error) {
	switch query.Abstracts.Mode {
	case contracts.SearchAbstractNone:
		return nil, nil
	case contracts.SearchAbstractAuto:
		if len(query.Words) <= 1 || len(query.URLs) != 0 {
			return nil, nil
		}
		word, ok := largestCandidateSet(autoInputs)
		if !ok {
			return nil, nil
		}
		return map[yacymodel.Hash]string{
			word: yacymodel.EncodeSearchIndexAbstract(candidateHashes(autoInputs[word])),
		}, nil
	case contracts.SearchAbstractExplicit:
		postingResult, err := s.rwi.SearchPostings(ctx, ports.PostingSearchQuery{
			WordHashes:   query.Abstracts.Words,
			URLHashes:    query.URLs,
			LimitPerWord: s.postingsPerWord,
			MaxDistance:  query.MaxDistance,
			Language:     query.Filters.Language,

			ContentDomain:    query.Filters.ContentDomain,
			StrictContentDom: query.Filters.StrictContentDom,
			Constraint:       query.Filters.Constraint,
			SiteHash:         query.Filters.SiteHash,
		})
		if err != nil {
			return nil, fmt.Errorf("search postings: %w", err)
		}
		abstracts := make(map[yacymodel.Hash]string, len(query.Abstracts.Words))
		for _, word := range query.Abstracts.Words {
			matched := matchWord(ctx, postingResult.Postings[word])
			abstracts[word] = yacymodel.EncodeSearchIndexAbstract(candidateHashes(matched))
		}
		return abstracts, nil
	default:
		return nil, nil
	}
}

func largestCandidateSet(
	sets map[yacymodel.Hash]map[yacymodel.Hash]searchCandidate,
) (yacymodel.Hash, bool) {
	var (
		selected yacymodel.Hash
		size     int
		ok       bool
	)
	for word, set := range sets {
		if !ok || len(set) > size || len(set) == size && compareAsc(word, selected) < 0 {
			selected = word
			size = len(set)
			ok = true
		}
	}
	return selected, ok
}

func candidateHashes(candidates map[yacymodel.Hash]searchCandidate) []yacymodel.Hash {
	hashes := make([]yacymodel.Hash, 0, len(candidates))
	for hash := range candidates {
		hashes = append(hashes, hash)
	}
	return hashes
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
