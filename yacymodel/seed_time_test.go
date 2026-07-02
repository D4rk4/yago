package yacymodel

import (
	"errors"
	"testing"
	"time"
)

func TestSeedLastSeenUTCFromTimeTruncatesToUTCSecond(t *testing.T) {
	local := time.FixedZone("sample", 2*60*60+30*60)
	seen := NewSeedLastSeenUTC(time.Date(2026, 7, 1, 12, 34, 56, 789, local))

	if got := seen.String(); got != "20260701100456" {
		t.Fatalf("LastSeen string = %q", got)
	}
	if !seen.Time().Equal(time.Date(2026, 7, 1, 10, 4, 56, 0, time.UTC)) {
		t.Fatalf("LastSeen time = %s", seen.Time())
	}
}

func TestSeedUTCOffsetFromTime(t *testing.T) {
	positive := time.FixedZone("positive", 2*60*60+30*60)
	if got := SeedUTCOffsetFromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, positive)); got != "+0230" {
		t.Fatalf("positive offset = %q", got)
	}

	negative := time.FixedZone("negative", -(3*60*60 + 45*60))
	if got := SeedUTCOffsetFromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, negative)); got != "-0345" {
		t.Fatalf("negative offset = %q", got)
	}
}

func TestParseSeedUTCOffsetRejectsBadNumbers(t *testing.T) {
	for _, raw := range []string{"+aa00", "+00aa", "+2400", "+0060"} {
		if _, err := ParseSeedUTCOffset(raw); !errors.Is(err, ErrBadSeedUTCOffset) {
			t.Fatalf("ParseSeedUTCOffset(%q) = %v, want ErrBadSeedUTCOffset", raw, err)
		}
	}
}
