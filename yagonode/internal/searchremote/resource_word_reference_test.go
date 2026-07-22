package searchremote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerreputation"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

func TestValidateRemoteResourceWordReference(t *testing.T) {
	valid := metadataRow(t, hashFor("valid"), "https://example.org/valid", "Valid")
	if err := validateRemoteResourceWordReference(valid); err != nil {
		t.Fatalf("validateRemoteResourceWordReference: %v", err)
	}

	missing := metadataRow(t, hashFor("missing"), "https://example.org/missing", "Missing")
	delete(missing.Properties, yagomodel.URLMetaWordReference)
	malformed := metadataRow(t, hashFor("malformed"), "https://example.org/malformed", "Malformed")
	malformed.Properties[yagomodel.URLMetaWordReference] = "not+enhanced"
	badShape := metadataRow(t, hashFor("bad-shape"), "https://example.org/bad-shape", "Bad shape")
	badShape.Properties[yagomodel.URLMetaWordReference] = yagomodel.Encode(
		[]byte("{h=bad-shapeAAA}"),
	)
	badResource := metadataRow(
		t,
		hashFor("bad-resource"),
		"https://example.org/bad-resource",
		"Bad resource",
	)
	badResource.Properties[yagomodel.URLMetaHash] = "bad"
	mismatch := metadataRow(t, hashFor("resource"), "https://example.org/mismatch", "Mismatch")
	mismatch.Properties[yagomodel.URLMetaWordReference] = wordReferenceForHash(hashFor("other"))
	decodedOversized := metadataRow(
		t,
		hashFor("decoded-big"),
		"https://example.org/decoded-big",
		"Decoded big",
	)
	decodedOversized.Properties[yagomodel.URLMetaWordReference] = yagomodel.Encode(
		make([]byte, maximumRemoteWordReferenceBytes+1),
	)
	oversized := metadataRow(t, hashFor("oversized"), "https://example.org/oversized", "Oversized")
	oversized.Properties[yagomodel.URLMetaWordReference] = string(
		make([]byte, maximumRemoteWordReferenceBytes*2+1),
	)

	for _, row := range []yagomodel.URIMetadataRow{
		missing,
		malformed,
		badShape,
		badResource,
		mismatch,
		decodedOversized,
		oversized,
	} {
		if err := validateRemoteResourceWordReference(row); !errors.Is(
			err,
			errRemoteResourceWordReference,
		) {
			t.Fatalf("validation error = %v", err)
		}
	}
}

func TestInvalidRemoteWordReferenceHasNoResultSideEffects(t *testing.T) {
	row := metadataRow(t, hashFor("invalid"), "https://example.org/invalid", "Invalid")
	delete(row.Properties, yagomodel.URLMetaWordReference)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeFixtureResponse(t, w, yagoproto.SearchResponse{
			JoinCount: 1,
			Count:     1,
			Resources: []yagomodel.URIMetadataRow{row},
		}.Encode().Encode())
	}))
	defer server.Close()

	reputation := &capturedReputationObservations{}
	received := 0
	response, err := NewSearcher(Config{
		Client:                 server.Client(),
		Peers:                  fakePeerSource{peers: []yagomodel.Seed{serverSeed(t, server.URL)}},
		MaxPeers:               1,
		Redundancy:             1,
		ReputationObservations: reputation,
		ObserveReceivedResources: func(_ context.Context, count int) {
			received += count
		},
	}).Search(t.Context(), searchcore.Request{
		Terms: []string{"invalid"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Results) != 0 || received != 0 {
		t.Fatalf("results = %#v, received = %d", response.Results, received)
	}
	if len(response.PartialFailures) != 1 {
		t.Fatalf("partial failures = %#v", response.PartialFailures)
	}
	if len(reputation.batches) != 1 || len(reputation.batches[0]) != 1 ||
		reputation.batches[0][0].Outcome != peerreputation.OutcomeInvalidResult {
		t.Fatalf("reputation observations = %#v", reputation.batches)
	}
}

func TestValidatedRemoteResourcePreservesEmptyMetadataFallback(t *testing.T) {
	row := metadataRow(
		t,
		hashFor("empty-title"),
		"https://example.org/empty-title",
		"unused",
	)
	delete(row.Properties, yagomodel.URLMetaColDescription)

	result, err := searchResult(t.Context(), row)
	if err != nil {
		t.Fatalf("searchResult: %v", err)
	}
	if result.Title != result.URL {
		t.Fatalf("title = %q, URL = %q", result.Title, result.URL)
	}
}

func TestResponsePenalizesMalformedValidatedMetadata(t *testing.T) {
	row := metadataRow(
		t,
		hashFor("bad-metadata"),
		"https://example.org/bad-metadata",
		"Bad metadata",
	)
	row.Properties[yagomodel.URLMetaURL] = "z|@@@"
	reputation := &reputationSession{}

	response := (searcher{weights: weightsOrDefault(nil)}).responseWithinBudget(
		t.Context(),
		searchcore.Request{Limit: 1},
		[]peerSearchResult{{
			peer:     yagomodel.Seed{Hash: hashFor("bad-peer")},
			response: searchResponse(row),
		}},
		reputation,
		newRemoteQueryBudget(),
	)
	if len(response.Results) != 0 || len(response.PartialFailures) != 1 {
		t.Fatalf("response = %#v", response)
	}
}

func wordReferenceForHash(hash yagomodel.Hash) string {
	return yagomodel.Encode([]byte(yagomodel.WordReferencePropertyForm(
		yagomodel.RWIPosting{Properties: map[string]string{yagomodel.ColURLHash: hash.String()}},
	)))
}
