package searchremote

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestCappedPeerRows(t *testing.T) {
	rows := []yagomodel.URIMetadataRow{{}, {}, {}}
	if got := cappedPeerRows(rows, 2); len(got) != 2 {
		t.Fatalf("capped rows = %d, want 2", len(got))
	}
	if got := cappedPeerRows(rows, 0); len(got) != 3 {
		t.Fatalf("uncapped rows = %d, want 3", len(got))
	}
	if got := cappedPeerRows(rows, 5); len(got) != 3 {
		t.Fatalf("under-limit rows = %d, want 3", len(got))
	}
}

func TestSearchResultsRetainsBodyOnlyPeerHit(t *testing.T) {
	rows := []yagomodel.URIMetadataRow{metadataRow(
		t,
		"MNOPQRSTUVWX",
		"https://example.org/document",
		"Unrelated metadata title",
	)}
	results, err := searchResults(
		t.Context(),
		searchcore.Request{
			Terms:  []string{"body-only-token"},
			Limit:  10,
			Verify: searchcore.VerifyIfExist,
		},
		rows,
		newRemoteScorer([]string{"body-only-token"}, DefaultRankingWeights()),
	)
	if err != nil || len(results) != 1 {
		t.Fatalf("body-only peer results = %#v, %v", results, err)
	}
}
