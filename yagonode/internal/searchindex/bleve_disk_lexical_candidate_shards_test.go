package searchindex

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

type lexicalCandidateShardNameProbe struct {
	bleveIndexContract
	name string
}

func (probe lexicalCandidateShardNameProbe) Name() string {
	return probe.name
}

type lexicalCandidateIndexReaderProbe struct {
	bleveindex.IndexReader
	identities [][]string
	err        error
}

func (probe *lexicalCandidateIndexReaderProbe) DocIDReaderOnly(
	identities []string,
) (bleveindex.DocIDReader, error) {
	probe.identities = append(probe.identities, slices.Clone(identities))
	if probe.err != nil {
		return nil, probe.err
	}

	return lexicalCandidateDocIDReaderProbe{}, nil
}

type lexicalCandidateDocIDReaderProbe struct{}

func (lexicalCandidateDocIDReaderProbe) Next() (bleveindex.IndexInternalID, error) {
	return nil, nil
}

func (lexicalCandidateDocIDReaderProbe) Advance(
	bleveindex.IndexInternalID,
) (bleveindex.IndexInternalID, error) {
	return nil, nil
}

func (lexicalCandidateDocIDReaderProbe) Size() int {
	return 0
}

func (lexicalCandidateDocIDReaderProbe) Close() error {
	return nil
}

func TestLexicalCandidateShardIdentitiesRequireExactOwnership(t *testing.T) {
	first := lexicalCandidateShardNameProbe{name: "first"}
	second := lexicalCandidateShardNameProbe{name: "second"}
	identities, recognized := lexicalCandidateShardIdentities(
		[]bleve.Index{first, second},
		lexicalCandidateHits(
			[2]string{"first", "one"},
			[2]string{"second", "two"},
			[2]string{"first", "three"},
		),
	)
	if !recognized || len(identities) != 2 ||
		!slices.Equal(identities[0], []string{"one", "three"}) ||
		!slices.Equal(identities[1], []string{"two"}) {
		t.Fatalf("shard identities = %v/%t", identities, recognized)
	}

	one, recognized := lexicalCandidateShardIdentities(
		[]bleve.Index{first},
		lexicalCandidateHits([2]string{"first", "one"}),
	)
	if !recognized || len(one) != 1 || !slices.Equal(one[0], []string{"one"}) {
		t.Fatalf("single shard identities = %v/%t", one, recognized)
	}

	cases := map[string]struct {
		shards     []bleve.Index
		candidates search.DocumentMatchCollection
	}{
		"no shards": {candidates: lexicalCandidateHits([2]string{"first", "one"})},
		"nil shard": {
			shards:     []bleve.Index{nil},
			candidates: lexicalCandidateHits([2]string{"first", "one"}),
		},
		"empty shard name": {
			shards:     []bleve.Index{lexicalCandidateShardNameProbe{}},
			candidates: lexicalCandidateHits([2]string{"", "one"}),
		},
		"duplicate shard name": {
			shards:     []bleve.Index{first, first},
			candidates: lexicalCandidateHits([2]string{"first", "one"}),
		},
		"nil candidate": {
			shards:     []bleve.Index{first},
			candidates: search.DocumentMatchCollection{nil},
		},
		"unknown candidate shard": {
			shards:     []bleve.Index{first},
			candidates: lexicalCandidateHits([2]string{"missing", "one"}),
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if identities, recognized := lexicalCandidateShardIdentities(
				test.shards,
				test.candidates,
			); recognized || identities != nil {
				t.Fatalf("shard identities = %v/%t", identities, recognized)
			}
		})
	}
}

func TestBleveLexicalCandidateIdentityQuerySelectsOwnedIdentities(t *testing.T) {
	shards := []bleve.Index{
		lexicalCandidateShardNameProbe{name: "first"},
		lexicalCandidateShardNameProbe{name: "second"},
	}
	candidates := lexicalCandidateHits(
		[2]string{"first", "one"},
		[2]string{"second", "two"},
		[2]string{"first", "three"},
	)
	query, ok := bleveLexicalCandidateIdentityQuery(
		shards,
		candidates,
	).(*bleveLexicalCandidateShardQuery)
	if !ok {
		t.Fatalf("identity query = %T", query)
	}

	reader := &lexicalCandidateIndexReaderProbe{}
	contexts := []context.Context{
		context.WithValue(t.Context(), bleveDiskSearchShardPosition{}, 0),
		context.WithValue(t.Context(), bleveDiskSearchShardPosition{}, 1),
		t.Context(),
		context.WithValue(t.Context(), bleveDiskSearchShardPosition{}, -1),
		context.WithValue(t.Context(), bleveDiskSearchShardPosition{}, 2),
	}
	for _, ctx := range contexts {
		candidateSearcher, err := query.Searcher(
			ctx,
			reader,
			mapping.NewIndexMapping(),
			search.SearcherOptions{},
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := candidateSearcher.Close(); err != nil {
			t.Fatal(err)
		}
	}
	want := [][]string{
		{"one", "three"},
		{"two"},
		{"one", "two", "three"},
		{"one", "two", "three"},
		{"one", "two", "three"},
	}
	if !slices.EqualFunc(reader.identities, want, slices.Equal[[]string]) {
		t.Fatalf("selected identities = %v, want %v", reader.identities, want)
	}

	query.SetField("ignored")
	if query.Field() != "_id" {
		t.Fatalf("identity field = %q", query.Field())
	}
	fields, err := blevequery.ExtractFields(query, mapping.NewIndexMapping(), nil)
	if err != nil || !fields.HasID() {
		t.Fatalf("identity fields = %v, error = %v", fields, err)
	}

	sentinel := errors.New("identity reader failed")
	reader.err = sentinel
	if _, err := query.Searcher(
		t.Context(),
		reader,
		mapping.NewIndexMapping(),
		search.SearcherOptions{},
	); !errors.Is(err, sentinel) {
		t.Fatalf("identity reader error = %v", err)
	}
}

func TestBleveLexicalCandidateIdentityQueryFallsBackWithoutOwnership(t *testing.T) {
	candidates := lexicalCandidateHits(
		[2]string{"missing", "one"},
		[2]string{"missing", "two"},
	)
	query, ok := bleveLexicalCandidateIdentityQuery(
		[]bleve.Index{lexicalCandidateShardNameProbe{name: "first"}},
		candidates,
	).(*blevequery.DocIDQuery)
	if !ok || !slices.Equal(query.IDs, []string{"one", "two"}) || query.Boost() != 0 {
		t.Fatalf("fallback identity query = %#v", query)
	}
}

func lexicalCandidateHits(
	values ...[2]string,
) search.DocumentMatchCollection {
	hits := make(search.DocumentMatchCollection, len(values))
	for position, value := range values {
		hits[position] = &search.DocumentMatch{Index: value[0], ID: value[1]}
	}

	return hits
}
