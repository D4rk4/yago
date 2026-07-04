package dhttarget

import (
	"errors"
	"fmt"
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

func withRWICount(value int) func(*yagomodel.Seed) {
	return func(seed *yagomodel.Seed) {
		seed.RWICount = yagomodel.Some(value)
	}
}

func TestSelectOrdersEligiblePeersFromStartHash(t *testing.T) {
	t.Parallel()

	start := dhtHash(t, "__________AA")
	targets, err := Select(
		start,
		[]yagomodel.Seed{
			dhtSeed(t, "BBBBBBBBBBBB"),
			dhtSeed(t, "AAAAAAAAAAAA"),
			dhtSeed(t, "__________AA"),
		},
		Config{Redundancy: 3},
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
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

func TestSelectFiltersReachabilityFlagsAgeAndLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	disabledFlags := yagomodel.ZeroFlags()
	targets, err := Select(
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
		Config{
			Redundancy:     2,
			MinimumAgeDays: 3,
			Now:            now,
		},
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	got := []yagomodel.Hash{targets[0].Peer.Hash, targets[1].Peer.Hash}
	want := []yagomodel.Hash{dhtHash(t, "CCCCCCCCCCCC"), dhtHash(t, "EEEEEEEEEEEE")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("target order = %#v, want %#v", got, want)
	}
}

func TestSelectBreaksPositionTiesAndTruncates(t *testing.T) {
	t.Parallel()

	targets, err := Select(
		dhtHash(t, "AAAAAAAAAAAA"),
		[]yagomodel.Seed{
			dhtSeed(t, "DDDDDDDDDDDD"),
			dhtSeed(t, "CCCCCCCCCCBB"),
			dhtSeed(t, "CCCCCCCCCCAA"),
		},
		Config{Redundancy: 2},
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	got := []yagomodel.Hash{targets[0].Peer.Hash, targets[1].Peer.Hash}
	want := []yagomodel.Hash{dhtHash(t, "CCCCCCCCCCAA"), dhtHash(t, "CCCCCCCCCCBB")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("target order = %#v, want %#v", got, want)
	}
}

func TestSelectFiltersPeerRWIInventory(t *testing.T) {
	t.Parallel()

	targets, err := Select(
		dhtHash(t, "AAAAAAAAAAAA"),
		[]yagomodel.Seed{
			dhtSeed(t, "BBBBBBBBBBBB"),
			dhtSeed(t, "CCCCCCCCCCCC", withRWICount(0)),
			dhtSeed(t, "DDDDDDDDDDDD", withRWICount(1)),
			dhtSeed(t, "EEEEEEEEEEEE", withRWICount(3)),
		},
		Config{
			Redundancy:      3,
			MinimumRWICount: 1,
		},
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	got := []yagomodel.Hash{targets[0].Peer.Hash, targets[1].Peer.Hash}
	want := []yagomodel.Hash{dhtHash(t, "DDDDDDDDDDDD"), dhtHash(t, "EEEEEEEEEEEE")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("target order = %#v, want %#v", got, want)
	}
}

func TestSelectSamplesCandidateRedundancy(t *testing.T) {
	t.Parallel()

	script := &targetIndexScript{values: []int{2, 0}}
	targets, err := Select(
		dhtHash(t, "AAAAAAAAAAAA"),
		[]yagomodel.Seed{
			dhtSeed(t, "BBBBBBBBBBBB"),
			dhtSeed(t, "CCCCCCCCCCCC"),
			dhtSeed(t, "DDDDDDDDDDDD"),
			dhtSeed(t, "EEEEEEEEEEEE"),
		},
		Config{
			Redundancy:          2,
			CandidateRedundancy: 4,
			RandomTargetIndex:   script.next,
		},
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	got := []yagomodel.Hash{targets[0].Peer.Hash, targets[1].Peer.Hash}
	want := []yagomodel.Hash{dhtHash(t, "DDDDDDDDDDDD"), dhtHash(t, "BBBBBBBBBBBB")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("target order = %#v, want %#v", got, want)
	}
}

func TestSelectTruncatesCandidateRedundancyWithoutRandomTargetIndex(t *testing.T) {
	t.Parallel()

	targets, err := Select(
		dhtHash(t, "AAAAAAAAAAAA"),
		[]yagomodel.Seed{
			dhtSeed(t, "BBBBBBBBBBBB"),
			dhtSeed(t, "CCCCCCCCCCCC"),
			dhtSeed(t, "DDDDDDDDDDDD"),
		},
		Config{Redundancy: 1, CandidateRedundancy: 3},
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if len(targets) != 1 || targets[0].Peer.Hash != dhtHash(t, "BBBBBBBBBBBB") {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestSelectRejectsRandomTargetFailure(t *testing.T) {
	t.Parallel()

	_, err := Select(
		dhtHash(t, "AAAAAAAAAAAA"),
		[]yagomodel.Seed{
			dhtSeed(t, "BBBBBBBBBBBB"),
			dhtSeed(t, "CCCCCCCCCCCC"),
		},
		Config{
			Redundancy:          1,
			CandidateRedundancy: 2,
			RandomTargetIndex: func(int) (int, error) {
				return 0, errors.New("entropy failed")
			},
		},
	)
	if err == nil {
		t.Fatal("expected random target error")
	}
}

func TestSelectRejectsInvalidRandomTargetIndex(t *testing.T) {
	t.Parallel()

	_, err := Select(
		dhtHash(t, "AAAAAAAAAAAA"),
		[]yagomodel.Seed{
			dhtSeed(t, "BBBBBBBBBBBB"),
			dhtSeed(t, "CCCCCCCCCCCC"),
		},
		Config{
			Redundancy:          1,
			CandidateRedundancy: 2,
			RandomTargetIndex: func(int) (int, error) {
				return 2, nil
			},
		},
	)
	if err == nil {
		t.Fatal("expected invalid random target index error")
	}
}

func TestSelectAtPositionRejectsInvalidPosition(t *testing.T) {
	t.Parallel()

	_, err := SelectAtPosition(
		yagomodel.MaxPosition+1,
		[]yagomodel.Seed{dhtSeed(t, "BBBBBBBBBBBB")},
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
		[]yagomodel.Seed{dhtSeed(t, "BBBBBBBBBBBB")},
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
		yagomodel.Hash("bad"),
		[]yagomodel.Seed{dhtSeed(t, "BBBBBBBBBBBB")},
		Config{Redundancy: 1},
	)
	if !errors.Is(err, yagomodel.ErrInvalidHash) {
		t.Fatalf("Select invalid start = %v, want ErrInvalidHash", err)
	}
}

type targetIndexScript struct {
	values []int
}

func (s *targetIndexScript) next(upper int) (int, error) {
	if len(s.values) == 0 {
		return 0, fmt.Errorf("empty script for %d candidates", upper)
	}
	value := s.values[0]
	s.values = s.values[1:]

	return value, nil
}
