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
		yacymodel.SeedHash:     sampleHash(tb, word).String(),
		yacymodel.SeedName:     name,
		yacymodel.SeedPeerType: yacymodel.PeerSenior.String(),
	}

	roundTrip, err := yacymodel.ParseSeed(seed.String())
	if err != nil {
		tb.Fatalf("sample seed does not round-trip: %v", err)
	}

	return roundTrip
}

func sampleRWIEntry(tb testing.TB, word, urlWord string) yacymodel.RWIEntry {
	tb.Helper()

	entry := yacymodel.RWIEntry{
		WordHash: sampleHash(tb, word),
		Properties: map[string]string{
			yacymodel.ColURLHash:        sampleHash(tb, urlWord).String(),
			yacymodel.ColLocalLinkCount: "AB",
		},
	}

	roundTrip, err := yacymodel.ParseRWIEntry(entry.String())
	if err != nil {
		tb.Fatalf("sample rwi entry does not round-trip: %v", err)
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
