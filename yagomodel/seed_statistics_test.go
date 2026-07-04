package yagomodel

import "testing"

func TestSeedStatisticsEmission(t *testing.T) {
	seed := Seed{
		Hash:              Hash("ABCDEFGHIJKL"),
		NoticedURLCount:   Some(0),
		OfferedURLCount:   Some(0),
		KnownSeedCount:    Some(7),
		ConnectsPerHour:   Some(0),
		IndexingSpeed:     Some(0),
		RequestSpeed:      Some(0),
		UplinkSpeed:       Some(0),
		SentWordCount:     Some[int64](1234),
		ReceivedWordCount: Some[int64](0),
		SentURLCount:      Some[int64](56),
		ReceivedURLCount:  Some[int64](78),
	}

	fields := seed.Properties()
	want := map[string]string{
		SeedNoticedURLCount:   "0",
		SeedOfferedURLCount:   "0",
		SeedKnownSeedCount:    "7",
		SeedConnectsPerHour:   "0",
		SeedIndexingSpeed:     "0",
		SeedRequestSpeed:      "0",
		SeedUplinkSpeed:       "0",
		SeedSentWordCount:     "1234",
		SeedReceivedWordCount: "0",
		SeedSentURLCount:      "56",
		SeedReceivedURLCount:  "78",
	}
	for key, value := range want {
		if fields[key] != value {
			t.Errorf("%s = %q, want %q", key, fields[key], value)
		}
	}
}

func TestSeedStatisticsRoundTripThroughWireForm(t *testing.T) {
	seed := Seed{
		Hash:           Hash("ABCDEFGHIJKL"),
		KnownSeedCount: Some(3),
		IndexingSpeed:  Some(0),
	}

	parsed, err := ParseSeed(t.Context(), seed.String())
	if err != nil {
		t.Fatalf("ParseSeed: %v", err)
	}
	if got := parsed.Properties()[SeedKnownSeedCount]; got != "3" {
		t.Errorf("%s after round trip = %q, want %q", SeedKnownSeedCount, got, "3")
	}
	if got := parsed.Properties()[SeedIndexingSpeed]; got != "0" {
		t.Errorf("%s after round trip = %q, want %q", SeedIndexingSpeed, got, "0")
	}
}

func TestSeedStatisticsAbsentByDefault(t *testing.T) {
	fields := Seed{Hash: Hash("ABCDEFGHIJKL")}.Properties()
	for _, key := range []string{
		SeedNoticedURLCount,
		SeedOfferedURLCount,
		SeedKnownSeedCount,
		SeedConnectsPerHour,
		SeedIndexingSpeed,
		SeedRequestSpeed,
		SeedUplinkSpeed,
	} {
		if _, ok := fields[key]; ok {
			t.Errorf("%s emitted for empty seed", key)
		}
	}
}
