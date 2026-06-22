package search

import (
	"context"
	"fmt"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/internal/urlmeta"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type searcher struct {
	index           rwi.PostingScanner
	urls            urlmeta.URLDirectory
	postingsPerWord int
}

func (s searcher) Search(ctx context.Context, query Query) (Result, error) {
	start := time.Now()
	if len(query.Words) == 0 && query.Abstracts.Mode == AbstractExplicit {
		return s.abstractCounts(ctx, query, start)
	}

	scanQuery, err := s.postingQuery(query, query.Words, query.Exclude)
	if err != nil {
		return Result{}, err
	}
	scanned, err := scanPostings(ctx, s.index, scanQuery)
	if err != nil {
		return Result{}, err
	}

	var wordCounts map[yacymodel.Hash]int
	if query.Abstracts.Mode != AbstractNone {
		wordCounts = make(map[yacymodel.Hash]int, len(query.Words))
	}
	abstractInputs := map[yacymodel.Hash]map[yacymodel.Hash]candidate{}

	var joined map[yacymodel.Hash]candidate
	for _, word := range query.Words {
		matched := matchWord(ctx, scanned.postings[word])
		if wordCounts != nil {
			wordCounts[word] = scanned.counts[word]
		}
		if query.Abstracts.Mode == AbstractAuto {
			abstractInputs[word] = cloneCandidates(matched)
		}
		joined = intersect(joined, matched)
	}

	joinCount := len(joined)
	ordered := truncate(rankedHashes(joined), query.MaxResults)

	rows, err := s.urls.RowsByHash(ctx, ordered)
	if err != nil {
		return Result{}, fmt.Errorf("rows by hash: %w", err)
	}
	abstracts, err := s.abstracts(ctx, query, abstractInputs)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Resources:  rows,
		JoinCount:  joinCount,
		SearchTime: time.Since(start),
		WordCounts: wordCounts,
		Abstracts:  abstracts,
	}, nil
}

func (s searcher) postingQuery(
	query Query,
	words, exclude []yacymodel.Hash,
) (postingQuery, error) {
	siteHash, err := query.joinSiteHash()
	if err != nil {
		return postingQuery{}, err
	}

	return postingQuery{
		wordHashes:       words,
		excludeHashes:    exclude,
		urlHashes:        query.URLs,
		limitPerWord:     s.postingsPerWord,
		maxDistance:      query.MaxDistance,
		language:         query.joinLanguage(),
		contentDomain:    query.Filters.ContentDomain,
		strictContentDom: query.Filters.StrictContentDom,
		constraint:       query.Filters.Constraint,
		siteHash:         siteHash,
	}, nil
}

func (s searcher) abstractCounts(
	ctx context.Context,
	query Query,
	start time.Time,
) (Result, error) {
	scanQuery, err := s.postingQuery(query, query.Abstracts.Words, nil)
	if err != nil {
		return Result{}, err
	}
	scanned, err := scanPostings(ctx, s.index, scanQuery)
	if err != nil {
		return Result{}, err
	}

	wordCounts := make(map[yacymodel.Hash]int, len(query.Abstracts.Words))
	abstracts := make(map[yacymodel.Hash]string, len(query.Abstracts.Words))
	for _, word := range query.Abstracts.Words {
		matched := matchWord(ctx, scanned.postings[word])
		wordCounts[word] = scanned.counts[word]
		abstracts[word] = yacymodel.EncodeSearchIndexAbstract(candidateHashes(matched))
	}

	return Result{
		SearchTime: time.Since(start),
		WordCounts: wordCounts,
		Abstracts:  abstracts,
	}, nil
}

func (s searcher) abstracts(
	ctx context.Context,
	query Query,
	autoInputs map[yacymodel.Hash]map[yacymodel.Hash]candidate,
) (map[yacymodel.Hash]string, error) {
	switch query.Abstracts.Mode {
	case AbstractNone:
		return nil, nil
	case AbstractAuto:
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
	case AbstractExplicit:
		scanQuery, err := s.postingQuery(query, query.Abstracts.Words, nil)
		if err != nil {
			return nil, err
		}
		scanned, err := scanPostings(ctx, s.index, scanQuery)
		if err != nil {
			return nil, err
		}
		abstracts := make(map[yacymodel.Hash]string, len(query.Abstracts.Words))
		for _, word := range query.Abstracts.Words {
			matched := matchWord(ctx, scanned.postings[word])
			abstracts[word] = yacymodel.EncodeSearchIndexAbstract(candidateHashes(matched))
		}

		return abstracts, nil
	default:
		return nil, nil
	}
}
