package yagomodel

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

func TestParseSeedUTCAcceptsOffsetAndTimestamp(t *testing.T) {
	for _, raw := range []string{"+0230", "-0345", "20260614000329"} {
		got, err := ParseSeedUTC(raw)
		if err != nil {
			t.Fatalf("ParseSeedUTC(%q): %v", raw, err)
		}
		if got.String() != raw {
			t.Fatalf("ParseSeedUTC(%q) = %q", raw, got)
		}
	}
}

func TestParseSeedUTCRejectsBadValues(t *testing.T) {
	for _, raw := range []string{"+aa00", "+00aa", "+2400", "+0060", "2026061400032", "20261314000329"} {
		if _, err := ParseSeedUTC(raw); !errors.Is(err, ErrBadSeedUTC) {
			t.Fatalf("ParseSeedUTC(%q) = %v, want ErrBadSeedUTC", raw, err)
		}
	}
}

// badSeedTimestamps are the shapes a peer can put in a bare 14-digit timestamp
// field. Nothing but the parse can tell them apart from a valid instant, and an
// accepted garbage instant lands directly in peer liveness and seed age, which
// drive DHT target selection and seed retention.
var badSeedTimestamps = []string{
	"",
	"2018062814071",       // one digit short
	"201806281407130",     // one digit too long
	"20181328140713",      // month 13
	"20180229140713",      // 2018 has no 29 February
	"2018-06-28T14:07:13", // RFC 3339, not YaCy's short form
	"20180628140713 ",     // trailing space
}

func TestParseSeedLastSeenUTCRejectsBadTimestamps(t *testing.T) {
	for _, raw := range badSeedTimestamps {
		if _, err := ParseSeedLastSeenUTC(raw); !errors.Is(err, ErrBadSeedLastSeenUTC) {
			t.Errorf("ParseSeedLastSeenUTC(%q) = %v, want ErrBadSeedLastSeenUTC", raw, err)
		}
	}
}

func TestParseSeedBirthDateUTCRejectsBadTimestamps(t *testing.T) {
	for _, raw := range badSeedTimestamps {
		if _, err := ParseSeedBirthDateUTC(raw); !errors.Is(err, ErrBadSeedBirthDateUTC) {
			t.Errorf("ParseSeedBirthDateUTC(%q) = %v, want ErrBadSeedBirthDateUTC", raw, err)
		}
	}
}

// The two timestamp sentinels exist so a seed refusal names which field was
// wrong, but ParseSeed folds every field failure into ErrBadSeed. The specific
// cause has to survive that wrapping, and LastSeen must not answer with the
// birth-date sentinel or the other way round: the fields have different
// consequences, so a cross-wired refusal would send an operator hunting the
// wrong column.
func TestParseSeedNamesTheRefusedTimestampField(t *testing.T) {
	_, lastSeen := ParseSeed(t.Context(), "Hash=ABCDEFGHIJKL,LastSeen=2026-06-22")
	if !errors.Is(lastSeen, ErrBadSeedLastSeenUTC) ||
		errors.Is(lastSeen, ErrBadSeedBirthDateUTC) {
		t.Fatalf("bad LastSeen = %v, want ErrBadSeedLastSeenUTC only", lastSeen)
	}

	_, birthDate := ParseSeed(t.Context(), "Hash=ABCDEFGHIJKL,BDate=2026-06-22")
	if !errors.Is(birthDate, ErrBadSeedBirthDateUTC) ||
		errors.Is(birthDate, ErrBadSeedLastSeenUTC) {
		t.Fatalf("bad BDate = %v, want ErrBadSeedBirthDateUTC only", birthDate)
	}
}
