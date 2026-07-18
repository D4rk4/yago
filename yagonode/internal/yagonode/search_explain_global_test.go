package yagonode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type globalSearchExplainScript struct {
	response searchcore.Response
	err      error
	request  searchcore.Request
}

func (s *globalSearchExplainScript) Search(
	_ context.Context,
	request searchcore.Request,
) (searchcore.Response, error) {
	s.request = request

	return s.response, s.err
}

func postGlobalExplain(
	t *testing.T,
	searcher searchcore.Searcher,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, pathSearchExplain, strings.NewReader(body),
	)
	newSearchExplainEndpoint(nil, nil, nil, nil, nil).withGlobal(searcher).
		ServeHTTP(recorder, request)

	return recorder
}

func TestSearchExplainEndpointExplainsGlobalFusion(t *testing.T) {
	peerEvidence := searchcore.NewRankingEvidence(
		searchcore.RankingSignalValue{Signal: searchcore.SignalRemoteRank, Value: 2},
		searchcore.RankingSignalValue{Signal: searchcore.SignalSourceCount, Value: 1},
	)
	webEvidence := searchcore.NewRankingEvidence(
		searchcore.RankingSignalValue{Signal: searchcore.SignalSourceCount, Value: 1},
		searchcore.RankingSignalValue{Signal: searchcore.SignalWebRank, Value: 1},
	)
	searcher := &globalSearchExplainScript{response: searchcore.Response{
		Results: []searchcore.Result{
			{
				URL: "https://peer.example/", Source: searchcore.SourceRemote,
				Score: searchcore.ReciprocalRankContribution(2), Evidence: peerEvidence,
			},
			{
				URL: "https://web.example/", Source: searchcore.SourceWeb,
				Score: searchcore.ReciprocalRankContribution(1), Evidence: webEvidence,
			},
		},
		PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceRemoteYaCy, Reason: "remote search failed",
		}},
	}}
	recorder := postGlobalExplain(t, searcher, `{"query":"alpha","scope":"global"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", recorder.Code, recorder.Body.String())
	}
	if searcher.request.Source != searchcore.SourceGlobal || !searcher.request.Explain ||
		searcher.request.Limit != searchExplainMaxResults {
		t.Fatalf("request = %#v", searcher.request)
	}
	var response searchExplainResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Scope != searchcore.SourceGlobal || len(response.PartialFailures) != 1 ||
		len(response.Results) != 2 {
		t.Fatalf("response = %#v", response)
	}
	bySource := map[string]searchExplainResult{}
	for _, result := range response.Results {
		bySource[result.Source] = result
	}
	if len(bySource["peer"].Fusion) != 1 ||
		bySource["peer"].Fusion[0].Branch != "peer" ||
		len(bySource["web"].Fusion) != 1 ||
		bySource["web"].Fusion[0].Branch != "web" {
		t.Fatalf("fusion = %#v", bySource)
	}
	if _, internalNameEscaped := bySource[string(searchcore.SourceWeb)]; internalNameEscaped {
		t.Fatalf("internal web provider name escaped: %#v", bySource)
	}
}

func TestSearchExplainSourceLabelsGlobalLocalRows(t *testing.T) {
	if source := searchExplainSource(
		searchcore.Result{Source: searchcore.SourceGlobal},
	); source != "local" {
		t.Fatalf("source = %q", source)
	}
}

func TestSearchExplainEndpointExplainsLocalAndWebDuplicateFusion(t *testing.T) {
	shared := "https://shared.example/"
	fused := searchcore.FuseByReciprocalRank(
		[]searchcore.Result{
			{URL: "https://local.example/", Source: searchcore.SourceLocal},
			{URL: shared, Source: searchcore.SourceLocal},
		},
		[]searchcore.Result{{URL: shared, Source: searchcore.SourceWeb}},
	)
	searcher := &globalSearchExplainScript{response: searchcore.Response{Results: fused}}
	recorder := postGlobalExplain(t, searcher, `{"query":"alpha","scope":"global"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", recorder.Code, recorder.Body.String())
	}
	var response searchExplainResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 2 || response.Results[0].URL != shared {
		t.Fatalf("results = %#v", response.Results)
	}
	contributions := map[string]searchExplainFusion{}
	for _, contribution := range response.Results[0].Fusion {
		contributions[contribution.Branch] = contribution
	}
	if contributions["local"].Rank != 2 || contributions["web"].Rank != 1 {
		t.Fatalf("contributions = %#v", contributions)
	}
}

func TestSearchExplainFusionInfersLegacyPureWebRank(t *testing.T) {
	contributions := searchExplainFusionContributions(searchcore.Result{
		Source: searchcore.SourceWeb,
		Score:  searchcore.ReciprocalRankContribution(3),
	}, nil)
	if len(contributions) != 1 || contributions[0].Branch != "web" ||
		contributions[0].Rank != 3 {
		t.Fatalf("contributions = %#v", contributions)
	}
}

func TestSearchExplainEndpointExplainsGlobalLearnedRanking(t *testing.T) {
	searcher := &globalSearchExplainScript{response: searchcore.Response{
		Results: []searchcore.Result{
			{
				URL: "https://peer.example/", Source: searchcore.SourceRemote,
				Score: 1, Evidence: searchcore.NewRankingEvidence(
					searchcore.RankingSignalValue{
						Signal: searchcore.SignalRetrievalScore, Value: 1,
					},
				),
			},
			{
				URL: "https://web.example/", Source: searchcore.SourceWeb,
				Score: 3, Evidence: searchcore.NewRankingEvidence(
					searchcore.RankingSignalValue{
						Signal: searchcore.SignalRetrievalScore, Value: 3,
					},
				),
			},
		},
	}}
	ranker := activeExplainRanker(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, pathSearchExplain,
		strings.NewReader(`{"query":"alpha","scope":"global"}`),
	)
	newSearchExplainEndpoint(nil, nil, nil, ranker, nil).withGlobal(searcher).
		ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", recorder.Code, recorder.Body.String())
	}
	var response searchExplainResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if searcher.request.Limit != ranker.CandidateWindow() ||
		len(response.Results) != 2 || response.Results[0].Source != "web" ||
		response.Results[0].Learned == nil ||
		response.Results[0].Learned.OriginalScore != 3 {
		t.Fatalf("learned global response = %#v request = %#v", response, searcher.request)
	}
}

func TestSearchExplainEndpointLabelsWebPartialFailures(t *testing.T) {
	searcher := &globalSearchExplainScript{response: searchcore.Response{
		PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceWeb,
			Reason: "provider failed",
		}},
	}}
	recorder := postGlobalExplain(t, searcher, `{"query":"alpha","scope":"global"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", recorder.Code, recorder.Body.String())
	}
	var response searchExplainResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.PartialFailures) != 1 || response.PartialFailures[0].Source != "web" {
		t.Fatalf("partial failures = %#v", response.PartialFailures)
	}
}

func TestSearchExplainEndpointBoundsResultAndEvidencePayload(t *testing.T) {
	results := make([]searchcore.Result, 300)
	evidenceValues := make([]searchcore.RankingSignalValue, 0, 32)
	for signal := searchcore.RankingSignal(0); signal <= searchcore.SignalWebRank; signal++ {
		evidenceValues = append(evidenceValues, searchcore.RankingSignalValue{
			Signal: signal,
			Value:  float64(signal + 1),
		})
	}
	evidence := searchcore.NewRankingEvidence(evidenceValues...)
	for index := range results {
		results[index] = searchcore.Result{
			URL:      fmt.Sprintf("https://result.example/%03d", index),
			Score:    float64(len(results) - index),
			Source:   searchcore.SourceRemote,
			Evidence: evidence,
		}
	}
	searcher := &globalSearchExplainScript{response: searchcore.Response{Results: results}}
	recorder := postGlobalExplain(t, searcher, `{"query":"alpha","scope":"global"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", recorder.Code, recorder.Body.String())
	}
	var response searchExplainResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != searchExplainMaxResults {
		t.Fatalf("results = %d", len(response.Results))
	}
	for _, result := range response.Results {
		if len(result.Evidence) != len(evidenceValues) {
			t.Fatalf("evidence = %d, want %d", len(result.Evidence), len(evidenceValues))
		}
	}
}

func TestSearchExplainEndpointRejectsUnavailableOrInvalidGlobalRequests(t *testing.T) {
	tests := []struct {
		name     string
		searcher searchcore.Searcher
		body     string
		status   int
	}{
		{"unavailable", nil, `{"query":"alpha","scope":"global"}`, http.StatusServiceUnavailable},
		{
			"invalid scope",
			&globalSearchExplainScript{},
			`{"query":"alpha","scope":"peer"}`,
			http.StatusBadRequest,
		},
		{
			"custom weights",
			&globalSearchExplainScript{},
			`{"query":"alpha","scope":"global","weights":{"title":5,"headings":1,"anchors":1,"body":1,"url":1,"quality":1}}`,
			http.StatusBadRequest,
		},
		{
			"failure", &globalSearchExplainScript{err: errors.New("broken")},
			`{"query":"alpha","scope":"global"}`, http.StatusInternalServerError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := postGlobalExplain(t, test.searcher, test.body)
			if recorder.Code != test.status {
				t.Fatalf(
					"status = %d, want %d, body = %q",
					recorder.Code,
					test.status,
					recorder.Body.String(),
				)
			}
		})
	}
}
