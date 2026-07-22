package documentsearch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func metadataConstraintRow(
	t *testing.T,
	identifier yagomodel.Hash,
	rawURL string,
	author string,
) yagomodel.URIMetadataRow {
	t.Helper()
	row := metadataRow(t, identifier, rawURL, "Title")
	if author != "" {
		row.Properties[yagomodel.URLMetaAuthor] = yagomodel.EncodeCompactWireForm(author)
	}

	return row
}

func TestMetadataConstraintsMatchSupportedFields(t *testing.T) {
	row := metadataConstraintRow(
		t,
		hashFor("doc"),
		"https://example.org/reports/plan.PDF",
		"Ada Lovelace and Grace Hopper",
	)
	tests := []struct {
		name string
		req  yagoproto.SearchRequest
		want bool
	}{
		{name: "author", req: yagoproto.SearchRequest{Author: "GRACE hopper"}, want: true},
		{name: "author absent", req: yagoproto.SearchRequest{Author: "Turing"}},
		{
			name: "url full match",
			req:  yagoproto.SearchRequest{Filter: `https://example\.org/.*\.PDF`},
			want: true,
		},
		{name: "url substring rejected", req: yagoproto.SearchRequest{Filter: `example\.org`}},
		{name: "file type", req: yagoproto.SearchRequest{FileType: ".pdf"}, want: true},
		{name: "file type absent", req: yagoproto.SearchRequest{FileType: "html"}},
		{name: "protocol", req: yagoproto.SearchRequest{Protocol: "HTTPS"}, want: true},
		{name: "protocol absent", req: yagoproto.SearchRequest{Protocol: "ftp"}},
		{name: "default filter", req: yagoproto.SearchRequest{Filter: ".*"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			constraints, err := metadataConstraintsFromRequest(test.req, queryOperators{})
			if err != nil {
				t.Fatal(err)
			}
			if got := constraints.matches(t.Context(), row); got != test.want {
				t.Fatalf("matches = %t, want %t", got, test.want)
			}
		})
	}
}

func TestMetadataConstraintsUseModifierOperators(t *testing.T) {
	operators := parseQueryOperators(
		"/language/DE site:example.org author:(Ada Lovelace) filetype:PDF /HTTPS",
	)
	if operators.Language != "de" ||
		operators.SiteHost != "example.org" ||
		operators.Author != "Ada Lovelace" ||
		operators.FileType != "PDF" ||
		operators.Protocol != "https" {
		t.Fatalf("operators = %#v", operators)
	}
	row := metadataConstraintRow(
		t,
		hashFor("doc"),
		"https://example.org/report.pdf",
		"Ada Lovelace",
	)
	constraints, err := metadataConstraintsFromRequest(yagoproto.SearchRequest{
		Author:   "ignored",
		FileType: "txt",
		Protocol: "ftp",
	}, operators)
	if err != nil {
		t.Fatal(err)
	}
	if !constraints.matches(t.Context(), row) {
		t.Fatalf("modifier constraints did not match %#v", constraints)
	}

	unparenthesized := parseQueryOperators("author:Hopper")
	if unparenthesized.Author != "Hopper" {
		t.Fatalf("author = %q, want Hopper", unparenthesized.Author)
	}
}

func TestMetadataConstraintsRejectInvalidOrOversizedFilter(t *testing.T) {
	for _, filter := range []string{"[", strings.Repeat("x", maximumURLFilterBytes+1)} {
		if _, err := metadataConstraintsFromRequest(
			yagoproto.SearchRequest{Filter: filter},
			queryOperators{},
		); err == nil {
			t.Fatalf("filter %q accepted", filter[:1])
		}
	}
}

func TestMetadataConstraintsRejectMissingOrInvalidMetadata(t *testing.T) {
	constraints, err := metadataConstraintsFromRequest(
		yagoproto.SearchRequest{Author: "Ada"},
		queryOperators{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if constraints.matches(t.Context(), yagomodel.URIMetadataRow{Properties: map[string]string{}}) {
		t.Fatal("missing author matched")
	}
	row := metadataConstraintRow(t, hashFor("doc"), "https://example.org/a", "Ada")
	row.Properties[yagomodel.URLMetaAuthor] = "q|invalid"
	if constraints.matches(t.Context(), row) {
		t.Fatal("invalid author encoding matched")
	}

	constraints, err = metadataConstraintsFromRequest(
		yagoproto.SearchRequest{Protocol: "https"},
		queryOperators{},
	)
	if err != nil {
		t.Fatal(err)
	}
	row.Properties[yagomodel.URLMetaURL] = "%"
	if constraints.matches(t.Context(), row) {
		t.Fatal("invalid url matched")
	}
}

func TestMetadataFiltersRunBeforeTopK(t *testing.T) {
	word := hashFor("word")
	first := hashFor("first")
	second := hashFor("second")
	directory := fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
		first: metadataConstraintRow(
			t,
			first,
			"https://example.org/first.pdf",
			"Other Author",
		),
		second: metadataConstraintRow(
			t,
			second,
			"https://example.org/second.pdf",
			"Wanted Author",
		),
	}}
	result, err := (searcher{
		index: fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
			word: {
				postingEntry(word, "first", 0, 10),
				postingEntry(word, "second", 0, 1),
			},
		}},
		documents:      directory,
		matchesPerTerm: 1000,
	}).search(t.Context(), searchCriteria{
		terms:      []yagomodel.Hash{word},
		maxResults: 1,
		metadata:   metadataConstraints{author: "wanted"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.totalDocumentsMatchingEveryTerm != 1 || len(result.resources) != 1 {
		t.Fatalf("result = %#v", result)
	}
	identifier, err := result.resources[0].URLHash()
	if err != nil || identifier.Hash() != second {
		t.Fatalf("resource = %#v error = %v", result.resources[0], err)
	}
}

func TestMetadataQualificationPropagatesDirectoryFailure(t *testing.T) {
	sentinel := errors.New("directory failed")
	_, err := (searcher{documents: fakeDirectory{err: sentinel}}).qualifyDocuments(
		t.Context(),
		map[yagomodel.Hash]matchedDocument{hashFor("doc"): {}},
		metadataConstraints{author: "author"},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

type unrelatedMetadataDirectory struct {
	fakeDirectory
	row yagomodel.URIMetadataRow
}

func (d unrelatedMetadataDirectory) RowsByHash(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.URIMetadataRow, error) {
	return []yagomodel.URIMetadataRow{d.row}, nil
}

func TestMetadataQualificationRejectsUnrequestedRows(t *testing.T) {
	requested := hashFor("requested")
	unrelated := hashFor("unrelated")
	qualified, err := (searcher{documents: unrelatedMetadataDirectory{
		row: metadataConstraintRow(
			t,
			unrelated,
			"https://example.org/unrelated.pdf",
			"Ada",
		),
	}}).qualifyDocuments(
		t.Context(),
		map[yagomodel.Hash]matchedDocument{requested: {identifier: requested}},
		metadataConstraints{author: "ada"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(qualified.matches) != 0 || len(qualified.resources) != 0 {
		t.Fatalf("qualified = %#v", qualified)
	}
}
