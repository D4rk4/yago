package searchlocal

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/searchsession"
	"github.com/D4rk4/yago/yagonode/internal/searchvisible"
)

type pageEvidenceInner struct {
	response searchcore.Response
	err      error
}

func (p pageEvidenceInner) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	return p.response, p.err
}

type pageEvidenceSource struct {
	req     searchindex.SearchRequest
	results []searchindex.SearchResult
	batches [][]searchindex.SearchResult
	err     error
	pending bool
}

func (p *pageEvidenceSource) SearchEvidence(
	_ context.Context,
	req searchindex.SearchRequest,
	results []searchindex.SearchResult,
) ([]searchindex.SearchResult, error) {
	p.req = req
	p.results = append([]searchindex.SearchResult(nil), results...)
	p.batches = append(p.batches, append([]searchindex.SearchResult(nil), results...))
	if p.err != nil {
		return nil, p.err
	}
	if p.pending {
		return results, nil
	}
	for index := range results {
		results[index].Snippet = "передача полномочий"
		results[index].BodyQueryMatches = []searchindex.TextQueryMatch{{Start: 12, End: 32}}
		results[index].EvidenceReady = true
	}

	return results, nil
}

func TestPageEvidenceEnrichesOnlyPendingLocalRows(t *testing.T) {
	source := &pageEvidenceSource{}
	searcher := NewPageEvidenceSearcher(pageEvidenceInner{response: searchcore.Response{
		Results: []searchcore.Result{
			{
				DocumentID: "pending", Analyzer: "ru", Source: searchcore.SourceGlobal,
				Snippet: "leading",
			},
			{
				DocumentID: "ready", Source: searchcore.SourceLocal,
				Snippet: "ready", EvidenceReady: true, BodyQueryMatches: []searchcore.QueryMatch{},
			},
			{DocumentID: "peer", Source: searchcore.SourceRemote, Snippet: "peer"},
		},
	}}, source)
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "полномочия", Terms: []string{"полномочия"}, Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(source.results) != 1 || source.results[0].DocumentID != "pending" ||
		source.results[0].Analyzer != "ru" || source.req.Query != "полномочия" {
		t.Fatalf("source req=%#v results=%#v", source.req, source.results)
	}
	if response.Results[0].Snippet != "передача полномочий" ||
		!response.Results[0].EvidenceReady || response.Results[1].Snippet != "ready" ||
		response.Results[2].Snippet != "peer" {
		t.Fatalf("response = %#v", response.Results)
	}
	matches := response.Results[0].QueryMatches
	if len(matches) != 1 ||
		response.Results[0].Snippet[matches[0].Start:matches[0].End] != "полномочий" {
		t.Fatalf("query matches = %#v", matches)
	}
	if len(response.Results[0].BodyQueryMatches) != 1 ||
		response.Results[0].BodyQueryMatches[0] != (searchcore.QueryMatch{Start: 12, End: 32}) {
		t.Fatalf("body query matches = %#v", response.Results[0].BodyQueryMatches)
	}
}

func TestPageEvidenceHydratesVisibleOnlyRowsFromLaterStablePage(t *testing.T) {
	results := make([]searchcore.Result, 20)
	for index := range results {
		results[index] = searchcore.Result{
			DocumentID: "document-" + strconv.Itoa(index),
			Source:     searchcore.SourceLocal,
			URL:        "https://example.test/" + strconv.Itoa(index),
			Snippet:    "alpha",
		}
	}
	source := &pageEvidenceSource{}
	stable := searchsession.NewStableWindow(searchvisible.NewVisibleEvidenceSearcher(
		pageEvidenceInner{
			response: searchcore.Response{TotalResults: len(results), Results: results},
		},
	))
	searcher := NewPageEvidenceSearcher(stable, source)
	request := searchcore.Request{Query: "alpha", Terms: []string{"alpha"}, Limit: 10}
	first, err := searcher.Search(t.Context(), request)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	request.Offset = 10
	second, err := searcher.Search(t.Context(), request)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(source.batches) != 2 || len(source.batches[0]) != 10 || len(source.batches[1]) != 10 ||
		source.batches[1][0].DocumentID != "document-10" {
		t.Fatalf("evidence batches = %#v", source.batches)
	}
	if len(first.Results) != 10 || len(second.Results) != 10 ||
		first.Results[0].BodyQueryMatches == nil || second.Results[0].BodyQueryMatches == nil {
		t.Fatalf("page evidence first=%#v second=%#v", first.Results, second.Results)
	}
}

func TestPageEvidenceDegradesOnEvidenceFailure(t *testing.T) {
	sentinel := errors.New("read failed")
	searcher := NewPageEvidenceSearcher(pageEvidenceInner{response: searchcore.Response{
		Results: []searchcore.Result{{
			DocumentID: "pending", Source: searchcore.SourceLocal, Snippet: "leading",
		}},
	}}, &pageEvidenceSource{err: sentinel})
	response, err := searcher.Search(t.Context(), searchcore.Request{Query: "needle"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if response.Results[0].Snippet != "leading" || len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Source != "local-evidence" {
		t.Fatalf("response = %#v", response)
	}
}

func TestPageEvidencePreservesSnippetWhenEvidenceStops(t *testing.T) {
	searcher := NewPageEvidenceSearcher(pageEvidenceInner{response: searchcore.Response{
		Results: []searchcore.Result{{
			DocumentID: "pending", Source: searchcore.SourceGlobal, Snippet: "cached leading text",
		}},
	}}, &pageEvidenceSource{pending: true})
	response, err := searcher.Search(t.Context(), searchcore.Request{Query: "needle"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if response.Results[0].Snippet != "cached leading text" ||
		response.Results[0].EvidenceReady {
		t.Fatalf("response = %#v", response.Results[0])
	}
}

func TestPageEvidenceRetainsRecoveryMode(t *testing.T) {
	source := &pageEvidenceSource{}
	searcher := NewPageEvidenceSearcher(pageEvidenceInner{response: searchcore.Response{
		Recovered: "fuzzy",
		Results: []searchcore.Result{{
			DocumentID: "pending", Source: searchcore.SourceLocal, Snippet: "leading",
		}},
	}}, source)
	if _, err := searcher.Search(t.Context(), searchcore.Request{Query: "recovry"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !source.req.Fuzzy {
		t.Fatal("recovered page evidence did not retain fuzzy matching")
	}
}

func TestPageEvidenceConstructorAndInnerErrors(t *testing.T) {
	inner := pageEvidenceInner{err: errors.New("search failed")}
	if _, ok := NewPageEvidenceSearcher(inner, nil).(pageEvidenceInner); !ok {
		t.Fatal("nil evidence changed searcher")
	}
	if NewPageEvidenceSearcher(nil, &pageEvidenceSource{}) != nil {
		t.Fatal("nil inner produced a searcher")
	}
	if _, err := NewPageEvidenceSearcher(inner, &pageEvidenceSource{}).Search(
		t.Context(),
		searchcore.Request{},
	); err == nil {
		t.Fatal("inner error was hidden")
	}
}
