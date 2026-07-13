package searchremote

import (
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
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
		response.PartialFailures[0].Source != searchcore.PartialFailureSourceRemoteYaCy ||
		!strings.Contains(
			response.PartialFailures[0].Reason,
			errRemoteSearchDecodedBudgetExhausted.Error(),
		) {
		t.Fatalf("decoded-budget response = %#v", response)
	}
}

func searchResponse(rows ...yagomodel.URIMetadataRow) yagoproto.SearchResponse {
	return yagoproto.SearchResponse{Count: len(rows), Resources: rows}
}
