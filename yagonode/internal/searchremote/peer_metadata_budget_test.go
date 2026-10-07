package searchremote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerreputation"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

func TestSearchResultBoundsDecodedMetadataFields(t *testing.T) {
	hash := hashFor("metadata-limit")
	row := metadataRow(t, hash, "https://example.org/", "title")
	row.Properties[yagomodel.URLMetaURL] = yagomodel.EncodeCompactWireForm(
		strings.Repeat("u", remoteMetadataURLByteLimit),
	)
	row.Properties[yagomodel.URLMetaColDescription] = yagomodel.EncodeCompactWireForm(
		strings.Repeat("t", remoteMetadataTitleByteLimit),
	)
	result, err := searchResult(t.Context(), row)
	if err != nil || len(result.URL) != remoteMetadataURLByteLimit ||
		len(result.Title) != remoteMetadataTitleByteLimit {
		t.Fatalf("bounded result lengths = %d/%d, %v", len(result.URL), len(result.Title), err)
	}

	row.Properties[yagomodel.URLMetaURL] = yagomodel.EncodeCompactWireForm(
		strings.Repeat("u", remoteMetadataURLByteLimit+1),
	)
	if len(row.Properties[yagomodel.URLMetaURL]) >= 512 {
		t.Fatalf("compressed URL fixture = %d bytes", len(row.Properties[yagomodel.URLMetaURL]))
	}
	if _, err := searchResult(t.Context(), row); err == nil {
		t.Fatal("oversized decoded URL was accepted")
	}

	row.Properties[yagomodel.URLMetaURL] = yagomodel.EncodeBase64WireForm("https://example.org/")
	row.Properties[yagomodel.URLMetaColDescription] = yagomodel.EncodeCompactWireForm(
		strings.Repeat("t", remoteMetadataTitleByteLimit+1),
	)
	if _, err := searchResult(t.Context(), row); err == nil {
		t.Fatal("oversized decoded title was accepted")
	}
}

func TestBoundedRowLanguageOwnsAggregateBudget(t *testing.T) {
	row := yagomodel.URIMetadataRow{Properties: map[string]string{
		"lang": strings.Repeat("R", remoteMetadataLanguageByteLimit),
	}}
	budget := newRemoteQueryBudget()
	language, err := boundedRowLanguage(row, budget)
	if err != nil || len(language) != remoteMetadataLanguageByteLimit ||
		budget.decodedBytesRemaining != remoteQueryDecodedByteBudget-len(language) {
		t.Fatalf(
			"bounded language = %q, budget=%d, err=%v",
			language,
			budget.decodedBytesRemaining,
			err,
		)
	}
	row.Properties["lang"] = strings.Repeat("r", remoteMetadataLanguageByteLimit+1)
	if _, err := boundedRowLanguage(row, budget); !errors.Is(
		err,
		errRemoteSearchInvalidResult,
	) {
		t.Fatalf("oversized language error = %v", err)
	}
}

func TestSearchResultSharesDecodedMetadataBudget(t *testing.T) {
	row := metadataRow(t, hashFor("aggregate-limit"), "ab", "cd")
	budget := newRemoteQueryBudget()
	budget.decodedBytesRemaining = 3
	if _, err := searchResultWithinBudget(
		t.Context(),
		searchcore.Request{},
		row,
		budget,
	); !errors.Is(err, errRemoteSearchDecodedBudgetExhausted) {
		t.Fatalf("aggregate error = %v", err)
	}
	if budget.decodedBytesRemaining != 1 {
		t.Fatalf("remaining decoded bytes = %d", budget.decodedBytesRemaining)
	}
	row = metadataRow(t, hashFor("language-aggregate-limit"), "a", "")
	row.Properties["lang"] = "r"
	budget.decodedBytesRemaining = 1
	if _, err := searchResultWithinBudget(
		t.Context(),
		searchcore.Request{},
		row,
		budget,
	); !errors.Is(err, errRemoteSearchDecodedBudgetExhausted) {
		t.Fatalf("language aggregate error = %v", err)
	}
}

func TestResponseStopsAtDecodedBudgetWithoutPenalizingPeer(t *testing.T) {
	row := metadataRow(t, hashFor("response-limit"), "a", "")
	budget := newRemoteQueryBudget()
	budget.decodedBytesRemaining = 0
	response := (searcher{weights: weightsOrDefault(nil)}).responseWithinBudget(
		t.Context(),
		searchcore.Request{Limit: 1},
		[]peerSearchResult{{response: searchResponse(row)}},
		nil,
		budget,
	)
	if len(response.Results) != 0 || len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Source != searchcore.PartialFailureSourceRemoteStage ||
		response.PartialFailures[0].Diagnostic != (searchcore.PartialFailureDiagnostic{
			Stage: searchcore.FailureStageRemoteStage,
			Cause: searchcore.FailureCauseBudget,
		}) ||
		!strings.Contains(
			response.PartialFailures[0].Reason,
			errRemoteSearchDecodedBudgetExhausted.Error(),
		) {
		t.Fatalf("decoded-budget response = %#v", response)
	}
}

func TestLocalResultEntryBudgetReportsOmissionWithoutPenalizingPeer(t *testing.T) {
	rows := []yagomodel.URIMetadataRow{
		metadataRow(t, hashFor("entry-budget-one"), "https://example.org/one", "one"),
		metadataRow(t, hashFor("entry-budget-two"), "https://example.org/two", "two"),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeFixtureResponse(t, w, searchResponse(rows...).Encode().Encode())
	}))
	defer server.Close()
	remote := NewSearcher(Config{Client: server.Client(), NetworkName: "freeworld"}).(searcher)
	remote.lifecycle = newPeerLifecycleSession(nil)
	request := searchcore.Request{
		Query: "alpha", Terms: []string{"alpha"}, Source: searchcore.SourceGlobal, Limit: 1,
	}
	wireRequest := remoteSearchRequest(request, "freeworld", time.Second)
	evidenceBinding := identityQueryMatchEvidenceBinding(request.Terms)
	evidenceBinding.request(&wireRequest)
	budget := newRemoteQueryBudget()
	budget.resultEntriesRemaining = 0
	results := remote.queryPeerJobsWithinBudget(t.Context(), []peerSearchJob{{
		peer:            serverSeed(t, server.URL),
		request:         wireRequest,
		evidenceBinding: evidenceBinding,
	}}, budget)
	var observed []peerreputation.Observation
	reputation := &reputationSession{observations: reputationObservationSinkFunc(
		func(_ context.Context, rows []peerreputation.Observation) {
			observed = append(observed, rows...)
		},
	)}
	response := remote.responseWithinBudget(t.Context(), request, results, reputation, budget)
	response = finalizeRemoteBudgetFailures(response, budget)
	reputation.flush(t.Context())
	if len(results) != 1 || !results[0].resourcesTruncated || len(response.Results) != 0 {
		t.Fatalf("results/response = %#v/%#v", results, response)
	}
	if budget.omittedPeerJobs != 1 {
		t.Fatalf("omitted peer jobs = %d, want 1", budget.omittedPeerJobs)
	}
	if len(observed) != 1 || observed[0].Outcome != peerreputation.OutcomeSuccess {
		t.Fatalf("local result-budget observations = %#v, want peer success", observed)
	}
	if len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Source != searchcore.PartialFailureSourceRemoteStage ||
		response.PartialFailures[0].Diagnostic != (searchcore.PartialFailureDiagnostic{
			Stage: searchcore.FailureStageRemoteStage,
			Cause: searchcore.FailureCauseBudget,
		}) {
		t.Fatalf("local result-budget failure = %#v", response.PartialFailures)
	}
}

func TestDecodedBudgetPreservesLaterPeerFailure(t *testing.T) {
	firstPeer := yagomodel.Seed{Hash: "peer-a"}
	laterPeer := yagomodel.Seed{Hash: "peer-b"}
	budget := newRemoteQueryBudget()
	budget.decodedBytesRemaining = 0
	row := metadataRow(t, hashFor("decode-budget"), "https://example.org/", "title")
	response := (searcher{weights: weightsOrDefault(nil)}).responseWithinBudget(
		t.Context(),
		searchcore.Request{Query: "alpha", Terms: []string{"alpha"}, Limit: 1},
		[]peerSearchResult{
			{peer: firstPeer, response: searchResponse(row)},
			{peer: laterPeer, err: errors.New("connection refused")},
		},
		nil,
		budget,
	)
	response = finalizeRemoteBudgetFailures(response, budget)

	if len(response.PartialFailures) != 2 {
		t.Fatalf(
			"decoded-budget failures = %#v, want local budget and later peer failure",
			response.PartialFailures,
		)
	}
	var localBudget, laterPeerFailure bool
	for _, failure := range response.PartialFailures {
		if failure.Source == searchcore.PartialFailureSourceRemoteStage &&
			failure.Diagnostic.Cause == searchcore.FailureCauseBudget {
			localBudget = strings.Contains(failure.Reason, "1 remote search job")
		}
		if failure.Source == laterPeer.Hash.String() &&
			failure.Diagnostic.Stage == searchcore.FailureStageRemotePeer {
			laterPeerFailure = true
		}
	}
	if !localBudget || !laterPeerFailure {
		t.Fatalf(
			"decoded-budget failures = %#v, want one local omission and later peer failure",
			response.PartialFailures,
		)
	}
}

func searchResponse(rows ...yagomodel.URIMetadataRow) yagoproto.SearchResponse {
	return yagoproto.SearchResponse{Count: len(rows), Resources: rows}
}
