package rankingprofile

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestWeightsCodecAddsNewDefaultsToLegacyProfile(t *testing.T) {
	weights, err := (weightsCodec{}).Decode([]byte(`{
		"title":9,"headings":2,"anchors":2,"body":1,"url":3,
		"hostRank":0,"freshness":0,"quality":0,"proximity":0
	}`))
	if err != nil {
		t.Fatalf("decode legacy profile: %v", err)
	}
	defaults := searchindex.DefaultRankingWeights()
	if weights.URLPrior != defaults.URLPrior ||
		weights.OrderedProximity != defaults.OrderedProximity ||
		weights.LexicalBlend != defaults.LexicalBlend ||
		weights.LexicalGapAgreement != defaults.LexicalGapAgreement {
		t.Fatalf("migrated weights = %+v, defaults = %+v", weights, defaults)
	}
	if weights.HostRank != 0 || weights.Freshness != 0 ||
		weights.Quality != 0 || weights.Proximity != 0 {
		t.Fatalf("legacy explicit zero changed: %+v", weights)
	}
}

func TestWeightsCodecPreservesExplicitNewZeros(t *testing.T) {
	weights, err := (weightsCodec{}).Decode([]byte(`{
		"title":1,"urlPrior":0,"orderedProximity":0,
		"lexicalBlend":0,"lexicalGapAgreement":0
	}`))
	if err != nil {
		t.Fatalf("decode explicit zero profile: %v", err)
	}
	if weights.URLPrior != 0 || weights.OrderedProximity != 0 ||
		weights.LexicalBlend != 0 || weights.LexicalGapAgreement != 0 {
		t.Fatalf("explicit zero changed: %+v", weights)
	}
}

func TestWeightsCodecKeepsMissingOlderOptionalPriorsDisabled(t *testing.T) {
	weights, err := (weightsCodec{}).Decode([]byte(`{
		"title":9,"headings":2,"anchors":2,"body":1,"url":3,
		"hostRank":0,"freshness":0
	}`))
	if err != nil {
		t.Fatalf("decode old optional profile: %v", err)
	}
	if weights.Quality != 0 || weights.Proximity != 0 {
		t.Fatalf("missing older priors were enabled: %+v", weights)
	}
}

func TestWeightsCodecAcceptsLegacyValueAboveNewWriteBounds(t *testing.T) {
	weights, err := (weightsCodec{}).Decode([]byte(`{"title":65}`))
	if err != nil || weights.Title != 65 {
		t.Fatalf("legacy profile = %+v, %v", weights, err)
	}
	if err := weights.Validate(); err == nil {
		t.Fatal("legacy value above the new bounds was accepted as a new write")
	}
}

func TestWeightsCodecRejectsInvalidFieldTypeAndNegativePersistedValue(t *testing.T) {
	if _, err := (weightsCodec{}).Decode([]byte(`{"title":"bad"}`)); err == nil {
		t.Fatal("invalid field type was accepted")
	}
	if _, err := (weightsCodec{}).Decode([]byte(`{"title":1,"hostRank":-1}`)); err == nil {
		t.Fatal("negative persisted weight was accepted")
	}
}
