package searchindex

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync/atomic"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	"github.com/blevesearch/bleve/v2/search/scorer"
	"github.com/blevesearch/bleve/v2/search/searcher"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

const diskLexicalCandidateMaximumDocumentsPerShard = diskLexicalCandidateMaximumDocuments / diskShardCount

const diskLexicalCandidateProgressiveWindowDocuments = 256

type bleveLexicalCandidateSnapshotQuery struct {
	candidate        blevequery.Query
	maximumDocuments int
	allowIncomplete  bool
	incomplete       *atomic.Bool
	randomStart      func(uint64) (uint64, error)
}

type bleveLexicalCandidateWindow struct {
	maximumDocuments int
	startID          bleveindex.IndexInternalID
	forceIncomplete  bool
}

func newBleveLexicalCandidateSnapshotQuery(
	req SearchRequest,
	multilingual bool,
	allowIncomplete bool,
	incomplete *atomic.Bool,
) blevequery.Query {
	maximumDocuments := diskLexicalCandidateMaximumDocumentsPerShard
	if allowIncomplete {
		maximumDocuments = diskLexicalCandidateProgressiveWindowDocuments
	}

	return &bleveLexicalCandidateSnapshotQuery{
		candidate:        bleveLexicalCandidateQuery(req, multilingual),
		maximumDocuments: maximumDocuments,
		allowIncomplete:  allowIncomplete,
		incomplete:       incomplete,
		randomStart:      cryptographicSearchRandomDocumentID,
	}
}

func (query *bleveLexicalCandidateSnapshotQuery) Searcher(
	ctx context.Context,
	reader bleveindex.IndexReader,
	indexMapping mapping.IndexMapping,
	options search.SearcherOptions,
) (search.Searcher, error) {
	window, err := query.progressiveWindow(reader)
	if err != nil {
		return nil, err
	}
	identities, complete, err := collectBleveLexicalCandidateSnapshotFrom(
		ctx,
		reader,
		indexMapping,
		query.candidate,
		window,
	)
	if err != nil {
		return nil, err
	}
	if !complete {
		return query.incompleteSearcher(ctx, reader, identities, options)
	}

	return newBleveLexicalCandidateInternalSearcher(identities, options), nil
}

func (query *bleveLexicalCandidateSnapshotQuery) progressiveWindow(
	reader bleveindex.IndexReader,
) (bleveLexicalCandidateWindow, error) {
	window := bleveLexicalCandidateWindow{maximumDocuments: query.maximumDocuments}
	if !query.allowIncomplete {
		return window, nil
	}
	documentCount, err := reader.DocCount()
	if err != nil {
		return bleveLexicalCandidateWindow{}, fmt.Errorf(
			"count documents for progressive sample: %w",
			err,
		)
	}
	if window.maximumDocuments <= 0 {
		return bleveLexicalCandidateWindow{}, fmt.Errorf(
			"progressive sample maximum must be positive",
		)
	}
	if documentCount <= uint64(window.maximumDocuments) {
		return window, nil
	}
	randomStart := query.randomStart
	if randomStart == nil {
		randomStart = cryptographicSearchRandomDocumentID
	}
	value, err := randomStart(documentCount)
	if err != nil {
		return bleveLexicalCandidateWindow{}, fmt.Errorf(
			"choose progressive sample position: %w",
			err,
		)
	}
	if value >= documentCount {
		return bleveLexicalCandidateWindow{}, fmt.Errorf(
			"progressive sample position outside document range",
		)
	}
	window.startID = bleveindex.NewIndexInternalID(nil, value)
	window.forceIncomplete = true

	return window, nil
}

func (query *bleveLexicalCandidateSnapshotQuery) incompleteSearcher(
	ctx context.Context,
	reader bleveindex.IndexReader,
	identities []bleveindex.IndexInternalID,
	options search.SearcherOptions,
) (search.Searcher, error) {
	if query.allowIncomplete {
		if query.incomplete != nil {
			query.incomplete.Store(true)
		}

		return newBleveLexicalCandidateInternalSearcher(identities, options), nil
	}
	allDocuments, err := searcher.NewMatchAllSearcher(ctx, reader, 0, options)
	if err != nil {
		return nil, fmt.Errorf("retain complete lexical search: %w", err)
	}

	return allDocuments, nil
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
	return collectBleveLexicalCandidateSnapshotFrom(
		ctx,
		reader,
		indexMapping,
		query,
		bleveLexicalCandidateWindow{maximumDocuments: maximumDocuments},
	)
}

func collectBleveLexicalCandidateSnapshotFrom(
	ctx context.Context,
	reader bleveindex.IndexReader,
	indexMapping mapping.IndexMapping,
	query blevequery.Query,
	window bleveLexicalCandidateWindow,
) ([]bleveindex.IndexInternalID, bool, error) {
	if query == nil {
		return nil, false, fmt.Errorf("lexical candidate query required")
	}
	if window.maximumDocuments <= 0 {
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
		window.maximumDocuments,
		window.startID,
	)
	closeErr := candidates.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close lexical candidate snapshot: %w", closeErr)
	}
	if collectionErr != nil || closeErr != nil {
		return nil, false, errors.Join(collectionErr, closeErr)
	}
	sort.Slice(identities, func(first, second int) bool {
		return identities[first].Compare(identities[second]) < 0
	})
	if len(identities) > window.maximumDocuments {
		return identities[:window.maximumDocuments], false, nil
	}
	if window.forceIncomplete {
		return identities, false, nil
	}

	return identities, true, nil
}

func readBleveLexicalCandidateSnapshot(
	ctx context.Context,
	reader bleveindex.IndexReader,
	candidates search.Searcher,
	maximumDocuments int,
	startID bleveindex.IndexInternalID,
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
	var first *search.DocumentMatch
	if len(startID) > 0 {
		match, err := candidates.Advance(candidateContext, startID)
		if err != nil {
			collectionErr = fmt.Errorf(
				"seek progressive lexical candidate window: %w",
				bleveSearchOperationError(ctx, err),
			)
		} else {
			first = match
		}
	}
	for len(identities) <= maximumDocuments && collectionErr == nil {
		if err := ctx.Err(); err != nil {
			collectionErr = fmt.Errorf(
				"collect lexical candidate snapshot: %w",
				context.Cause(ctx),
			)

			break
		}
		match := first
		first = nil
		if match == nil {
			var err error
			match, err = candidates.Next(candidateContext)
			if err != nil {
				collectionErr = fmt.Errorf(
					"collect lexical candidate snapshot: %w",
					bleveSearchOperationError(ctx, err),
				)

				break
			}
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
	if first != nil {
		candidateContext.DocumentMatchPool.Put(first)
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
