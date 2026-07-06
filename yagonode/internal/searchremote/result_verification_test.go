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

func TestVerifiedPeerResultsDropsTermlessRows(t *testing.T) {
	results := []searchcore.Result{
		{Title: "Сербия и Черногория", URL: "https://example.org/mn"},
		{Title: "Unantastbar - Punkrock aus Südtirol", URL: "https://www.bing.com/ck/a?p=x"},
	}
	kept := verifiedPeerResults(
		searchcore.Request{Terms: []string{"черногория"}, Verify: searchcore.VerifyIfExist},
		results,
	)
	if len(kept) != 1 || kept[0].URL != "https://example.org/mn" {
		t.Fatalf("kept = %#v", kept)
	}
}

func TestVerifiedPeerResultsTrustsWhenVerifyFalse(t *testing.T) {
	results := []searchcore.Result{{Title: "unrelated"}}
	kept := verifiedPeerResults(
		searchcore.Request{Terms: []string{"черногория"}, Verify: searchcore.VerifyFalse},
		results,
	)
	if len(kept) != 1 {
		t.Fatalf("verify=false dropped rows: %#v", kept)
	}
}

func TestVerificationTermsFallsBackToQueryWords(t *testing.T) {
	got := verificationTerms(searchcore.Request{Query: "black mountain"})
	if len(got) != 2 || got[0] != "black" || got[1] != "mountain" {
		t.Fatalf("fallback terms = %#v", got)
	}
	got = verificationTerms(searchcore.Request{Terms: []string{"a"}, Query: "b c"})
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("parsed terms not preferred: %#v", got)
	}
}

func TestVerificationTermsPreferContentWords(t *testing.T) {
	got := verificationTerms(searchcore.Request{Terms: []string{"что", "такое", "осень"}})
	if len(got) != 1 || got[0] != "осень" {
		t.Fatalf("verification terms = %#v", got)
	}
	got = verificationTerms(searchcore.Request{Terms: []string{"что", "такое"}})
	if len(got) != 2 {
		t.Fatalf("all-stopword fallback = %#v", got)
	}
}

func TestVerifiedPeerResultsRejectStopwordOnlyMentions(t *testing.T) {
	results := []searchcore.Result{
		{Title: "Что лучше: чулки или колготки", URL: "https://example.org/tights"},
		{Title: "Что такое осень — стихи", URL: "https://example.org/autumn"},
	}
	kept := verifiedPeerResults(
		searchcore.Request{
			Terms:  []string{"что", "такое", "осень"},
			Verify: searchcore.VerifyIfExist,
		},
		results,
	)
	if len(kept) != 1 || kept[0].URL != "https://example.org/autumn" {
		t.Fatalf("kept = %#v", kept)
	}
}
