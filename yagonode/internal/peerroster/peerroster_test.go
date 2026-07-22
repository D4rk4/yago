package peerroster_test

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
)

const hashFiller = "AAAAAAAAAAAA"

func hashFor(base string) yagomodel.Hash {
	if len(base) >= yagomodel.HashLength {
		return yagomodel.Hash(base[:yagomodel.HashLength])
	}

	return yagomodel.Hash(base + hashFiller[len(base):])
}

func seniorSeed(t testing.TB, hash, ip string, port int) yagomodel.Seed {
	t.Helper()

	seed := yagomodel.Seed{
		Hash:     hashFor(hash),
		PeerType: yagomodel.Some(yagomodel.PeerSenior),
	}
	if ip != "" {
		host, err := yagomodel.ParseHost(ip)
		if err != nil {
			t.Fatalf("parse host: %v", err)
		}
		seed.IP = yagomodel.Some(host)
	}
	if port != 0 {
		seed.Port = yagomodel.Some(yagomodel.Port(port))
	}

	return seed
}

type tickingClock struct {
	now time.Time
}

func (c *tickingClock) Now() time.Time {
	c.now = c.now.Add(time.Second)

	return c.now
}

func openRoster(t *testing.T, reservoirCap, activeCap int) peerroster.Roster {
	t.Helper()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	clock := &tickingClock{now: time.Unix(1_000, 0)}
	roster, err := peerroster.Open(
		t.Context(), v, hashFor("local"), clock.Now,
		peerroster.Capacity{Reservoir: reservoirCap, Active: activeCap},
	)
	if err != nil {
		t.Fatalf("peerroster.Open: %v", err)
	}

	return roster
}

func openConcurrentRoster(t *testing.T, reservoirCap, activeCap int) peerroster.Roster {
	t.Helper()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	roster, err := peerroster.Open(
		t.Context(), v, hashFor("local"), time.Now,
		peerroster.Capacity{Reservoir: reservoirCap, Active: activeCap},
	)
	if err != nil {
		t.Fatalf("peerroster.Open: %v", err)
	}

	return roster
}

func hashes(seeds []yagomodel.Seed) map[yagomodel.Hash]struct{} {
	out := make(map[yagomodel.Hash]struct{}, len(seeds))
	for _, seed := range seeds {
		out[seed.Hash] = struct{}{}
	}

	return out
}
