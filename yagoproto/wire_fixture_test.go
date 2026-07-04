package yagoproto_test

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func sampleHash(tb testing.TB, word string) yagomodel.Hash {
	tb.Helper()

	hash := yagomodel.WordHash(word)
	if !hash.Valid() {
		tb.Fatalf("sample hash for %q is invalid: %q", word, hash)
	}

	return hash
}

func sampleSeed(tb testing.TB, word, name string) yagomodel.Seed {
	tb.Helper()

	seed := yagomodel.Seed{
		Hash:     sampleHash(tb, word),
		Name:     yagomodel.Some(name),
		PeerType: yagomodel.Some(yagomodel.PeerSenior),
	}

	roundTrip, err := yagomodel.ParseSeed(tb.Context(), seed.String())
	if err != nil {
		tb.Fatalf("sample seed does not round-trip: %v", err)
	}

	return roundTrip
}

func sampleRWIPosting(tb testing.TB, word, urlWord string) yagomodel.RWIPosting {
	tb.Helper()

	entry := yagomodel.RWIPosting{
		WordHash: sampleHash(tb, word),
		Properties: map[string]string{
			yagomodel.ColURLHash:        sampleHash(tb, urlWord).String(),
			yagomodel.ColLocalLinkCount: "AB",
		},
	}

	roundTrip, err := yagomodel.ParseRWIPosting(entry.String())
	if err != nil {
		tb.Fatalf("sample rwi posting does not round-trip: %v", err)
	}

	return roundTrip
}

func sampleURLRow(tb testing.TB, urlWord string) yagomodel.URIMetadataRow {
	tb.Helper()

	row := yagomodel.URIMetadataRow{
		Properties: map[string]string{
			yagomodel.URLMetaHash: sampleHash(tb, urlWord).String(),
		},
	}

	roundTrip, err := yagomodel.ParseURIMetadataRow(row.String())
	if err != nil {
		tb.Fatalf("sample url row does not round-trip: %v", err)
	}

	return roundTrip
}
