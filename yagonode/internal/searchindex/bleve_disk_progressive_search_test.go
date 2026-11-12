package searchindex

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/blevesearch/bleve/v2"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type progressiveShardProbe struct {
	bleveIndexContract
	number int
	calls  *[]int
	err    error
	cancel context.CancelFunc
}

func (probe *progressiveShardProbe) DocCount() (uint64, error) {
	return 1, nil
}

func (probe *progressiveShardProbe) SearchInContext(
	_ context.Context,
	_ *bleve.SearchRequest,
) (*bleve.SearchResult, error) {
	*probe.calls = append(*probe.calls, probe.number)
	if probe.cancel != nil {
		probe.cancel()
	}
	if probe.err != nil {
		return nil, probe.err
	}

	return &bleve.SearchResult{Total: 1}, nil
}

type failingEntropyReader struct{}

func (failingEntropyReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func TestRandomShardOrderShufflesEveryShardOnce(t *testing.T) {
	lastIndex := func(maximum int) (int, error) { return maximum - 1, nil }
	firstIndex := func(int) (int, error) { return 0, nil }
	ordered, err := randomShardOrder(8, lastIndex)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ordered, []int{0, 1, 2, 3, 4, 5, 6, 7}) {
		t.Fatalf("last-index order = %v", ordered)
	}
	shuffled, err := randomShardOrder(8, firstIndex)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(shuffled, ordered) {
		t.Fatalf("injected random values did not alter shard order: %v", shuffled)
	}
	sort.Ints(shuffled)
	if !reflect.DeepEqual(shuffled, ordered) {
		t.Fatalf("shuffled shard coverage = %v", shuffled)
	}
}

func TestRandomShardOrderRejectsInvalidRandomSources(t *testing.T) {
	sentinel := errors.New("random source unavailable")
	for _, test := range []struct {
		name        string
		count       int
		randomBelow func(int) (int, error)
	}{
		{name: "negative shard count", count: -1},
		{name: "random source error", count: 2, randomBelow: func(int) (int, error) {
			return 0, sentinel
		}},
		{name: "out of range", count: 2, randomBelow: func(int) (int, error) {
			return 2, nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := randomShardOrder(test.count, test.randomBelow); err == nil {
				t.Fatal("invalid shard order inputs were accepted")
			} else if test.name == "random source error" && !errors.Is(err, sentinel) {
				t.Fatalf("random error = %v", err)
			}
		})
	}
	if _, err := cryptographicSearchRandomIndex(0); err == nil {
		t.Fatal("zero random range was accepted")
	}
}

func TestCryptographicSearchRandomIndexReturnsInRange(t *testing.T) {
	for range 32 {
		value, err := cryptographicSearchRandomIndex(7)
		if err != nil || value < 0 || value >= 7 {
			t.Fatalf("random index = %d, error = %v", value, err)
		}
	}
}

func TestCryptographicSearchRandomDocumentIDReturnsInRange(t *testing.T) {
	for range 32 {
		value, err := cryptographicSearchRandomDocumentID(10_000)
		if err != nil || value >= 10_000 {
			t.Fatalf("random document ID = %d, error = %v", value, err)
		}
	}
	if _, err := cryptographicSearchRandomDocumentID(0); err == nil {
		t.Fatal("zero document range was accepted")
	}
}

func TestCryptographicSearchRandomHelpersReturnEntropyErrors(t *testing.T) {
	previous := cryptorand.Reader
	cryptorand.Reader = failingEntropyReader{}
	t.Cleanup(func() { cryptorand.Reader = previous })
	if _, err := cryptographicSearchRandomIndex(4); err == nil {
		t.Fatal("shard randomization accepted a failed entropy source")
	}
	if _, err := cryptographicSearchRandomDocumentID(4); err == nil {
		t.Fatal("document sampling accepted a failed entropy source")
	}
}

func TestProgressiveSearchUsesOrdinarySearchForSmallOrFilteredIndexes(t *testing.T) {
	for _, test := range []struct {
		name    string
		request SearchRequest
	}{
		{name: "one shard", request: SearchRequest{Query: "needle", MaxResults: 2}},
		{name: "facets", request: SearchRequest{Query: "needle", MaxResults: 2, WithFacets: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			index := newSingleShardProgressiveIndex(t, false)
			var updates []SearchProgress
			set, err := index.SearchProgressively(
				t.Context(),
				test.request,
				func(progress SearchProgress) {
					updates = append(updates, progress)
				},
			)
			if err != nil || set.CompletedShards != 1 || set.TotalShards != 1 ||
				len(updates) != 1 || !updates[0].Complete {
				t.Fatalf("set=%+v updates=%+v error=%v", set, updates, err)
			}
		})
	}

	index := newSingleShardProgressiveIndex(t, true)
	var sampledUpdates []SearchProgress
	sampled, err := index.SearchProgressively(
		t.Context(), SearchRequest{
			Query: "needle", Terms: []string{"needle"}, MaxResults: 2,
			AllowCandidateSampling: true,
		},
		func(progress SearchProgress) { sampledUpdates = append(sampledUpdates, progress) },
	)
	if err != nil || !sampled.Incomplete || sampled.SampledShards != 1 ||
		len(sampledUpdates) != 1 || sampledUpdates[0].Complete {
		t.Fatalf("sampled fallback=%+v updates=%+v error=%v", sampled, sampledUpdates, err)
	}
}

func newSingleShardProgressiveIndex(t *testing.T, manyDocuments bool) *BleveDiskIndex {
	t.Helper()
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		t.Fatal(err)
	}
	shard, err := newBleveShard(
		filepath.Join(t.TempDir(), "single-progressive.bleve"),
		indexMapping,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shard.Close() })
	documents := []documentstore.Document{{
		NormalizedURL: "https://single.example/needle",
		Title:         "needle result",
		ExtractedText: "needle matching body",
		Language:      "en",
	}}
	if manyDocuments {
		documents = make([]documentstore.Document, diskLexicalCandidateProgressiveWindowDocuments+1)
		for ordinal := range documents {
			documents[ordinal] = documentstore.Document{
				NormalizedURL: fmt.Sprintf("https://single.example/%d", ordinal),
				Title:         "needle result",
				ExtractedText: "needle matching body",
				Language:      "en",
			}
		}
	}
	batch := shard.NewBatch()
	for _, document := range documents {
		indexed, err := bleveDocumentFromStore(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := batch.Index(documentID(document), indexed); err != nil {
			t.Fatal(err)
		}
	}
	if err := shard.Batch(batch); err != nil {
		t.Fatal(err)
	}
	return &BleveDiskIndex{
		shards:           []bleve.Index{shard},
		alias:            newBleveDiskSearchAlias([]bleve.Index{shard}),
		documents:        newFakeDocumentDirectory(documents...),
		multilingual:     supportsMultilingualAnalyzers(shard),
		analyzerScope:    supportsAnalyzerScope(shard),
		storedCandidates: supportsStoredCandidateProjection(shard),
	}
}

func TestProgressiveSearchReturnsRandomizationAndCanceledShardErrors(t *testing.T) {
	sentinel := errors.New("random order unavailable")
	index, calls := progressiveDiskIndex(2, -1, nil, func(int) (int, error) {
		return 0, sentinel
	})
	set, err := index.SearchProgressively(
		t.Context(), SearchRequest{Query: "needle", MaxResults: 1}, nil,
	)
	if !errors.Is(err, sentinel) || !set.Incomplete || len(*calls) != 0 {
		t.Fatalf("random failure result=%+v calls=%v error=%v", set, *calls, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	shardFailure := errors.New("shard failed while canceling")
	index, calls = progressiveDiskIndex(2, 0, shardFailure, func(int) (int, error) {
		return 0, nil
	})
	index.shards[0].(*progressiveShardProbe).cancel = cancel
	set, err = index.SearchProgressively(
		ctx, SearchRequest{Query: "needle", MaxResults: 1}, nil,
	)
	if !errors.Is(err, context.Canceled) ||
		!set.Incomplete || set.CompletedShards != 1 || len(*calls) != 2 {
		t.Fatalf("canceled shard result=%+v calls=%v error=%v", set, *calls, err)
	}
}

func TestProgressiveDiskSearchReportsCompletedShards(t *testing.T) {
	index, calls := progressiveDiskIndex(8, -1, nil, func(int) (int, error) {
		return 0, nil
	})
	var updates []SearchProgress
	set, err := index.SearchProgressively(
		t.Context(), SearchRequest{Query: "needle", MaxResults: 4},
		func(progress SearchProgress) { updates = append(updates, progress) },
	)
	if err != nil {
		t.Fatal(err)
	}
	if set.Incomplete || set.CompletedShards != 8 || set.TotalShards != 8 || set.Total != 8 {
		t.Fatalf("complete progressive result = %+v", set)
	}
	if len(updates) != 8 || len(*calls) != 8 {
		t.Fatalf("updates=%d shard calls=%v", len(updates), *calls)
	}
	if updates[0].CompletedShards != 1 || updates[0].Complete ||
		updates[7].CompletedShards != 8 || !updates[7].Complete {
		t.Fatalf("progress updates = %+v", updates)
	}
	if reflect.DeepEqual(*calls, []int{0, 1, 2, 3, 4, 5, 6, 7}) {
		t.Fatalf("fixed shard order was retained: %v", *calls)
	}
}

func TestProgressiveDiskSearchReportsSampledShardCoverage(t *testing.T) {
	index := newSampledProgressiveDiskIndex(t)
	var updates []SearchProgress
	result, err := index.SearchProgressively(
		t.Context(), SearchRequest{
			Query: "needle", Terms: []string{"needle"}, MaxResults: 10,
		},
		func(progress SearchProgress) { updates = append(updates, progress) },
	)
	if err != nil {
		t.Fatalf("sampled progressive search returned an error: %v", err)
	}
	if !result.Incomplete || result.CompletedShards != diskShardCount ||
		result.SampledShards != diskShardCount || result.TotalShards != diskShardCount {
		t.Fatalf("sampled progressive result = %+v", result)
	}
	if len(updates) != diskShardCount ||
		updates[0].SampledShards != 1 ||
		updates[len(updates)-1].SampledShards != diskShardCount {
		t.Fatalf("sampled progress updates = %+v", updates)
	}
}

func sampledProgressiveDocuments() []documentstore.Document {
	shards := make([]bleve.Index, diskShardCount)
	documents := make([]documentstore.Document, 0, diskShardCount*300)
	perShard := make([]int, diskShardCount)
	for ordinal := 0; len(documents) < diskShardCount*300; ordinal++ {
		url := fmt.Sprintf("https://sample-%d.example/page", ordinal)
		shard := diskShardNumber(shards, url)
		if perShard[shard] == 300 {
			continue
		}
		perShard[shard]++
		documents = append(documents, documentstore.Document{
			NormalizedURL: url,
			Title:         "needle result",
			ExtractedText: "needle matching body",
			Language:      "en",
		})
	}

	return documents
}

func newSampledProgressiveDiskIndex(t *testing.T) *BleveDiskIndex {
	t.Helper()
	documents := sampledProgressiveDocuments()
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		t.Fatal(err)
	}
	indexes := make([]bleve.Index, diskShardCount)
	t.Cleanup(func() { closeBleveShards(indexes) })
	for shardNumber := range indexes {
		shard, err := newBleveShard(
			filepath.Join(t.TempDir(), fmt.Sprintf("progressive-%d.bleve", shardNumber)),
			indexMapping,
		)
		if err != nil {
			t.Fatal(err)
		}
		indexes[shardNumber] = shard
		batch := shard.NewBatch()
		for _, document := range documents {
			if diskShardNumber(indexes, document.NormalizedURL) != shardNumber {
				continue
			}
			indexed, err := bleveDocumentFromStore(document)
			if err != nil {
				t.Fatal(err)
			}
			if err := batch.Index(documentID(document), indexed); err != nil {
				t.Fatal(err)
			}
		}
		if err := shard.Batch(batch); err != nil {
			t.Fatal(err)
		}
	}
	index := &BleveDiskIndex{
		shards:           indexes,
		alias:            newBleveDiskSearchAlias(indexes),
		documents:        newFakeDocumentDirectory(documents...),
		multilingual:     supportsMultilingualAnalyzers(indexes[0]),
		analyzerScope:    supportsAnalyzerScope(indexes[0]),
		storedCandidates: supportsStoredCandidateProjection(indexes[0]),
	}

	return index
}

func TestProgressiveDiskSearchReturnsCompletedShardWorkAtDeadline(t *testing.T) {
	index, calls := progressiveDiskIndex(8, -1, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	var updates int
	set, err := index.SearchProgressively(
		ctx, SearchRequest{Query: "needle", MaxResults: 4},
		func(progress SearchProgress) {
			updates++
			if progress.CompletedShards == 1 {
				cancel()
			}
		},
	)
	if !errors.Is(err, context.Canceled) || !set.Incomplete ||
		set.CompletedShards != 1 || set.TotalShards != 8 || set.Total != 1 {
		t.Fatalf("partial progressive result=%+v error=%v", set, err)
	}
	if updates != 1 || len(*calls) != 1 {
		t.Fatalf("updates=%d shard calls=%v", updates, *calls)
	}
}

func TestProgressiveDiskSearchKeepsHealthyShardsAfterOneShardFailure(t *testing.T) {
	sentinel := errors.New("shard read failed")
	index, calls := progressiveDiskIndex(8, 1, sentinel, nil)
	set, err := index.SearchProgressively(
		t.Context(), SearchRequest{Query: "needle", MaxResults: 4}, nil,
	)
	if !errors.Is(err, sentinel) || !set.Incomplete ||
		set.CompletedShards != 7 || set.TotalShards != 8 || set.Total != 7 {
		t.Fatalf("failed-shard result=%+v error=%v", set, err)
	}
	if len(*calls) != 8 {
		t.Fatalf("shard calls = %v", *calls)
	}
}

func TestProgressiveDiskSearchRejectsClosedAndCanceledIndexes(t *testing.T) {
	closed, _ := progressiveDiskIndex(2, -1, nil, nil)
	closed.closed = true
	if _, err := closed.SearchProgressively(
		t.Context(), SearchRequest{Query: "needle", MaxResults: 1}, nil,
	); err == nil {
		t.Fatal("closed index was accepted")
	}

	index, calls := progressiveDiskIndex(2, -1, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	set, err := index.SearchProgressively(
		ctx, SearchRequest{Query: "needle", MaxResults: 1}, nil,
	)
	if !errors.Is(err, context.Canceled) || !set.Incomplete || len(*calls) != 0 {
		t.Fatalf("canceled search result=%+v calls=%v error=%v", set, *calls, err)
	}
}

func TestProgressiveShardMergeUsesRanksAndDeduplicates(t *testing.T) {
	merged := mergeProgressiveShardResults(
		[]SearchResult{{DocumentID: "same", Score: 0.01}},
		[]SearchResult{
			{DocumentID: "same", Score: 20},
			{DocumentID: "other", Score: 10},
		},
		2,
	)
	if len(merged) != 2 || merged[0].DocumentID != "same" ||
		merged[1].DocumentID != "other" || merged[0].Score <= merged[1].Score {
		t.Fatalf("merged results = %+v", merged)
	}
	if mergeProgressiveShardResults(nil, []SearchResult{{DocumentID: "x"}}, 0) != nil {
		t.Fatal("zero-sized result window was not empty")
	}
	continued := mergeProgressiveShardResults(
		[]SearchResult{{DocumentID: "shared", Score: 1}},
		[]SearchResult{
			{DocumentID: "shared", Score: 10},
			{DocumentID: "extra-a", Score: 9},
			{DocumentID: "extra-b", Score: 8},
		},
		4,
	)
	if len(continued) != 3 {
		t.Fatalf("deduplicated merge=%+v", continued)
	}
}

func TestSampledProgressiveSearchResultRequiresEveryShard(t *testing.T) {
	sampled := SearchResultSet{CompletedShards: 4, SampledShards: 2, TotalShards: 4}
	if result, err := sampledProgressiveSearchResult(
		sampled,
	); err != nil ||
		!reflect.DeepEqual(result, sampled) {
		t.Fatalf("sampled result=%+v error=%v", result, err)
	}
	incomplete := SearchResultSet{CompletedShards: 3, SampledShards: 1, TotalShards: 4}
	if _, err := sampledProgressiveSearchResult(incomplete); err == nil {
		t.Fatal("incomplete shard walk was accepted as a complete sampled search")
	}
	if _, err := sampledProgressiveSearchResult(SearchResultSet{SampledShards: 1}); err == nil {
		t.Fatal("sampled result without a shard count was accepted")
	}
}

func progressiveDiskIndex(
	shardCount int,
	failingShard int,
	failure error,
	randomBelow func(int) (int, error),
) (*BleveDiskIndex, *[]int) {
	calls := &[]int{}
	shards := make([]bleve.Index, shardCount)
	for number := range shards {
		var shardError error
		if number == failingShard {
			shardError = failure
		}
		shards[number] = &progressiveShardProbe{
			number: number, calls: calls, err: shardError,
		}
	}
	return &BleveDiskIndex{
		shards:        shards,
		alias:         newBleveDiskSearchAlias(shards),
		analyzerScope: false,
		randomIndex:   randomBelow,
	}, calls
}
