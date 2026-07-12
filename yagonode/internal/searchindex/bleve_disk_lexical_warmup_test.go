package searchindex

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

func TestBleveLexicalWarmupVisitsEverySearchDictionaryPerShard(t *testing.T) {
	firstShard := &warmupBleveIndex{}
	secondShard := &warmupBleveIndex{}
	report := warmBleveLexicalDictionaries(
		t.Context(),
		[]bleve.Index{firstShard, secondShard},
	)

	wantFields := append([]string{documentAnalyzerField}, searchIndexedFields()...)
	if report.attempted != len(wantFields)*2 || report.failures != 0 ||
		report.interruption != nil {
		t.Fatalf("report = %#v", report)
	}
	for shardNumber, shard := range []*warmupBleveIndex{firstShard, secondShard} {
		if !reflect.DeepEqual(shard.fields, wantFields) {
			t.Fatalf("shard %d fields = %#v, want %#v", shardNumber, shard.fields, wantFields)
		}
		for _, dictionary := range shard.dictionaries {
			if !dictionary.cardinalityRead || !dictionary.closed {
				t.Fatalf("shard %d dictionary = %#v", shardNumber, dictionary)
			}
		}
	}
}

func TestBleveLexicalWarmupStopsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	shard := &warmupBleveIndex{cancel: cancel, cancelAfter: 1}
	report := warmBleveLexicalDictionaries(ctx, []bleve.Index{shard, &warmupBleveIndex{}})

	if len(shard.fields) != 1 || len(shard.dictionaries) != 1 {
		t.Fatalf(
			"warmup work after cancellation = fields %#v dictionaries %#v",
			shard.fields,
			shard.dictionaries,
		)
	}
	if report.attempted != 1 || !errors.Is(report.interruption, context.Canceled) {
		t.Fatalf("report = %#v", report)
	}
	if !shard.dictionaries[0].closed {
		t.Fatal("dictionary opened before cancellation was not closed")
	}
}

func TestBleveLexicalWarmupContinuesAfterDictionaryError(t *testing.T) {
	shard := &warmupBleveIndex{
		failedField:       "headings",
		closeFailureField: "body",
	}
	report := warmBleveLexicalDictionaries(t.Context(), []bleve.Index{shard})

	wantFields := append([]string{documentAnalyzerField}, searchIndexedFields()...)
	if !reflect.DeepEqual(shard.fields, wantFields) {
		t.Fatalf("fields = %#v, want %#v", shard.fields, wantFields)
	}
	if len(shard.dictionaries) != len(wantFields)-1 {
		t.Fatalf("opened dictionaries = %d, want %d", len(shard.dictionaries), len(wantFields)-1)
	}
	if report.attempted != len(wantFields) || report.failures != 2 ||
		report.firstFailure.field != "headings" ||
		report.firstFailure.operation != "open" {
		t.Fatalf("report = %#v", report)
	}
}

func TestBleveLexicalWarmupLogsOneAggregateWarning(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	alias := &warmupBleveIndex{searchErr: errors.New("match-all failed")}
	index := &BleveDiskIndex{alias: alias, shards: []bleve.Index{&warmupBleveIndex{
		failedField: "headings",
	}}}
	index.warm(t.Context())

	logOutput := output.String()
	if strings.Count(logOutput, "\n") != 1 ||
		!strings.Contains(logOutput, bleveLexicalWarmupIncompleteMessage) ||
		!strings.Contains(logOutput, `"failures":2`) ||
		!strings.Contains(logOutput, `"field":"_id"`) ||
		!strings.Contains(logOutput, `"operation":"match-all"`) {
		t.Fatalf("warning = %s", logOutput)
	}
}

func TestBleveLexicalWarmupPreservesMatchAllProbe(t *testing.T) {
	alias := &warmupBleveIndex{}
	index := &BleveDiskIndex{alias: alias}
	index.warm(t.Context())

	if alias.searches != 1 || alias.searchRequest == nil || alias.searchRequest.Size != 1 {
		t.Fatalf("match-all probe = searches %d request %#v", alias.searches, alias.searchRequest)
	}
	if _, ok := alias.searchRequest.Query.(*blevequery.MatchAllQuery); !ok {
		t.Fatalf("warmup query = %T", alias.searchRequest.Query)
	}
}

func TestBleveLexicalWarmupHonorsCanceledParentContext(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	alias := &warmupBleveIndex{}
	index := &BleveDiskIndex{alias: alias}
	index.warm(ctx)

	if alias.searches != 0 || !strings.Contains(output.String(), "context canceled") {
		t.Fatalf("canceled warmup = searches %d warning %s", alias.searches, output.String())
	}
}

type warmupBleveIndex struct {
	bleveIndexContract
	fields            []string
	dictionaries      []*warmupFieldDictionary
	failedField       string
	closeFailureField string
	cancel            context.CancelFunc
	cancelAfter       int
	searches          int
	searchRequest     *bleve.SearchRequest
	searchErr         error
}

func (i *warmupBleveIndex) FieldDict(field string) (bleveindex.FieldDict, error) {
	i.fields = append(i.fields, field)
	if i.cancel != nil && len(i.fields) == i.cancelAfter {
		i.cancel()
	}
	if field == i.failedField {
		return nil, errors.New("dictionary failed")
	}
	dictionary := &warmupFieldDictionary{}
	if field == i.closeFailureField {
		dictionary.closeErr = errors.New("dictionary close failed")
	}
	i.dictionaries = append(i.dictionaries, dictionary)

	return dictionary, nil
}

func (i *warmupBleveIndex) SearchInContext(
	_ context.Context,
	request *bleve.SearchRequest,
) (*bleve.SearchResult, error) {
	i.searches++
	i.searchRequest = request

	return nil, i.searchErr
}

type warmupFieldDictionary struct {
	cardinalityRead bool
	closed          bool
	closeErr        error
}

func (d *warmupFieldDictionary) Next() (*bleveindex.DictEntry, error) {
	return nil, nil
}

func (d *warmupFieldDictionary) Close() error {
	d.closed = true

	return d.closeErr
}

func (d *warmupFieldDictionary) Cardinality() int {
	d.cardinalityRead = true

	return 0
}

func (d *warmupFieldDictionary) BytesRead() uint64 {
	return 0
}
