package dhttarget

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
)

func dhtHash(tb testing.TB, raw string) yacymodel.Hash {
	tb.Helper()

	hash, err := yacymodel.ParseHash(raw)
	if err != nil {
		tb.Fatalf("parse hash %q: %v", raw, err)
	}

	return hash
}

func dhtAcceptFlags() yacymodel.Flags {
	return yacymodel.ZeroFlags().Set(yacymodel.FlagAcceptRemoteIndex, true)
}

func dhtSeed(tb testing.TB, raw string, options ...func(*yacymodel.Seed)) yacymodel.Seed {
	tb.Helper()

	host, err := yacymodel.ParseHost("192.0.2.1")
	if err != nil {
		tb.Fatalf("parse host: %v", err)
	}

	seed := yacymodel.Seed{
		Hash:  dhtHash(tb, raw),
		IP:    yacymodel.Some(host),
		Port:  yacymodel.Some(yacymodel.Port(8090)),
		Flags: yacymodel.Some(dhtAcceptFlags()),
	}
	for _, option := range options {
		option(&seed)
	}

	return seed
}

func withBirthDate(t time.Time) func(*yacymodel.Seed) {
	return func(seed *yacymodel.Seed) {
		seed.BirthDate = yacymodel.Some(yacymodel.NewSeedBirthDateUTC(t))
	}
}

func TestSelectOrdersEligiblePeersFromStartHash(t *testing.T) {
	t.Parallel()

	start := dhtHash(t, "__________AA")
	targets, err := Select(
		start,
		[]yacymodel.Seed{
			dhtSeed(t, "BBBBBBBBBBBB"),
			dhtSeed(t, "AAAAAAAAAAAA"),
			dhtSeed(t, "__________AA"),
		},
		Config{Redundancy: 3},
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	got := []yacymodel.Hash{
		targets[0].Peer.Hash,
		targets[1].Peer.Hash,
		targets[2].Peer.Hash,
	}
	want := []yacymodel.Hash{
		dhtHash(t, "__________AA"),
		dhtHash(t, "AAAAAAAAAAAA"),
		dhtHash(t, "BBBBBBBBBBBB"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("target order = %#v, want %#v", got, want)
	}
	if targets[0].Distance != 0 || targets[1].Distance >= targets[2].Distance {
		t.Fatalf("distances = %#v", targets)
	}
}

func TestSelectFiltersReachabilityFlagsAgeAndLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	disabledFlags := yacymodel.ZeroFlags()
	targets, err := Select(
		dhtHash(t, "AAAAAAAAAAAA"),
		[]yacymodel.Seed{
			dhtSeed(t, "CCCCCCCCCCCC", withBirthDate(now.AddDate(0, 0, -5))),
			dhtSeed(t, "DDDDDDDDDDDD", withBirthDate(now.AddDate(0, 0, -1))),
			dhtSeed(t, "EEEEEEEEEEEE"),
			dhtSeed(t, "FFFFFFFFFFFF", func(seed *yacymodel.Seed) {
				seed.Flags = yacymodel.Some(disabledFlags)
			}),
			dhtSeed(t, "GGGGGGGGGGGG", func(seed *yacymodel.Seed) {
				seed.Port = yacymodel.None[yacymodel.Port]()
			}),
			{
				Hash:  yacymodel.Hash("bad"),
				IP:    dhtSeed(t, "HHHHHHHHHHHH").IP,
				Port:  yacymodel.Some(yacymodel.Port(8090)),
				Flags: yacymodel.Some(dhtAcceptFlags()),
			},
		},
		Config{
			Redundancy:     2,
			MinimumAgeDays: 3,
			Now:            now,
		},
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	got := []yacymodel.Hash{targets[0].Peer.Hash, targets[1].Peer.Hash}
	want := []yacymodel.Hash{dhtHash(t, "CCCCCCCCCCCC"), dhtHash(t, "EEEEEEEEEEEE")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("target order = %#v, want %#v", got, want)
	}
}

func TestSelectBreaksPositionTiesAndTruncates(t *testing.T) {
	t.Parallel()

	targets, err := Select(
		dhtHash(t, "AAAAAAAAAAAA"),
		[]yacymodel.Seed{
			dhtSeed(t, "DDDDDDDDDDDD"),
			dhtSeed(t, "CCCCCCCCCCBB"),
			dhtSeed(t, "CCCCCCCCCCAA"),
		},
		Config{Redundancy: 2},
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	got := []yacymodel.Hash{targets[0].Peer.Hash, targets[1].Peer.Hash}
	want := []yacymodel.Hash{dhtHash(t, "CCCCCCCCCCAA"), dhtHash(t, "CCCCCCCCCCBB")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("target order = %#v, want %#v", got, want)
	}
}

func TestSelectAtPositionRejectsInvalidPosition(t *testing.T) {
	t.Parallel()

	_, err := SelectAtPosition(
		yacymodel.MaxPosition+1,
		[]yacymodel.Seed{dhtSeed(t, "BBBBBBBBBBBB")},
		Config{Redundancy: 1},
	)
	if err == nil {
		t.Fatal("expected invalid dht position error")
	}
}

func TestSelectReturnsNoTargetsWhenRedundancyIsDisabled(t *testing.T) {
	t.Parallel()

	targets, err := Select(
		dhtHash(t, "AAAAAAAAAAAA"),
		[]yacymodel.Seed{dhtSeed(t, "BBBBBBBBBBBB")},
		Config{},
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("targets = %#v, want none", targets)
	}
}

func TestSelectRejectsInvalidStartHash(t *testing.T) {
	t.Parallel()

	_, err := Select(
		yacymodel.Hash("bad"),
		[]yacymodel.Seed{dhtSeed(t, "BBBBBBBBBBBB")},
		Config{Redundancy: 1},
	)
	if !errors.Is(err, yacymodel.ErrInvalidHash) {
		t.Fatalf("Select invalid start = %v, want ErrInvalidHash", err)
	}
}
