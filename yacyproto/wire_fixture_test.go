package yacyproto_test

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func sampleHash(tb testing.TB, word string) yacymodel.Hash {
	tb.Helper()

	hash := yacymodel.WordHash(word)
	if !hash.Valid() {
		tb.Fatalf("sample hash for %q is invalid: %q", word, hash)
	}

	return hash
}

func sampleSeed(tb testing.TB, word, name string) yacymodel.Seed {
	tb.Helper()

	seed := yacymodel.Seed{
		Hash:     sampleHash(tb, word),
		Name:     yacymodel.Some(name),
		PeerType: yacymodel.Some(yacymodel.PeerSenior),
	}

	roundTrip, err := yacymodel.ParseSeed(tb.Context(), seed.String())
	if err != nil {
		tb.Fatalf("sample seed does not round-trip: %v", err)
	}

	return roundTrip
}

func sampleRWIPosting(tb testing.TB, word, urlWord string) yacymodel.RWIPosting {
	tb.Helper()

	entry := yacymodel.RWIPosting{
		WordHash: sampleHash(tb, word),
		Properties: map[string]string{
			yacymodel.ColURLHash:        sampleHash(tb, urlWord).String(),
			yacymodel.ColLocalLinkCount: "AB",
		},
	}

	roundTrip, err := yacymodel.ParseRWIPosting(entry.String())
	if err != nil {
		tb.Fatalf("sample rwi posting does not round-trip: %v", err)
	}

	return roundTrip
}

func sampleURLRow(tb testing.TB, urlWord string) yacymodel.URIMetadataRow {
	tb.Helper()

	row := yacymodel.URIMetadataRow{
		Properties: map[string]string{
			yacymodel.URLMetaHash: sampleHash(tb, urlWord).String(),
		},
	}

	roundTrip, err := yacymodel.ParseURIMetadataRow(row.String())
	if err != nil {
		tb.Fatalf("sample url row does not round-trip: %v", err)
	}

	return roundTrip
}
