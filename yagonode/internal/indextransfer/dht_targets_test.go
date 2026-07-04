package indextransfer

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

func dhtHash(tb testing.TB, raw string) yagomodel.Hash {
	tb.Helper()

	hash, err := yagomodel.ParseHash(raw)
	if err != nil {
		tb.Fatalf("parse hash %q: %v", raw, err)
	}

	return hash
}

func dhtAcceptFlags() yagomodel.Flags {
	return yagomodel.ZeroFlags().Set(yagomodel.FlagAcceptRemoteIndex, true)
}

func dhtSeed(tb testing.TB, raw string, options ...func(*yagomodel.Seed)) yagomodel.Seed {
	tb.Helper()

	host, err := yagomodel.ParseHost("192.0.2.1")
	if err != nil {
		tb.Fatalf("parse host: %v", err)
	}

	seed := yagomodel.Seed{
		Hash:  dhtHash(tb, raw),
		IP:    yagomodel.Some(host),
		Port:  yagomodel.Some(yagomodel.Port(8090)),
		Flags: yagomodel.Some(dhtAcceptFlags()),
	}
	for _, option := range options {
		option(&seed)
	}

	return seed
}

func withBirthDate(t time.Time) func(*yagomodel.Seed) {
	return func(seed *yagomodel.Seed) {
		seed.BirthDate = yagomodel.Some(yagomodel.NewSeedBirthDateUTC(t))
	}
}

func TestSelectDHTTargetsOrdersEligiblePeersFromStartHash(t *testing.T) {
	t.Parallel()

	start := dhtHash(t, "__________AA")
	targets, err := SelectDHTTargets(
		start,
		[]yagomodel.Seed{
			dhtSeed(t, "BBBBBBBBBBBB"),
			dhtSeed(t, "AAAAAAAAAAAA"),
			dhtSeed(t, "__________AA"),
		},
		DHTTargetConfig{Redundancy: 3},
	)
	if err != nil {
		t.Fatalf("SelectDHTTargets: %v", err)
	}

	got := []yagomodel.Hash{
		targets[0].Peer.Hash,
		targets[1].Peer.Hash,
		targets[2].Peer.Hash,
	}
	want := []yagomodel.Hash{
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

func TestSelectDHTTargetsFiltersReachabilityFlagsAgeAndLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	disabledFlags := yagomodel.ZeroFlags()
	targets, err := SelectDHTTargets(
		dhtHash(t, "AAAAAAAAAAAA"),
		[]yagomodel.Seed{
			dhtSeed(t, "CCCCCCCCCCCC", withBirthDate(now.AddDate(0, 0, -5))),
			dhtSeed(t, "DDDDDDDDDDDD", withBirthDate(now.AddDate(0, 0, -1))),
			dhtSeed(t, "EEEEEEEEEEEE"),
			dhtSeed(t, "FFFFFFFFFFFF", func(seed *yagomodel.Seed) {
				seed.Flags = yagomodel.Some(disabledFlags)
			}),
			dhtSeed(t, "GGGGGGGGGGGG", func(seed *yagomodel.Seed) {
				seed.Port = yagomodel.None[yagomodel.Port]()
			}),
			{
				Hash:  yagomodel.Hash("bad"),
				IP:    dhtSeed(t, "HHHHHHHHHHHH").IP,
				Port:  yagomodel.Some(yagomodel.Port(8090)),
				Flags: yagomodel.Some(dhtAcceptFlags()),
			},
		},
		DHTTargetConfig{
			Redundancy:     2,
			MinimumAgeDays: 3,
			Now:            now,
		},
	)
	if err != nil {
		t.Fatalf("SelectDHTTargets: %v", err)
	}

	got := []yagomodel.Hash{targets[0].Peer.Hash, targets[1].Peer.Hash}
	want := []yagomodel.Hash{dhtHash(t, "CCCCCCCCCCCC"), dhtHash(t, "EEEEEEEEEEEE")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("target order = %#v, want %#v", got, want)
	}
}

func TestSelectDHTTargetsBreaksPositionTiesAndTruncates(t *testing.T) {
	t.Parallel()

	targets, err := SelectDHTTargets(
		dhtHash(t, "AAAAAAAAAAAA"),
		[]yagomodel.Seed{
			dhtSeed(t, "DDDDDDDDDDDD"),
			dhtSeed(t, "CCCCCCCCCCBB"),
			dhtSeed(t, "CCCCCCCCCCAA"),
		},
		DHTTargetConfig{Redundancy: 2},
	)
	if err != nil {
		t.Fatalf("SelectDHTTargets: %v", err)
	}

	got := []yagomodel.Hash{targets[0].Peer.Hash, targets[1].Peer.Hash}
	want := []yagomodel.Hash{dhtHash(t, "CCCCCCCCCCAA"), dhtHash(t, "CCCCCCCCCCBB")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("target order = %#v, want %#v", got, want)
	}
}

func TestSelectDHTTargetsAtPositionRejectsInvalidPosition(t *testing.T) {
	t.Parallel()

	_, err := SelectDHTTargetsAtPosition(
		yagomodel.MaxPosition+1,
		[]yagomodel.Seed{dhtSeed(t, "BBBBBBBBBBBB")},
		DHTTargetConfig{Redundancy: 1},
	)
	if err == nil {
		t.Fatal("expected invalid dht position error")
	}
}

func TestSelectDHTTargetsAtPositionOrdersEligiblePeers(t *testing.T) {
	t.Parallel()

	start, err := yagomodel.Position(dhtHash(t, "AAAAAAAAAAAA"))
	if err != nil {
		t.Fatalf("dht position: %v", err)
	}
	targets, err := SelectDHTTargetsAtPosition(
		start,
		[]yagomodel.Seed{
			dhtSeed(t, "CCCCCCCCCCCC"),
			dhtSeed(t, "BBBBBBBBBBBB"),
		},
		DHTTargetConfig{Redundancy: 1},
	)
	if err != nil {
		t.Fatalf("SelectDHTTargetsAtPosition: %v", err)
	}
	if len(targets) != 1 || targets[0].Peer.Hash != dhtHash(t, "BBBBBBBBBBBB") {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestSelectDHTTargetsReturnsNoTargetsWhenRedundancyIsDisabled(t *testing.T) {
	t.Parallel()

	targets, err := SelectDHTTargets(
		dhtHash(t, "AAAAAAAAAAAA"),
		[]yagomodel.Seed{dhtSeed(t, "BBBBBBBBBBBB")},
		DHTTargetConfig{},
	)
	if err != nil {
		t.Fatalf("SelectDHTTargets: %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("targets = %#v, want none", targets)
	}
}

func TestSelectDHTTargetsRejectsInvalidStartHash(t *testing.T) {
	t.Parallel()

	_, err := SelectDHTTargets(
		yagomodel.Hash("bad"),
		[]yagomodel.Seed{dhtSeed(t, "BBBBBBBBBBBB")},
		DHTTargetConfig{Redundancy: 1},
	)
	if !errors.Is(err, yagomodel.ErrInvalidHash) {
		t.Fatalf("SelectDHTTargets invalid start = %v, want ErrInvalidHash", err)
	}
}
