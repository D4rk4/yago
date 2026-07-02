package yacymodel

import (
	"testing"
	"time"
)

func TestYaCyGenericFormatterShortSecondFixtures(t *testing.T) {
	lastSeen, err := ParseSeedLastSeenUTC("20180628140713")
	if err != nil {
		t.Fatalf("ParseSeedLastSeenUTC: %v", err)
	}
	if want := time.Date(2018, 6, 28, 14, 7, 13, 0, time.UTC); !lastSeen.Time().Equal(want) {
		t.Fatalf("LastSeen time = %s, want %s", lastSeen.Time(), want)
	}

	birth, err := ParseSeedBirthDateUTC("20180628140713")
	if err != nil {
		t.Fatalf("ParseSeedBirthDateUTC: %v", err)
	}
	if !birth.Time().Equal(lastSeen.Time()) {
		t.Fatalf("BirthDate time = %s, want %s", birth.Time(), lastSeen.Time())
	}

	instant := time.Date(2018, 6, 28, 10, 49, 35, 726_000_000, time.UTC)
	if got := NewSeedLastSeenUTC(instant).String(); got != "20180628104935" {
		t.Fatalf("LastSeen wire form = %q, want 20180628104935", got)
	}
	if got := NewSeedBirthDateUTC(instant).String(); got != "20180628104935" {
		t.Fatalf("BirthDate wire form = %q, want 20180628104935", got)
	}
}
