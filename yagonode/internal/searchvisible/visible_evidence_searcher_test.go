package searchvisible

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type visibleResultSource struct {
	response searchcore.Response
	err      error
}

type stagedVisibleEvidenceCancellation struct {
	context.Context
	remaining int
}

func (c *stagedVisibleEvidenceCancellation) Err() error {
	if c.remaining == 0 {
		return context.Canceled
	}
	c.remaining--

	return nil
}

func (s visibleResultSource) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	response := s.response
	response.Results = append([]searchcore.Result(nil), response.Results...)

	return response, s.err
}

func TestVisibleEvidenceSearcherAnalyzesPeerWebAndLegacyRows(t *testing.T) {
	sources := []searchcore.Source{
		searchcore.SourceRemote,
		searchcore.SourceWeb,
		searchcore.SourceGlobal,
	}
	results := make([]searchcore.Result, len(sources)+1)
	for index, source := range sources {
		results[index] = searchcore.Result{
			Source:   source,
			Language: "ru",
			Title:    "Чрезвычайные меры",
			Snippet:  "Обзор чрезвычайных полномочий президента.",
			URL:      "https://example.test/%D0%BF%D0%BE%D0%BB%D0%BD%D0%BE%D0%BC%D0%BE%D1%87%D0%B8%D1%8F",
		}
	}
	retainedPositions := map[string]map[string][]int{"body": {"полномочия": {7}}}
	results[len(sources)] = searchcore.Result{
		Source:             searchcore.SourceGlobal,
		EvidenceReady:      true,
		Analyzer:           "retained",
		FieldTermPositions: retainedPositions,
	}
	response, err := NewVisibleEvidenceSearcher(visibleResultSource{
		response: searchcore.Response{Results: results},
	}).Search(t.Context(), searchcore.Request{
		Terms: []string{"чрезвычайные", "полномочия"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for index, source := range sources {
		result := response.Results[index]
		if result.Source != source || !result.EvidenceReady || result.Analyzer != "ru" {
			t.Fatalf("result %d = %#v", index, result)
		}
		positions := result.FieldTermPositions["snippet"]
		if len(positions["чрезвычайные"]) == 0 || len(positions["полномочия"]) == 0 ||
			len(result.FieldTermPositions["url"]["полномочия"]) == 0 {
			t.Fatalf("result %d positions = %#v", index, result.FieldTermPositions)
		}
		if len(result.QueryMatches) != 2 {
			t.Fatalf("result %d matches = %#v", index, result.QueryMatches)
		}
	}
	retained := response.Results[len(sources)]
	if retained.Analyzer != "retained" ||
		!reflect.DeepEqual(retained.FieldTermPositions, retainedPositions) {
		t.Fatalf("ready result changed = %#v", retained)
	}
}

func TestVisibleEvidenceSearcherImprovesMorphologicalProximityRanking(t *testing.T) {
	far := "Игровые " + strings.Repeat("обычные слова ", 18) + "мыши"
	inner := visibleResultSource{response: searchcore.Response{Results: []searchcore.Result{
		{URL: "far", Score: 1, Source: searchcore.SourceRemote, Language: "ru", Snippet: far},
		{
			URL:      "close",
			Score:    1,
			Source:   searchcore.SourceWeb,
			Language: "ru",
			Snippet:  "Лучшие игровые мыши",
		},
		{
			URL:      "missing",
			Score:    1,
			Source:   searchcore.SourceGlobal,
			Language: "ru",
			Snippet:  "Клавиатуры",
		},
	}}}
	searcher := searchcore.NewLexicalEvidenceSearcher(NewVisibleEvidenceSearcher(inner))
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Terms: []string{"игровая", "мышь"},
		Limit: 3,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if response.Results[0].URL != "close" {
		t.Fatalf(
			"ranked URLs = %q, %q, %q",
			response.Results[0].URL,
			response.Results[1].URL,
			response.Results[2].URL,
		)
	}
	closePositions := response.Results[0].FieldTermPositions["snippet"]
	if closePositions["игровая"][0]+1 != closePositions["мышь"][0] {
		t.Fatalf("close positions = %#v", closePositions)
	}
}

func TestVisibleEvidenceSearcherLeavesInvalidTextForStructuralFallback(t *testing.T) {
	result := searchcore.Result{
		Source:  searchcore.SourceRemote,
		Title:   "alpha beta",
		Snippet: "alpha\xffbeta",
	}
	response, err := NewVisibleEvidenceSearcher(visibleResultSource{
		response: searchcore.Response{Results: []searchcore.Result{result}},
	}).Search(t.Context(), searchcore.Request{Terms: []string{"alpha", "beta"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := response.Results[0]
	if got.EvidenceReady || got.QueryMatches != nil || got.FieldTermPositions != nil {
		t.Fatalf("invalid result was marked analyzed = %#v", got)
	}
}

func TestVisibleEvidenceSearcherLeavesRowsUntouchedWithoutQueryRequirements(t *testing.T) {
	result := searchcore.Result{Source: searchcore.SourceRemote, Snippet: "alpha"}
	response, err := NewVisibleEvidenceSearcher(visibleResultSource{
		response: searchcore.Response{Results: []searchcore.Result{result}},
	}).Search(t.Context(), searchcore.Request{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !reflect.DeepEqual(response.Results[0], result) {
		t.Fatalf("result changed = %#v", response.Results[0])
	}
}

func TestVisibleEvidenceSearcherBoundsCandidateWindowWithoutDroppingRows(t *testing.T) {
	results := make([]searchcore.Result, maximumVisibleEvidenceCandidates+2)
	for index := range results {
		results[index] = searchcore.Result{
			URL:     fmt.Sprintf("https://example.test/%d", index),
			Source:  searchcore.SourceRemote,
			Snippet: "alpha",
		}
	}
	response, err := NewVisibleEvidenceSearcher(visibleResultSource{
		response: searchcore.Response{Results: results},
	}).Search(t.Context(), searchcore.Request{Terms: []string{"alpha"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Results) != len(results) {
		t.Fatalf("results = %d", len(response.Results))
	}
	for index, result := range response.Results {
		if got, want := result.EvidenceReady, index < maximumVisibleEvidenceCandidates; got != want {
			t.Fatalf("result %d evidence ready = %v, want %v", index, got, want)
		}
	}
}

func TestVisibleEvidenceSearcherPropagatesInnerFailureAndPreservesRowsOnCancellation(t *testing.T) {
	innerFailure := errors.New("inner failed")
	if _, err := NewVisibleEvidenceSearcher(visibleResultSource{err: innerFailure}).Search(
		t.Context(),
		searchcore.Request{},
	); !errors.Is(err, innerFailure) {
		t.Fatalf("inner error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	response, err := NewVisibleEvidenceSearcher(visibleResultSource{response: searchcore.Response{
		Results: []searchcore.Result{
			{URL: "https://example.test/", Source: searchcore.SourceWeb, Snippet: "alpha"},
		},
	}}).Search(ctx, searchcore.Request{Terms: []string{"alpha"}})
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].URL != "https://example.test/" ||
		response.Results[0].EvidenceReady {
		t.Fatalf("cancellation response = %#v error = %v", response, err)
	}
}

func TestVisibleEvidenceSearcherPreservesRowsWhenAnalysisIsCancelled(t *testing.T) {
	ctx := &stagedVisibleEvidenceCancellation{Context: t.Context(), remaining: 2}
	response, err := NewVisibleEvidenceSearcher(visibleResultSource{response: searchcore.Response{
		Results: []searchcore.Result{{
			URL: "https://example.test/", Source: searchcore.SourceRemote, Snippet: "alpha beta",
		}},
	}}).Search(ctx, searchcore.Request{Terms: []string{"alpha", "beta"}})
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].URL != "https://example.test/" ||
		response.Results[0].EvidenceReady {
		t.Fatalf("analysis cancellation response = %#v error = %v", response, err)
	}
}

func TestVisibleEvidenceSearcherUsesQueryWordsAndAutoDetectsScript(t *testing.T) {
	response, err := NewVisibleEvidenceSearcher(visibleResultSource{response: searchcore.Response{
		Results: []searchcore.Result{{
			Source:  searchcore.SourceRemote,
			Snippet: "Чрезвычайных полномочий",
		}},
	}}).Search(t.Context(), searchcore.Request{Query: "чрезвычайные полномочия"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result := response.Results[0]; result.Analyzer != "ru" ||
		len(result.FieldTermPositions["snippet"]["полномочия"]) == 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestDecodedVisibleURLAndMatchMapping(t *testing.T) {
	if got := decodedVisibleURL(
		"https://example.test/a%20b+c",
	); got != "https://example.test/a b+c" {
		t.Fatalf("decoded URL = %q", got)
	}
	invalid := "https://example.test/%zz"
	if got := decodedVisibleURL(invalid); got != invalid {
		t.Fatalf("invalid URL = %q", got)
	}
	if got := coreQueryMatches(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil mapped matches = %#v", got)
	}
}

func BenchmarkVisibleEvidenceSearcherCandidateWindow(b *testing.B) {
	results := make([]searchcore.Result, maximumVisibleEvidenceCandidates)
	for index := range results {
		results[index] = searchcore.Result{
			Source:   searchcore.SourceRemote,
			Language: "ru",
			Title:    "Чрезвычайные полномочия",
			Snippet: fmt.Sprintf(
				"Правовой обзор чрезвычайных полномочий и условий их применения, документ %d.",
				index,
			),
			URL: fmt.Sprintf("https://example.test/legal/emergency-powers/%d", index),
		}
	}
	searcher := NewVisibleEvidenceSearcher(visibleResultSource{response: searchcore.Response{
		Results: results,
	}})
	request := searchcore.Request{Terms: []string{"чрезвычайные", "полномочия"}}
	b.ReportAllocs()
	for range b.N {
		response, err := searcher.Search(b.Context(), request)
		if err != nil || !response.Results[0].EvidenceReady {
			b.Fatalf("ready=%v err=%v", response.Results[0].EvidenceReady, err)
		}
	}
	b.ReportMetric(
		float64(b.Elapsed().Nanoseconds())/float64(b.N*len(results)),
		"ns/result",
	)
}
