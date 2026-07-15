package yagomodel

import (
	"errors"
	"testing"
)

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
	if value, ok := parsed.KnownSeedCount.Get(); !ok || value != 3 {
		t.Errorf("typed known seed count = %d, %t, want 3, true", value, ok)
	}
	if value, ok := parsed.IndexingSpeed.Get(); !ok || value != 0 {
		t.Errorf("typed indexing speed = %d, %t, want 0, true", value, ok)
	}
}

func TestParseSeedStatisticsPopulatesTypedCounters(t *testing.T) {
	parsed, err := ParseSeed(
		t.Context(),
		"{Hash=ABCDEFGHIJKL,NCount=1,RCount=2,SCount=3,ISpeed=4,USpeed=5,"+
			"sI=6,rI=7,sU=8,rU=9}",
	)
	if err != nil {
		t.Fatalf("ParseSeed: %v", err)
	}

	integers := []struct {
		name  string
		value Optional[int]
		want  int
	}{
		{SeedNoticedURLCount, parsed.NoticedURLCount, 1},
		{SeedOfferedURLCount, parsed.OfferedURLCount, 2},
		{SeedKnownSeedCount, parsed.KnownSeedCount, 3},
		{SeedIndexingSpeed, parsed.IndexingSpeed, 4},
		{SeedUplinkSpeed, parsed.UplinkSpeed, 5},
	}
	for _, field := range integers {
		if got, ok := field.value.Get(); !ok || got != field.want {
			t.Errorf("%s = %d, %t, want %d, true", field.name, got, ok, field.want)
		}
	}
	transfers := []struct {
		name  string
		value Optional[int64]
		want  int64
	}{
		{SeedSentWordCount, parsed.SentWordCount, 6},
		{SeedReceivedWordCount, parsed.ReceivedWordCount, 7},
		{SeedSentURLCount, parsed.SentURLCount, 8},
		{SeedReceivedURLCount, parsed.ReceivedURLCount, 9},
	}
	for _, field := range transfers {
		if got, ok := field.value.Get(); !ok || got != field.want {
			t.Errorf("%s = %d, %t, want %d, true", field.name, got, ok, field.want)
		}
	}
}

func TestParseSeedStatisticsRejectsInvalidTransferCounters(t *testing.T) {
	for _, value := range []string{"not-an-integer", "9223372036854775808"} {
		if _, err := ParseSeed(
			t.Context(),
			"{Hash=ABCDEFGHIJKL,sI="+value+"}",
		); !errors.Is(err, ErrBadSeed) {
			t.Errorf("ParseSeed sI=%q error = %v, want ErrBadSeed", value, err)
		}
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
