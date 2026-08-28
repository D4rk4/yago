package searchindex

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	"github.com/blevesearch/bleve/v2/search/scorer"
	"github.com/blevesearch/bleve/v2/search/searcher"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

const diskLexicalCandidateMaximumDocumentsPerShard = diskLexicalCandidateMaximumDocuments / diskShardCount

type bleveLexicalCandidateSnapshotQuery struct {
	candidate        blevequery.Query
	maximumDocuments int
}

func newBleveLexicalCandidateSnapshotQuery(
	req SearchRequest,
	multilingual bool,
) blevequery.Query {
	return &bleveLexicalCandidateSnapshotQuery{
		candidate:        bleveLexicalCandidateQuery(req, multilingual),
		maximumDocuments: diskLexicalCandidateMaximumDocumentsPerShard,
	}
}

func (query *bleveLexicalCandidateSnapshotQuery) Searcher(
	ctx context.Context,
	reader bleveindex.IndexReader,
	indexMapping mapping.IndexMapping,
	options search.SearcherOptions,
) (search.Searcher, error) {
	identities, complete, err := collectBleveLexicalCandidateSnapshot(
		ctx,
		reader,
		indexMapping,
		query.candidate,
		query.maximumDocuments,
	)
	if err != nil {
		return nil, err
	}
	if !complete {
		allDocuments, err := searcher.NewMatchAllSearcher(ctx, reader, 0, options)
		if err != nil {
			return nil, fmt.Errorf("retain complete lexical search: %w", err)
		}

		return allDocuments, nil
	}
	if len(identities) == 0 {
		return newBleveLexicalCandidateInternalSearcher(nil, options), nil
	}

	return newBleveLexicalCandidateInternalSearcher(identities, options), nil
}

func (*bleveLexicalCandidateSnapshotQuery) Field() string {
	return "_id"
}

func (*bleveLexicalCandidateSnapshotQuery) SetField(string) {}

func collectBleveLexicalCandidateSnapshot(
	ctx context.Context,
	reader bleveindex.IndexReader,
	indexMapping mapping.IndexMapping,
	query blevequery.Query,
	maximumDocuments int,
) ([]bleveindex.IndexInternalID, bool, error) {
	if query == nil {
		return nil, false, fmt.Errorf("lexical candidate query required")
	}
	if maximumDocuments <= 0 {
		return nil, false, fmt.Errorf("lexical candidate maximum required")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, fmt.Errorf(
			"collect lexical candidate snapshot: %w",
			context.Cause(ctx),
		)
	}
	options := search.SearcherOptions{Score: bleve.ScoreNone}
	candidates, err := query.Searcher(ctx, reader, indexMapping, options)
	if err != nil {
		return nil, false, fmt.Errorf(
			"open lexical candidate snapshot: %w",
			bleveSearchOperationError(ctx, err),
		)
	}
	if candidates == nil {
		return nil, false, fmt.Errorf("open lexical candidate snapshot: searcher unavailable")
	}
	identities, collectionErr := readBleveLexicalCandidateSnapshot(
		ctx,
		reader,
		candidates,
		maximumDocuments,
	)
	closeErr := candidates.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close lexical candidate snapshot: %w", closeErr)
	}
	if collectionErr != nil || closeErr != nil {
		return nil, false, errors.Join(collectionErr, closeErr)
	}
	if len(identities) > maximumDocuments {
		return nil, false, nil
	}
	sort.Slice(identities, func(first, second int) bool {
		return identities[first].Compare(identities[second]) < 0
	})

	return identities, true, nil
}

func readBleveLexicalCandidateSnapshot(
	ctx context.Context,
	reader bleveindex.IndexReader,
	candidates search.Searcher,
	maximumDocuments int,
) ([]bleveindex.IndexInternalID, error) {
	candidateContext := &search.SearchContext{
		DocumentMatchPool: search.NewDocumentMatchPool(
			candidates.DocumentMatchPoolSize(),
			0,
		),
		IndexReader: reader,
	}
	identities := make([]bleveindex.IndexInternalID, 0, maximumDocuments+1)
	var collectionErr error
	for len(identities) <= maximumDocuments {
		if err := ctx.Err(); err != nil {
			collectionErr = fmt.Errorf(
				"collect lexical candidate snapshot: %w",
				context.Cause(ctx),
			)

			break
		}
		match, err := candidates.Next(candidateContext)
		if err != nil {
			collectionErr = fmt.Errorf(
				"collect lexical candidate snapshot: %w",
				bleveSearchOperationError(ctx, err),
			)

			break
		}
		if match == nil {
			break
		}
		if len(match.IndexInternalID) == 0 {
			candidateContext.DocumentMatchPool.Put(match)
			collectionErr = fmt.Errorf("collect lexical candidate snapshot: identity missing")

			break
		}
		identities = append(
			identities,
			append(bleveindex.IndexInternalID(nil), match.IndexInternalID...),
		)
		candidateContext.DocumentMatchPool.Put(match)
	}

	return identities, collectionErr
}

type bleveLexicalCandidateInternalSearcher struct {
	identities []bleveindex.IndexInternalID
	position   int
	scorer     *scorer.ConstantScorer
}

func newBleveLexicalCandidateInternalSearcher(
	identities []bleveindex.IndexInternalID,
	options search.SearcherOptions,
) search.Searcher {
	return &bleveLexicalCandidateInternalSearcher{
		identities: identities,
		scorer:     scorer.NewConstantScorer(1, 0, options),
	}
}

func (candidate *bleveLexicalCandidateInternalSearcher) Next(
	ctx *search.SearchContext,
) (*search.DocumentMatch, error) {
	if candidate.position >= len(candidate.identities) {
		return nil, nil
	}
	identity := candidate.identities[candidate.position]
	candidate.position++

	return candidate.scorer.Score(ctx, identity), nil
}

func (candidate *bleveLexicalCandidateInternalSearcher) Advance(
	ctx *search.SearchContext,
	identity bleveindex.IndexInternalID,
) (*search.DocumentMatch, error) {
	remaining := candidate.identities[candidate.position:]
	candidate.position += sort.Search(len(remaining), func(position int) bool {
		return remaining[position].Compare(identity) >= 0
	})

	return candidate.Next(ctx)
}

func (*bleveLexicalCandidateInternalSearcher) Close() error {
	return nil
}

func (candidate *bleveLexicalCandidateInternalSearcher) Weight() float64 {
	return candidate.scorer.Weight()
}

func (candidate *bleveLexicalCandidateInternalSearcher) SetQueryNorm(queryNorm float64) {
	candidate.scorer.SetQueryNorm(queryNorm)
}

func (candidate *bleveLexicalCandidateInternalSearcher) Count() uint64 {
	return uint64(len(candidate.identities))
}

func (*bleveLexicalCandidateInternalSearcher) Min() int {
	return 0
}

func (candidate *bleveLexicalCandidateInternalSearcher) Size() int {
	bytes := int(reflect.TypeOf(*candidate).Size()) + candidate.scorer.Size()
	bytes += len(candidate.identities) * int(reflect.TypeOf(bleveindex.IndexInternalID(nil)).Size())
	for _, identity := range candidate.identities {
		bytes += len(identity)
	}

	return bytes
}

func (*bleveLexicalCandidateInternalSearcher) DocumentMatchPoolSize() int {
	return 1
}

var (
	_ blevequery.Query          = (*bleveLexicalCandidateSnapshotQuery)(nil)
	_ blevequery.FieldableQuery = (*bleveLexicalCandidateSnapshotQuery)(nil)
	_ search.Searcher           = (*bleveLexicalCandidateInternalSearcher)(nil)
)
